package service

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/suanova/medrouter/internal/config"
	"github.com/suanova/medrouter/internal/kube"
)

// datasetSnapshot is an immutable view of one refresh attempt.
type datasetSnapshot struct {
	list      *DatasetList
	datasets  []kube.Dataset
	overviews map[string]*DatasetOverview // dataset -> parsed, validated overview
	fetchedAt time.Time
	expiresAt time.Time
	available bool
}

// DatasetCatalog caches raw Dataset CRs and projects them per request. It does
// not share state with ModelCatalog: a missing datasets CRD must not make the
// model library unavailable, and vice versa.
type DatasetCatalog struct {
	source  kube.Source
	ttl     time.Duration
	timeout time.Duration
	now     func() time.Time

	mu       sync.Mutex
	current  *datasetSnapshot
	lastGood *datasetSnapshot

	refreshMu sync.Mutex
	lastLog   string
}

func NewDatasetCatalog(cfg *config.Config, source kube.Source) *DatasetCatalog {
	ttl := cfg.DatasetLibraryCacheTTL
	if ttl < minCatalogTTL {
		ttl = defaultCatalogTTL
	}
	timeout := cfg.DatasetLibraryTimeout
	if timeout <= 0 {
		timeout = defaultCatalogTimeout
	}
	return &DatasetCatalog{
		source:  source,
		ttl:     ttl,
		timeout: timeout,
		now:     time.Now,
	}
}

// List returns the projected catalog, refreshing it when the TTL expires.
func (c *DatasetCatalog) List(ctx context.Context) *DatasetList {
	return c.snapshot(ctx).list
}

// VersionStorage resolves one Dataset version for the file browser. It returns
// the raw storage block (only inside the API process; it is never serialized),
// whether the version exists, and the catalog status so the caller can tell a
// missing dataset from an unavailable catalog.
//
// versionRef may be a Dataset metadata.name, a spec.version, or empty for the
// latest version of the group.
func (c *DatasetCatalog) VersionStorage(ctx context.Context, name, versionRef string) (DatasetVersionStorage, bool, DatasetCatalogStatus) {
	snap := c.snapshot(ctx)
	status := snap.list.DatasetCatalogStatus
	if !snap.available {
		return DatasetVersionStorage{}, false, status
	}

	group, ok := findDatasetGroup(groupDatasets(snap.datasets), name)
	if !ok {
		return DatasetVersionStorage{}, false, status
	}

	chosen := group.versions[0]
	if ref := strings.TrimSpace(versionRef); ref != "" {
		found := false
		for _, version := range group.versions {
			if version.Metadata.Name == ref || version.Spec.Version == ref {
				chosen = version
				found = true
				break
			}
		}
		if !found {
			return DatasetVersionStorage{}, false, status
		}
	}

	return DatasetVersionStorage{
		Ref:      chosen.Metadata.Name,
		Version:  chosen.Spec.Version,
		Strategy: chosen.Spec.Storage.Strategy,
		S3URI:    s3URI(chosen.Spec.Storage.S3),
	}, true, status
}

// DatasetVersionStorage is the subset of a Dataset version the file browser
// needs. S3URI stays server-side.
type DatasetVersionStorage struct {
	Ref      string
	Version  string
	Strategy string
	S3URI    string
}

func s3URI(storage *kube.S3Storage) string {
	if storage == nil {
		return ""
	}
	return strings.TrimSpace(storage.URI)
}

// Detail returns the expanded dataset and whether ref exists. When the catalog
// is unavailable the returned detail carries the availability status and found
// is false.
func (c *DatasetCatalog) Detail(ctx context.Context, ref string) (*DatasetDetail, bool) {
	snap := c.snapshot(ctx)
	if !snap.available {
		return &DatasetDetail{DatasetCatalogStatus: snap.list.DatasetCatalogStatus}, false
	}
	detail, ok := BuildDatasetDetail(snap.list, snap.datasets, ref)
	if !ok {
		return nil, false
	}
	if ov := snap.overviews[detail.Dataset]; ov != nil && ov.Valid {
		detail.Overview = ov
	}
	return detail, true
}

func (c *DatasetCatalog) snapshot(ctx context.Context) *datasetSnapshot {
	now := c.now()
	c.mu.Lock()
	current := c.current
	c.mu.Unlock()
	if current != nil && now.Before(current.expiresAt) {
		return current
	}

	// One refresh at a time; readers that missed the double-check wait for the
	// result instead of stampeding the API.
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()

	now = c.now()
	c.mu.Lock()
	current = c.current
	c.mu.Unlock()
	if current != nil && now.Before(current.expiresAt) {
		return current
	}

	fresh := c.fetch(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()

	if fresh.available {
		c.current = fresh
		c.lastGood = fresh
		c.logState("", "")
		return fresh
	}

	reason := fresh.list.Reason
	if c.lastGood != nil && now.Sub(c.lastGood.fetchedAt) <= c.staleWindow() {
		stale := *c.lastGood
		stale.list = copyDatasetList(c.lastGood.list)
		stale.list.Stale = true
		stale.list.Warning = datasetCatalogMessage(reason)
		stale.list.GeneratedAt = c.lastGood.list.GeneratedAt
		stale.available = true
		stale.expiresAt = now.Add(c.negativeTTL(reason))
		c.current = &stale
		c.logState(reason, datasetCatalogMessage(reason))
		return &stale
	}

	c.current = fresh
	c.logState(reason, datasetCatalogMessage(reason))
	return fresh
}

func (c *DatasetCatalog) fetch(ctx context.Context) *datasetSnapshot {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	var (
		datasets []kube.Dataset
		ovs      []kube.OverviewConfigMap
		dsErr    error
		ovErr    error
		wg       sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		datasets, dsErr = c.source.ListDatasets(ctx)
	}()
	go func() {
		defer wg.Done()
		if os, ok := c.source.(kube.OverviewSource); ok {
			ovs, ovErr = os.ListOverviewConfigMaps(ctx)
		}
	}()
	wg.Wait()

	now := c.now()
	if dsErr != nil {
		reason, retryable := statusFromError(dsErr)
		return &datasetSnapshot{
			list: &DatasetList{
				DatasetCatalogStatus: DatasetCatalogStatus{
					Available:   false,
					Reason:      reason,
					Message:     datasetCatalogMessage(reason),
					Retryable:   retryable,
					GeneratedAt: now.UTC(),
				},
				Datasets: []DatasetSummary{},
			},
			fetchedAt: now,
			expiresAt: now.Add(c.negativeTTL(reason)),
		}
	}

	return &datasetSnapshot{
		list:      BuildDatasetList(datasets, now),
		datasets:  datasets,
		overviews: buildDatasetOverviews(ovs, datasets, ovErr),
		fetchedAt: now,
		expiresAt: now.Add(c.ttl),
		available: true,
	}
}

// buildDatasetOverviews indexes dataset overview ConfigMaps by Dataset.spec.dataset,
// keeping only those that match an existing dataset and pass anchor validation.
// The match must be exact (dataset strong-validation). Overview errors are not
// fatal; at worst overviews is empty and the overview area shows core fields.
func buildDatasetOverviews(cms []kube.OverviewConfigMap, datasets []kube.Dataset, ovErr error) map[string]*DatasetOverview {
	out := map[string]*DatasetOverview{}
	if ovErr != nil {
		return out
	}
	set := map[string]struct{}{}
	for _, ds := range datasets {
		set[ds.Spec.Dataset] = struct{}{}
	}
	for _, cm := range cms {
		body, ok := cm.OverviewBody()
		if !ok {
			continue
		}
		var probe struct {
			Dataset string `yaml:"dataset"`
		}
		if err := yaml.Unmarshal([]byte(body), &probe); err != nil || probe.Dataset == "" {
			continue
		}
		if _, known := set[probe.Dataset]; !known {
			continue
		}
		ov, err := ParseDatasetOverview(body, probe.Dataset)
		if err != nil || !ov.Valid {
			continue
		}
		out[probe.Dataset] = ov
	}
	return out
}

func (c *DatasetCatalog) staleWindow() time.Duration {
	window := 5 * c.ttl
	if window < time.Minute {
		window = time.Minute
	}
	return window
}

func (c *DatasetCatalog) negativeTTL(reason string) time.Duration {
	if reason == CatalogCRDMissing {
		return crdMissingCatalogTTL
	}
	if c.ttl < negativeCatalogTTL {
		return c.ttl
	}
	return negativeCatalogTTL
}

func (c *DatasetCatalog) logState(reason, message string) {
	key := reason + "|" + message
	if key == c.lastLog {
		return
	}
	c.lastLog = key
	if reason == "" {
		log.Printf("dataset catalog: available")
		return
	}
	log.Printf("WARN dataset catalog unavailable reason=%s message=%q", reason, message)
}

func copyDatasetList(in *DatasetList) *DatasetList {
	if in == nil {
		return &DatasetList{Datasets: []DatasetSummary{}}
	}
	out := *in
	out.Datasets = append([]DatasetSummary(nil), in.Datasets...)
	return &out
}
