package controller

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/suanova/medrouter/internal/config"
	"github.com/suanova/medrouter/internal/kube"
	"github.com/suanova/medrouter/internal/s3"
	"github.com/suanova/medrouter/internal/service"
)

type modelSource struct {
	mvs []kube.ModelVersion
	err error
}

func (s modelSource) ListModelVersions(context.Context) ([]kube.ModelVersion, error) {
	return s.mvs, s.err
}
func (s modelSource) ListRuntimeProfiles(context.Context) ([]kube.RuntimeProfile, error) {
	return []kube.RuntimeProfile{}, nil
}
func (s modelSource) ListDatasets(context.Context) ([]kube.Dataset, error) {
	return []kube.Dataset{}, nil
}

func demoModelVersion() kube.ModelVersion {
	return kube.ModelVersion{
		Metadata: kube.ObjectMeta{Name: "demo-2024", CreationTimestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		Spec: kube.ModelVersionSpec{
			Model:   "demo",
			Version: "2024",
			Storage: kube.ModelStorage{Strategy: "S3", S3: &kube.S3Storage{URI: "s3://models/demo/2024"}},
		},
	}
}

func modelFilesRouter(source kube.Source, store service.ObjectStore) *gin.Engine {
	cfg := &config.Config{
		ModelLibraryCacheTTL: time.Minute,
		ModelLibraryTimeout:  time.Second,
		S3PresignTTL:         10 * time.Minute,
		S3ListCacheTTL:       0,
		S3Timeout:            time.Second,
	}
	catalog := service.NewModelCatalog(cfg, source)
	controller := NewModelController(catalog, service.NewFilesService(cfg, catalog, store))
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/models/:name/files", controller.Files)
	engine.GET("/models/:name/files/download", controller.Download)
	return engine
}

func TestModelFilesEndpointListsEntries(t *testing.T) {
	store := stubStore{page: &s3.ListPage{
		Prefixes: []string{"demo/2024/dense/"},
		Objects:  []s3.Object{{Key: "demo/2024/config.json", Size: 42}},
	}}
	engine := modelFilesRouter(modelSource{mvs: []kube.ModelVersion{demoModelVersion()}}, store)

	status, body := doRequest(t, engine, "/models/demo/files")
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
	if first["type"] != "dir" || first["path"] != "dense/" {
		t.Fatalf("dir entry = %v", first)
	}
}

func TestModelFilesEndpointNotFound(t *testing.T) {
	engine := modelFilesRouter(modelSource{}, stubStore{})
	status, body := doRequest(t, engine, "/models/missing/files")
	if status != http.StatusNotFound || body["code"] != "MODEL_VERSION_NOT_FOUND" {
		t.Fatalf("status=%d body=%v", status, body)
	}
}

func TestModelDownloadRejectsInvalidPath(t *testing.T) {
	engine := modelFilesRouter(modelSource{mvs: []kube.ModelVersion{demoModelVersion()}}, stubStore{})
	status, body := doRequest(t, engine, "/models/demo/files/download?path=../secret")
	if status != http.StatusBadRequest || body["code"] != service.FilesInvalidPath {
		t.Fatalf("status=%d body=%v", status, body)
	}
}

func TestModelDownloadMissingObject(t *testing.T) {
	engine := modelFilesRouter(modelSource{mvs: []kube.ModelVersion{demoModelVersion()}}, stubStore{
		headErr: &s3.Error{Reason: s3.ReasonNotFound},
	})
	status, body := doRequest(t, engine, "/models/demo/files/download?path=missing.bin")
	if status != http.StatusNotFound || body["code"] != "FILE_NOT_FOUND" {
		t.Fatalf("status=%d body=%v", status, body)
	}
}
