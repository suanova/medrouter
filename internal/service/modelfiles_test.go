package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/suanova/medrouter/internal/kube"
	"github.com/suanova/medrouter/internal/s3"
)

func modelVersionFixture(name, model, version, strategy, uri string) kube.ModelVersion {
	return kube.ModelVersion{
		Metadata: kube.ObjectMeta{Name: name, CreationTimestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		Spec: kube.ModelVersionSpec{
			Model:   model,
			Version: version,
			Storage: kube.ModelStorage{
				Strategy: strategy,
				S3:       &kube.S3Storage{URI: uri},
			},
		},
	}
}

func newModelFilesService(t *testing.T, versions []kube.ModelVersion, store ObjectStore) *FilesService {
	t.Helper()
	cfg := filesTestConfig()
	catalog := NewModelCatalog(cfg, &fakeSource{mvs: versions})
	return NewFilesService(cfg, catalog, store)
}

func TestModelFilesListProjectsRelativeEntries(t *testing.T) {
	modified := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	store := &fakeStore{page: &s3.ListPage{
		Prefixes: []string{"demo/2024/dense/"},
		Objects: []s3.Object{
			{Key: "demo/2024/", Size: 0, LastModified: modified},
			{Key: "demo/2024/config.json", Size: 42, LastModified: modified},
		},
	}}
	svc := newModelFilesService(t, []kube.ModelVersion{
		modelVersionFixture("demo-2024", "demo", "2024", "S3", "s3://models/demo/2024"),
	}, store)

	files, found := svc.List(context.Background(), "demo", "", "", "", 200)
	if !found || files == nil || !files.Available {
		t.Fatalf("files = %#v found=%v", files, found)
	}
	if files.Ref != "demo-2024" || files.Version != "2024" || files.Storage != "S3" {
		t.Fatalf("identity = %#v", files)
	}
	if store.gotBucket != "models" || store.gotPrefix != "demo/2024/" {
		t.Fatalf("store call bucket=%q prefix=%q", store.gotBucket, store.gotPrefix)
	}
	if len(files.Entries) != 2 {
		t.Fatalf("entries = %#v", files.Entries)
	}
	if files.Entries[0].Type != "dir" || files.Entries[0].Path != "dense/" {
		t.Fatalf("dir = %#v", files.Entries[0])
	}
	file := files.Entries[1]
	if file.Type != "file" || file.Path != "config.json" || file.Size == nil || *file.Size != 42 {
		t.Fatalf("file = %#v", file)
	}
}

func TestModelFilesListReasons(t *testing.T) {
	cases := []struct {
		name       string
		version    kube.ModelVersion
		store      ObjectStore
		wantReason string
	}{
		{
			name:       "host path is unsupported",
			version:    modelVersionFixture("demo-2024", "demo", "2024", "HostPath", ""),
			store:      &fakeStore{},
			wantReason: FilesUnsupportedStorage,
		},
		{
			name:       "missing uri",
			version:    modelVersionFixture("demo-2024", "demo", "2024", "S3", ""),
			store:      &fakeStore{},
			wantReason: FilesURIMissing,
		},
		{
			name:       "store disabled",
			version:    modelVersionFixture("demo-2024", "demo", "2024", "S3", "s3://models/demo/2024"),
			store:      nil,
			wantReason: FilesDisabled,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newModelFilesService(t, []kube.ModelVersion{tc.version}, tc.store)
			files, found := svc.List(context.Background(), "demo", "", "", "", 200)
			if !found || files == nil {
				t.Fatalf("found=%v files=%#v", found, files)
			}
			if files.Available || files.Reason != tc.wantReason {
				t.Fatalf("available=%v reason=%q want %q", files.Available, files.Reason, tc.wantReason)
			}
		})
	}
}

func TestModelFilesVersionSelection(t *testing.T) {
	v1 := modelVersionFixture("demo-2024", "demo", "2024", "S3", "s3://models/demo/2024")
	v2 := modelVersionFixture("demo-2025", "demo", "2025", "S3", "s3://models/demo/2025")
	v2.Metadata.CreationTimestamp = v2.Metadata.CreationTimestamp.Add(24 * time.Hour)
	store := &fakeStore{page: &s3.ListPage{}}
	svc := newModelFilesService(t, []kube.ModelVersion{v1, v2}, store)

	files, found := svc.List(context.Background(), "demo", "2024", "", "", 200)
	if !found || files.Ref != "demo-2024" || files.Version != "2024" {
		t.Fatalf("by spec.version = %#v found=%v", files, found)
	}
	files, found = svc.List(context.Background(), "demo", "demo-2025", "", "", 200)
	if !found || files.Ref != "demo-2025" || files.Version != "2025" {
		t.Fatalf("by ref = %#v found=%v", files, found)
	}
	files, found = svc.List(context.Background(), "demo", "", "", "", 200)
	if !found || files.Version != "2025" {
		t.Fatalf("latest = %#v found=%v", files, found)
	}
	if _, found := svc.List(context.Background(), "demo", "2049", "", "", 200); found {
		t.Fatal("unknown version should not be found")
	}
	if _, found := svc.List(context.Background(), "other", "", "", "", 200); found {
		t.Fatal("unknown model should not be found")
	}
}

func TestModelFilesOnUnavailableCatalog(t *testing.T) {
	cfg := filesTestConfig()
	catalog := NewModelCatalog(cfg, &fakeSource{mvErr: errors.New("cluster down")})
	svc := NewFilesService(cfg, catalog, &fakeStore{})

	files, found := svc.List(context.Background(), "demo", "", "", "", 200)
	if !found || files == nil {
		t.Fatalf("found=%v", found)
	}
	if files.Available || files.Reason != CatalogAPIError {
		t.Fatalf("files = %#v", files)
	}
}

func TestModelFilesDownload(t *testing.T) {
	store := &fakeStore{presignURL: "https://objects.example/x?sig=1"}
	svc := newModelFilesService(t, []kube.ModelVersion{
		modelVersionFixture("demo-2024", "demo", "2024", "S3", "s3://models/demo/2024"),
	}, store)

	download, found := svc.Download(context.Background(), "user-1", "demo", "", "config.json")
	if !found || download == nil || !download.Available {
		t.Fatalf("download = %#v found=%v", download, found)
	}
	if store.gotBucket != "models" || store.gotKey != "demo/2024/config.json" {
		t.Fatalf("store call bucket=%q key=%q", store.gotBucket, store.gotKey)
	}
	if download.URL != store.presignURL || download.ExpiresAt == nil {
		t.Fatalf("download = %#v", download)
	}
}
