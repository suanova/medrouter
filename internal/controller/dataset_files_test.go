package controller

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/suanova/medrouter/internal/config"
	"github.com/suanova/medrouter/internal/kube"
	"github.com/suanova/medrouter/internal/s3"
	"github.com/suanova/medrouter/internal/service"
)

type datasetSource struct {
	datasets []kube.Dataset
	err      error
}

func (s datasetSource) ListModelVersions(context.Context) ([]kube.ModelVersion, error) {
	return []kube.ModelVersion{}, nil
}
func (s datasetSource) ListRuntimeProfiles(context.Context) ([]kube.RuntimeProfile, error) {
	return []kube.RuntimeProfile{}, nil
}
func (s datasetSource) ListDatasets(context.Context) ([]kube.Dataset, error) {
	return s.datasets, s.err
}

type stubStore struct {
	page       *s3.ListPage
	headErr    error
	presignURL string
}

func (s stubStore) List(context.Context, string, string, string, string, int32) (*s3.ListPage, error) {
	return s.page, nil
}
func (s stubStore) Head(context.Context, string, string) (s3.Object, error) {
	return s3.Object{}, s.headErr
}
func (s stubStore) PresignGet(context.Context, string, string, time.Duration) (string, error) {
	return s.presignURL, nil
}

func demoDataset() kube.Dataset {
	return kube.Dataset{
		Metadata: kube.ObjectMeta{Name: "demo-2024", CreationTimestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		Spec: kube.DatasetSpec{
			Dataset: "demo",
			Version: "2024",
			Storage: kube.ModelStorage{Strategy: "S3", S3: &kube.S3Storage{URI: "s3://datasets/demo/2024"}},
		},
	}
}

func filesRouter(source kube.Source, store service.ObjectStore) (*gin.Engine, *service.DatasetCatalog) {
	cfg := &config.Config{
		DatasetLibraryCacheTTL: time.Minute,
		DatasetLibraryTimeout:  time.Second,
		S3PresignTTL:           10 * time.Minute,
		S3ListCacheTTL:         0,
		S3Timeout:              time.Second,
	}
	catalog := service.NewDatasetCatalog(cfg, source)
	controller := NewDatasetController(catalog, service.NewDatasetFilesService(cfg, catalog, store))
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/datasets/:name/files", controller.Files)
	engine.GET("/datasets/:name/files/download", controller.Download)
	return engine, catalog
}

func TestDatasetFilesEndpointListsEntries(t *testing.T) {
	store := stubStore{page: &s3.ListPage{
		Prefixes: []string{"demo/2024/sub/"},
		Objects:  []s3.Object{{Key: "demo/2024/a.csv", Size: 5}},
	}}
	engine, _ := filesRouter(datasetSource{datasets: []kube.Dataset{demoDataset()}}, store)

	status, body := doRequest(t, engine, "/datasets/demo/files")
	if status != http.StatusOK || body["success"] != true {
		t.Fatalf("status=%d body=%v", status, body)
	}
	data := body["data"].(map[string]any)
	if data["available"] != true || data["version"] != "2024" {
		t.Fatalf("data = %v", data)
	}
	entries, ok := data["entries"].([]any)
	if !ok || len(entries) != 2 {
		t.Fatalf("entries = %v", data["entries"])
	}
	first := entries[0].(map[string]any)
	if first["type"] != "dir" || first["path"] != "sub/" {
		t.Fatalf("dir entry = %v", first)
	}
}

func TestDatasetFilesEndpointUnavailableCatalog(t *testing.T) {
	engine, _ := filesRouter(datasetSource{err: errors.New("down")}, stubStore{})

	status, body := doRequest(t, engine, "/datasets/demo/files")
	if status != http.StatusOK || body["success"] != true {
		t.Fatalf("status=%d body=%v", status, body)
	}
	data := body["data"].(map[string]any)
	if data["available"] != false || data["reason"] != service.CatalogAPIError {
		t.Fatalf("data = %v", data)
	}
}

func TestDatasetFilesEndpointNotFound(t *testing.T) {
	engine, _ := filesRouter(datasetSource{}, stubStore{})
	status, body := doRequest(t, engine, "/datasets/missing/files")
	if status != http.StatusNotFound || body["code"] != "DATASET_VERSION_NOT_FOUND" {
		t.Fatalf("status=%d body=%v", status, body)
	}
}

func TestDatasetFilesDisabledController(t *testing.T) {
	cfg := &config.Config{DatasetLibraryCacheTTL: time.Minute, DatasetLibraryTimeout: time.Second}
	catalog := service.NewDatasetCatalog(cfg, kube.DisabledSource)
	controller := NewDatasetController(catalog, nil)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/datasets/:name/files", controller.Files)

	status, body := doRequest(t, engine, "/datasets/demo/files")
	if status != http.StatusOK || body["success"] != true {
		t.Fatalf("status=%d body=%v", status, body)
	}
	data := body["data"].(map[string]any)
	if data["available"] != false || data["reason"] != service.FilesDisabled {
		t.Fatalf("data = %v", data)
	}
}

func TestDatasetDownloadEndpoint(t *testing.T) {
	engine, _ := filesRouter(datasetSource{datasets: []kube.Dataset{demoDataset()}}, stubStore{
		presignURL: "https://objects.example/demo/a.csv?sig=1",
	})

	status, body := doRequest(t, engine, "/datasets/demo/files/download?path=a.csv")
	if status != http.StatusOK || body["success"] != true {
		t.Fatalf("status=%d body=%v", status, body)
	}
	data := body["data"].(map[string]any)
	if data["available"] != true || data["url"] != "https://objects.example/demo/a.csv?sig=1" {
		t.Fatalf("data = %v", data)
	}
	if _, ok := data["expires_at"].(string); !ok {
		t.Fatalf("expires_at = %v", data["expires_at"])
	}
}

func TestDatasetDownloadRejectsInvalidPath(t *testing.T) {
	engine, _ := filesRouter(datasetSource{datasets: []kube.Dataset{demoDataset()}}, stubStore{})
	status, body := doRequest(t, engine, "/datasets/demo/files/download?path=../secret")
	if status != http.StatusBadRequest || body["code"] != service.FilesInvalidPath {
		t.Fatalf("status=%d body=%v", status, body)
	}
}

func TestDatasetDownloadMissingObject(t *testing.T) {
	engine, _ := filesRouter(datasetSource{datasets: []kube.Dataset{demoDataset()}}, stubStore{
		headErr: &s3.Error{Reason: s3.ReasonNotFound},
	})
	status, body := doRequest(t, engine, "/datasets/demo/files/download?path=missing.csv")
	if status != http.StatusNotFound || body["code"] != "FILE_NOT_FOUND" {
		t.Fatalf("status=%d body=%v", status, body)
	}
}
