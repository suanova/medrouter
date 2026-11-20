package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/suanova/medrouter/internal/config"
	"github.com/suanova/medrouter/internal/kube"
	"github.com/suanova/medrouter/internal/s3"
)

type fakeStore struct {
	mu sync.Mutex

	page       *s3.ListPage
	listErr    error
	headErr    error
	presignErr error
	presignURL string
	rangeErr   error
	objectSize int64
	objectBody []byte

	listCalls    int
	headCalls    int
	presignCalls int
	rangeCalls   int

	gotBucket  string
	gotPrefix  string
	gotToken   string
	gotMaxKeys int32
	gotKey     string
	gotTTL     time.Duration
	gotOffset  int64
	gotLength  int64
}

func (f *fakeStore) List(_ context.Context, bucket, prefix, _, token string, maxKeys int32) (*s3.ListPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls++
	f.gotBucket, f.gotPrefix, f.gotToken, f.gotMaxKeys = bucket, prefix, token, maxKeys
	return f.page, f.listErr
}

func (f *fakeStore) Head(_ context.Context, bucket, key string) (s3.Object, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.headCalls++
	f.gotBucket, f.gotKey = bucket, key
	return s3.Object{Key: key, Size: f.objectSize}, f.headErr
}

func (f *fakeStore) GetRange(_ context.Context, bucket, key string, offset, length int64) (*s3.RangeObject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rangeCalls++
	f.gotBucket, f.gotKey, f.gotOffset, f.gotLength = bucket, key, offset, length
	if f.rangeErr != nil {
		return nil, f.rangeErr
	}
	start := offset
	if start > int64(len(f.objectBody)) {
		start = int64(len(f.objectBody))
	}
	end := start + length
	if end > int64(len(f.objectBody)) {
		end = int64(len(f.objectBody))
	}
	return &s3.RangeObject{
		Body:      io.NopCloser(strings.NewReader(string(f.objectBody[start:end]))),
		TotalSize: f.objectSize,
	}, nil
}

func (f *fakeStore) PresignGet(_ context.Context, bucket, key string, ttl time.Duration) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.presignCalls++
	f.gotBucket, f.gotKey, f.gotTTL = bucket, key, ttl
	return f.presignURL, f.presignErr
}

func filesTestConfig() *config.Config {
	return &config.Config{
		DatasetLibraryCacheTTL:   time.Minute,
		DatasetLibraryTimeout:    time.Second,
		S3PresignTTL:             10 * time.Minute,
		S3ListCacheTTL:           0,
		S3Timeout:                time.Second,
		S3PreviewEnabled:         true,
		S3PreviewTextWindow:      256 << 10,
		S3PreviewImageMax:        10 << 20,
		S3PreviewPDFMax:          32 << 20,
		S3DownloadLargeThreshold: 2 << 30,
		S3DownloadLargeTTL:       time.Hour,
	}
}

func filesTestDataset(strategy, uri string) kube.Dataset {
	return kube.Dataset{
		Metadata: kube.ObjectMeta{Name: "demo-2024", CreationTimestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		Spec: kube.DatasetSpec{
			Dataset: "demo",
			Version: "2024",
			Format:  "Parquet",
			Storage: kube.ModelStorage{
				Strategy: strategy,
				S3:       &kube.S3Storage{URI: uri},
			},
		},
	}
}

func newFilesService(t *testing.T, dataset kube.Dataset, store ObjectStore) *FilesService {
	t.Helper()
	cfg := filesTestConfig()
	catalog := NewDatasetCatalog(cfg, &fakeSource{datasets: []kube.Dataset{dataset}})
	return NewFilesService(cfg, catalog, store)
}

func TestDatasetFilesListProjectsRelativeEntries(t *testing.T) {
	modified := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{page: &s3.ListPage{
		Prefixes: []string{"demo/2024/sub/"},
		Objects: []s3.Object{
			{Key: "demo/2024/", Size: 0, LastModified: modified},
			{Key: "demo/2024/a.csv", Size: 42, LastModified: modified},
		},
	}}
	svc := newFilesService(t, filesTestDataset("S3", "s3://datasets/demo/2024"), store)

	files, found := svc.List(context.Background(), "demo", "", "", "", 200)
	if !found || files == nil || !files.Available {
		t.Fatalf("files = %#v found=%v", files, found)
	}
	if files.Ref != "demo-2024" || files.Version != "2024" || files.Storage != "S3" {
		t.Fatalf("identity = %#v", files)
	}
	if store.gotBucket != "datasets" || store.gotPrefix != "demo/2024/" {
		t.Fatalf("store call bucket=%q prefix=%q", store.gotBucket, store.gotPrefix)
	}
	if store.gotMaxKeys != 200 {
		t.Fatalf("maxKeys = %d", store.gotMaxKeys)
	}
	if len(files.Entries) != 2 {
		t.Fatalf("entries = %#v", files.Entries)
	}
	if files.Entries[0].Type != "dir" || files.Entries[0].Name != "sub" || files.Entries[0].Path != "sub/" {
		t.Fatalf("dir entry = %#v", files.Entries[0])
	}
	file := files.Entries[1]
	if file.Type != "file" || file.Name != "a.csv" || file.Path != "a.csv" || file.Size == nil || *file.Size != 42 {
		t.Fatalf("file entry = %#v", file)
	}
}

func TestDatasetFilesListNeverLeaksBucketOrURI(t *testing.T) {
	store := &fakeStore{page: &s3.ListPage{Objects: []s3.Object{{Key: "demo/2024/a.csv"}}}}
	svc := newFilesService(t, filesTestDataset("S3", "s3://datasets/demo/2024"), store)

	files, _ := svc.List(context.Background(), "demo", "", "", "", 200)
	raw, err := json.Marshal(files)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, secret := range []string{"s3://", "datasets/demo", "internal-bucket"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("response leaks %q: %s", secret, raw)
		}
	}
}

func TestDatasetFilesListReasons(t *testing.T) {
	cases := []struct {
		name       string
		dataset    kube.Dataset
		store      ObjectStore
		prefix     string
		wantReason string
	}{
		{
			name:       "unsupported storage",
			dataset:    filesTestDataset("HostPath", ""),
			store:      &fakeStore{},
			wantReason: FilesUnsupportedStorage,
		},
		{
			name:       "missing uri",
			dataset:    filesTestDataset("S3", ""),
			store:      &fakeStore{},
			wantReason: FilesURIMissing,
		},
		{
			name:       "bucket root is rejected",
			dataset:    filesTestDataset("S3", "s3://datasets"),
			store:      &fakeStore{},
			wantReason: FilesURIMissing,
		},
		{
			name:       "store disabled",
			dataset:    filesTestDataset("S3", "s3://datasets/demo/2024"),
			store:      nil,
			wantReason: FilesDisabled,
		},
		{
			name:       "invalid prefix",
			dataset:    filesTestDataset("S3", "s3://datasets/demo/2024"),
			store:      &fakeStore{},
			prefix:     "../etc",
			wantReason: FilesInvalidPath,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newFilesService(t, tc.dataset, tc.store)
			files, found := svc.List(context.Background(), "demo", "", tc.prefix, "", 200)
			if !found || files == nil {
				t.Fatalf("found = %v files = %#v", found, files)
			}
			if files.Available || files.Reason != tc.wantReason {
				t.Fatalf("available=%v reason=%q want %q", files.Available, files.Reason, tc.wantReason)
			}
			if len(files.Entries) != 0 {
				t.Fatalf("entries = %#v", files.Entries)
			}
		})
	}
}

func TestDatasetFilesListMapsStoreErrors(t *testing.T) {
	store := &fakeStore{listErr: &s3.Error{Reason: s3.ReasonAuth, Retryable: false}}
	svc := newFilesService(t, filesTestDataset("S3", "s3://datasets/demo/2024"), store)

	files, _ := svc.List(context.Background(), "demo", "", "", "", 200)
	if files.Available || files.Reason != s3.ReasonAuth || files.Retryable {
		t.Fatalf("files = %#v", files)
	}
}

func TestDatasetFilesListOnUnavailableCatalog(t *testing.T) {
	cfg := filesTestConfig()
	catalog := NewDatasetCatalog(cfg, &fakeSource{dsErr: errors.New("cluster down")})
	svc := NewFilesService(cfg, catalog, &fakeStore{})

	files, found := svc.List(context.Background(), "demo", "", "", "", 200)
	if !found || files == nil {
		t.Fatalf("found = %v", found)
	}
	if files.Available || files.Reason != CatalogAPIError {
		t.Fatalf("files = %#v", files)
	}
}

func TestDatasetFilesUnknownVersionIsNotFound(t *testing.T) {
	svc := newFilesService(t, filesTestDataset("S3", "s3://datasets/demo/2024"), &fakeStore{})
	if _, found := svc.List(context.Background(), "demo", "2049", "", "", 200); found {
		t.Fatalf("unknown version should not be found")
	}
	if _, found := svc.List(context.Background(), "other", "", "", "", 200); found {
		t.Fatalf("unknown dataset should not be found")
	}
}

func TestDatasetFilesVersionSelection(t *testing.T) {
	cfg := filesTestConfig()
	v1 := filesTestDataset("S3", "s3://datasets/demo/2024")
	v2 := filesTestDataset("S3", "s3://datasets/demo/2025")
	v2.Metadata.Name = "demo-2025"
	v2.Spec.Version = "2025"
	v2.Metadata.CreationTimestamp = v2.Metadata.CreationTimestamp.Add(24 * time.Hour)
	catalog := NewDatasetCatalog(cfg, &fakeSource{datasets: []kube.Dataset{v1, v2}})
	svc := NewFilesService(cfg, catalog, &fakeStore{})

	files, found := svc.List(context.Background(), "demo", "2024", "", "", 200)
	if !found || files.Ref != "demo-2024" || files.Version != "2024" {
		t.Fatalf("version by spec.version = %#v found=%v", files, found)
	}
	files, found = svc.List(context.Background(), "demo", "demo-2025", "", "", 200)
	if !found || files.Ref != "demo-2025" || files.Version != "2025" {
		t.Fatalf("version by ref = %#v found=%v", files, found)
	}
	files, found = svc.List(context.Background(), "demo", "", "", "", 200)
	if !found || files.Version != "2025" {
		t.Fatalf("latest version = %#v found=%v", files, found)
	}
}

func TestDatasetFilesDownload(t *testing.T) {
	store := &fakeStore{presignURL: "https://objects.example/x?sig=1"}
	svc := newFilesService(t, filesTestDataset("S3", "s3://datasets/demo/2024"), store)

	download, found := svc.Download(context.Background(), "user-1", "demo", "", "a.csv")
	if !found || download == nil || !download.Available {
		t.Fatalf("download = %#v found=%v", download, found)
	}
	if download.URL != store.presignURL || download.ExpiresAt == nil {
		t.Fatalf("download = %#v", download)
	}
	if store.gotBucket != "datasets" || store.gotKey != "demo/2024/a.csv" {
		t.Fatalf("store call bucket=%q key=%q", store.gotBucket, store.gotKey)
	}
	if store.gotTTL != 10*time.Minute {
		t.Fatalf("ttl = %s", store.gotTTL)
	}
}

func TestDatasetFilesDownloadRejectsTraversal(t *testing.T) {
	store := &fakeStore{presignURL: "https://objects.example/x"}
	svc := newFilesService(t, filesTestDataset("S3", "s3://datasets/demo/2024"), store)

	cases := []string{"../secret", "a/../../secret", "/etc/passwd", `a\b`, "dir/", ""}
	for _, raw := range cases {
		download, found := svc.Download(context.Background(), "user-1", "demo", "", raw)
		if !found || download == nil {
			t.Fatalf("path %q: found=%v", raw, found)
		}
		if download.Available || download.Reason != FilesInvalidPath {
			t.Fatalf("path %q: %#v", raw, download)
		}
	}
	if store.headCalls != 0 || store.presignCalls != 0 {
		t.Fatalf("store was called for an invalid path: head=%d presign=%d", store.headCalls, store.presignCalls)
	}
}

func TestDatasetFilesDownloadNotFound(t *testing.T) {
	store := &fakeStore{headErr: &s3.Error{Reason: s3.ReasonNotFound}}
	svc := newFilesService(t, filesTestDataset("S3", "s3://datasets/demo/2024"), store)

	download, found := svc.Download(context.Background(), "user-1", "demo", "", "missing.csv")
	if !found || download == nil || download.Available || download.Reason != s3.ReasonNotFound {
		t.Fatalf("download = %#v found=%v", download, found)
	}
	if store.presignCalls != 0 {
		t.Fatalf("presign should not run for a missing object")
	}
}

func TestParseS3URI(t *testing.T) {
	bucket, prefix, err := ParseS3URI("s3://datasets/uniprot/uniref50/2024_01/")
	if err != nil || bucket != "datasets" || prefix != "uniprot/uniref50/2024_01" {
		t.Fatalf("got %q %q %v", bucket, prefix, err)
	}
	for _, raw := range []string{"", "datasets/x", "s3://", "s3://datasets", "s3://datasets/", "s3://datasets/../x", "s3://datasets//x"} {
		if _, _, err := ParseS3URI(raw); err == nil {
			t.Fatalf("ParseS3URI(%q) should fail", raw)
		}
	}
}

func TestNormalizePaths(t *testing.T) {
	prefix, err := NormalizePrefix("a//b/")
	if err != nil || prefix != "a/b/" {
		t.Fatalf("prefix = %q err=%v", prefix, err)
	}
	if prefix, err := NormalizePrefix(""); err != nil || prefix != "" {
		t.Fatalf("empty prefix = %q err=%v", prefix, err)
	}
	for _, raw := range []string{"/a", "a/../b", `a\b`, "a/\x00b"} {
		if _, err := NormalizePrefix(raw); err == nil {
			t.Fatalf("NormalizePrefix(%q) should fail", raw)
		}
	}
	if value, err := NormalizeFilePath("sub/a.csv"); err != nil || value != "sub/a.csv" {
		t.Fatalf("file path = %q err=%v", value, err)
	}
	for _, raw := range []string{"", "/a", "a/", "a/../b", `a\b`} {
		if _, err := NormalizeFilePath(raw); err == nil {
			t.Fatalf("NormalizeFilePath(%q) should fail", raw)
		}
	}
}

func TestProjectEntriesSkipsRootAndNestedObjects(t *testing.T) {
	modified := time.Now().UTC()
	page := &s3.ListPage{
		Prefixes: []string{"datasets/demo/2024/sub/", "other/prefix/"},
		Objects: []s3.Object{
			{Key: "datasets/demo/2024/", LastModified: modified},
			{Key: "datasets/demo/2024/sub/nested.txt", LastModified: modified},
			{Key: "datasets/demo/2024/b.parquet", Size: 7, LastModified: modified},
		},
	}
	entries := projectEntries(page, "datasets/demo/2024", "")
	if len(entries) != 2 {
		t.Fatalf("entries = %#v", entries)
	}
	if entries[0].Type != "dir" || entries[0].Path != "sub/" {
		t.Fatalf("dir = %#v", entries[0])
	}
	if entries[1].Type != "file" || entries[1].Path != "b.parquet" {
		t.Fatalf("file = %#v", entries[1])
	}
}

func TestParentPrefix(t *testing.T) {
	cases := map[string]string{
		"":       "",
		"a/":     "",
		"a/b/":   "a/",
		"a/b/c/": "a/b/",
	}
	for in, want := range cases {
		if got := parentPrefix(in); got != want {
			t.Fatalf("parentPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDatasetFilesListKeepsPathsRelativeToVersionRoot(t *testing.T) {
	store := &fakeStore{page: &s3.ListPage{
		Prefixes: []string{"demo/2024/figures/inner/"},
		Objects:  []s3.Object{{Key: "demo/2024/figures/sample.png", Size: 9}},
	}}
	svc := newFilesService(t, filesTestDataset("S3", "s3://datasets/demo/2024"), store)

	listing, found := svc.List(context.Background(), "demo", "", "figures/", "", 0)
	if !found {
		t.Fatal("version should exist")
	}
	// Paths stay rooted at the version so one entry's Path can be used directly
	// as the `path` of a preview or download request.
	want := map[string]string{"inner": "figures/inner/", "sample.png": "figures/sample.png"}
	if len(listing.Entries) != len(want) {
		t.Fatalf("entries = %+v", listing.Entries)
	}
	for _, entry := range listing.Entries {
		if want[entry.Name] != entry.Path {
			t.Fatalf("entry %q path = %q", entry.Name, entry.Path)
		}
	}
}

func TestDatasetFilesListSkipsDeeperObjects(t *testing.T) {
	// Stores that ignore the delimiter must not leak grandchildren into a
	// directory listing.
	store := &fakeStore{page: &s3.ListPage{
		Objects: []s3.Object{
			{Key: "demo/2024/figures/inner/deep.png", Size: 1},
			{Key: "demo/2024/figures/sample.png", Size: 9},
		},
	}}
	svc := newFilesService(t, filesTestDataset("S3", "s3://datasets/demo/2024"), store)

	listing, _ := svc.List(context.Background(), "demo", "", "figures/", "", 0)
	if len(listing.Entries) != 1 || listing.Entries[0].Path != "figures/sample.png" {
		t.Fatalf("entries = %+v", listing.Entries)
	}
}
