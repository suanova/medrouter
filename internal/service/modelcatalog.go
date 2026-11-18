package service

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/suanova/medrouter/internal/config"
	"github.com/suanova/medrouter/internal/kube"
	"gopkg.in/yaml.v3"
)

const (
	defaultCatalogTTL     = 30 * time.Second
	defaultCatalogTimeout = 5 * time.Second
	negativeCatalogTTL    = 10 * time.Second
	crdMissingCatalogTTL  = 60 * time.Second
	minCatalogTTL         = time.Second
)

// catalogSnapshot is an immutable view of one refresh attempt.
type catalogSnapshot struct {
	list      *ModelList
	mvs       []kube.ModelVersion
	profiles  []kube.RuntimeProfile
	overviews map[string]*ModelOverview // model -> parsed, validated overview
	fetchedAt time.Time
	expiresAt time.Time
	available bool
}

// ModelCatalog caches raw CubeStack resources and projects them per request.
// The cache is process-local: several MedRouter replicas each read the API
// independently, which is cheap because the CRDs are small.
type ModelCatalog struct {
	source  kube.Source
	ttl     time.Duration
	timeout time.Duration
	now     func() time.Time

	mu       sync.Mutex
	current  *catalogSnapshot
	lastGood *catalogSnapshot

	refreshMu sync.Mutex
	lastLog   string
}

func NewModelCatalog(cfg *config.Config, source kube.Source) *ModelCatalog {
	ttl := cfg.ModelLibraryCacheTTL
	if ttl < minCatalogTTL {
		ttl = defaultCatalogTTL
	}
	timeout := cfg.ModelLibraryTimeout
	if timeout <= 0 {
		timeout = defaultCatalogTimeout
	}
	return &ModelCatalog{
		source:  source,
		ttl:     ttl,
		timeout: timeout,
		now:     time.Now,
	}
}

// List returns the projected catalog, refreshing it when the TTL expires.
func (c *ModelCatalog) List(ctx context.Context) *ModelList {
	return c.snapshot(ctx).list
}

// Detail returns the expanded model and whether ref exists. When the catalog
// is unavailable the returned detail carries the availability status and
// found is false.
func (c *ModelCatalog) Detail(ctx context.Context, ref string) (*ModelDetail, bool) {
	snap := c.snapshot(ctx)
	if !snap.available {
		return &ModelDetail{CatalogStatus: snap.list.CatalogStatus}, false
	}
	detail, ok := BuildModelDetail(snap.list, snap.mvs, snap.profiles, ref)
	if !ok {
		return nil, false
	}
	if ov := snap.overviews[detail.Model]; ov != nil && ov.Valid {
		detail.Overview = ov
	}
	return detail, true
}

func (c *ModelCatalog) snapshot(ctx context.Context) *catalogSnapshot {
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
		stale.list = copyList(c.lastGood.list)
		stale.list.Stale = true
		stale.list.Warning = catalogMessage(reason)
		stale.list.GeneratedAt = c.lastGood.list.GeneratedAt
		stale.available = true
		stale.expiresAt = now.Add(c.negativeTTL(reason))
		c.current = &stale
		c.logState(reason, catalogMessage(reason))
		return &stale
	}

	c.current = fresh
	c.logState(reason, catalogMessage(reason))
	return fresh
}

func (c *ModelCatalog) fetch(ctx context.Context) *catalogSnapshot {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	var (
		mvs       []kube.ModelVersion
		profiles  []kube.RuntimeProfile
		ovModels  []kube.OverviewConfigMap
		mvErr     error
		profErr   error
		ovErr     error
		wg        sync.WaitGroup
	)
	wg.Add(3)
	go func() {
		defer wg.Done()
		mvs, mvErr = c.source.ListModelVersions(ctx)
	}()
	go func() {
		defer wg.Done()
		profiles, profErr = c.source.ListRuntimeProfiles(ctx)
	}()
	go func() {
		defer wg.Done()
		if os, ok := c.source.(kube.OverviewSource); ok {
			ovModels, ovErr = os.ListOverviewConfigMaps(ctx)
		}
	}()
	wg.Wait()

	now := c.now()
	if mvErr != nil {
		reason, retryable := statusFromError(mvErr)
		status := CatalogStatus{
			Available:         false,
			Reason:            reason,
			Message:           catalogMessage(reason),
			Retryable:         retryable,
			ProfilesAvailable: false,
			GeneratedAt:       now.UTC(),
		}
		return &catalogSnapshot{
			list: &ModelList{
				CatalogStatus: status,
				Models:        []ModelSummary{},
			},
			fetchedAt: now,
			expiresAt: now.Add(c.negativeTTL(reason)),
		}
	}

	list := BuildCatalogList(mvs, profiles, profErr, now)
	return &catalogSnapshot{
		list:      list,
		mvs:       mvs,
		profiles:  profiles,
		overviews: buildOverviews(ovModels, mvs, ovErr, now),
		fetchedAt: now,
		expiresAt: now.Add(c.ttl),
		available: true,
	}
}

// buildOverviews indexes overview ConfigMaps by ModelVersion spec.model, keeping
// only those that match an existing model and pass anchor validation. The
// matching must be exact (model strong-validation). Overview errors are not
// fatal; at worst overviews is empty and the overview tab shows core fields.
func buildOverviews(cms []kube.OverviewConfigMap, mvs []kube.ModelVersion, ovErr error, now time.Time) map[string]*ModelOverview {
	out := map[string]*ModelOverview{}
	if ovErr != nil {
		return out
	}
	modelSet := map[string]struct{}{}
	for _, mv := range mvs {
		modelSet[mv.Spec.Model] = struct{}{}
	}
	for _, cm := range cms {
		body, ok := cm.OverviewBody()
		if !ok {
			continue
		}
		// The model anchor is unknown until parsed; we still need the model name
		// to index. Parse returns the anchored ModelOverview, so synthesize a
		// candidate by attempting parse with each known model would be wrong.
		// Instead, the body's own model field decides the key; if it matches a
		// known model we accept, otherwise it is dropped (INVALID_MODEL_OVERVIEW).
		var probe struct {
			Model string `yaml:"model"`
		}
		_ = yaml.Unmarshal([]byte(body), &probe)
		if probe.Model == "" {
			continue
		}
		if _, known := modelSet[probe.Model]; !known {
			continue
		}
		ov, err := ParseModelOverview(body, probe.Model)
		if err != nil || !ov.Valid {
			continue
		}
		out[probe.Model] = ov
	}
	return out
}

func (c *ModelCatalog) staleWindow() time.Duration {
	window := 5 * c.ttl
	if window < time.Minute {
		window = time.Minute
	}
	return window
}

func (c *ModelCatalog) negativeTTL(reason string) time.Duration {
	if reason == CatalogCRDMissing {
		return crdMissingCatalogTTL
	}
	if c.ttl < negativeCatalogTTL {
		return c.ttl
	}
	return negativeCatalogTTL
}

func (c *ModelCatalog) logState(reason, message string) {
	key := reason + "|" + message
	if key == c.lastLog {
		return
	}
	c.lastLog = key
	if reason == "" {
		log.Printf("model catalog: available")
		return
	}
	log.Printf("WARN model catalog unavailable reason=%s message=%q", reason, message)
}

func copyList(in *ModelList) *ModelList {
	if in == nil {
		return &ModelList{Models: []ModelSummary{}}
	}
	out := *in
	out.Models = append([]ModelSummary(nil), in.Models...)
	return &out
}
