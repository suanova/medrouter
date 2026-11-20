package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/suanova/medrouter/internal/config"
	"github.com/suanova/medrouter/internal/kube"
	"github.com/suanova/medrouter/internal/service"
)

// previewRouter registers the preview and content endpoints for a dataset.
func previewRouter(store service.ObjectStore) *gin.Engine {
	cfg := &config.Config{
		DatasetLibraryCacheTTL: time.Minute,
		DatasetLibraryTimeout:  time.Second,
		S3PresignTTL:           10 * time.Minute,
		S3Timeout:              time.Second,
		S3PreviewEnabled:       true,
		S3PreviewTextWindow:    256 << 10,
		S3PreviewImageMax:      10 << 20,
		S3PreviewPDFMax:        32 << 20,
	}
	catalog := service.NewDatasetCatalog(cfg, datasetSource{datasets: []kube.Dataset{demoDataset()}})
	controller := NewDatasetController(catalog, service.NewFilesService(cfg, catalog, store))
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/datasets/:name/files/preview", controller.Preview)
	engine.GET("/datasets/:name/files/content", controller.Content)
	return engine
}

// doRawRequest returns the recorder so tests can inspect binary responses.
func doRawRequest(t *testing.T, engine *gin.Engine, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	engine.ServeHTTP(rec, req)
	return rec
}

func TestPreviewEndpointReturnsText(t *testing.T) {
	body := "line one\nline two\n"
	engine := previewRouter(stubStore{objectSize: int64(len(body)), objectBody: body})

	status, envelope := doRequest(t, engine, "/datasets/demo/files/preview?path=README.md")
	if status != http.StatusOK || envelope["success"] != true {
		t.Fatalf("status=%d body=%v", status, envelope)
	}
	data := envelope["data"].(map[string]any)
	if data["kind"] != "text" || data["previewable"] != true || data["content"] != body {
		t.Fatalf("data = %v", data)
	}
	if data["truncated"] != false {
		t.Fatalf("truncated = %v", data["truncated"])
	}
}

func TestPreviewEndpointMarksUnsupportedFormat(t *testing.T) {
	engine := previewRouter(stubStore{objectSize: 5 << 30})

	status, envelope := doRequest(t, engine, "/datasets/demo/files/preview?path=model.safetensors")
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%v", status, envelope)
	}
	data := envelope["data"].(map[string]any)
	if data["available"] != true || data["previewable"] != false {
		t.Fatalf("data = %v", data)
	}
	if data["reason"] != service.FilesPreviewUnsupported {
		t.Fatalf("reason = %v", data["reason"])
	}
}

func TestPreviewEndpointPointsImageAtContent(t *testing.T) {
	engine := previewRouter(stubStore{objectSize: 7, objectBody: "PNGDATA"})

	_, envelope := doRequest(t, engine, "/datasets/demo/files/preview?path=figures/plot.png")
	data := envelope["data"].(map[string]any)
	if data["kind"] != "image" || data["previewable"] != true {
		t.Fatalf("data = %v", data)
	}
	contentURL, _ := data["content_url"].(string)
	if !strings.HasSuffix(strings.SplitN(contentURL, "?", 2)[0], "/files/content") {
		t.Fatalf("content_url = %q", contentURL)
	}
	if !strings.Contains(contentURL, "path=figures") {
		t.Fatalf("content_url lost the path: %q", contentURL)
	}
}

func TestContentEndpointStreamsWholeObject(t *testing.T) {
	engine := previewRouter(stubStore{objectSize: 7, objectBody: "PNGDATA"})

	rec := doRawRequest(t, engine, "/datasets/demo/files/content?path=plot.png", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("content type = %q", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Content-Disposition") != "inline" || rec.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatalf("headers = %v", rec.Header())
	}
	if rec.Body.String() != "PNGDATA" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestContentEndpointHonoursRange(t *testing.T) {
	engine := previewRouter(stubStore{objectSize: 7, objectBody: "PNGDATA"})

	rec := doRawRequest(t, engine, "/datasets/demo/files/content?path=plot.png",
		map[string]string{"Range": "bytes=2-4"})
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Range"); got != "bytes 2-4/7" {
		t.Fatalf("content-range = %q", got)
	}
	if rec.Body.String() != "GDA" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestContentEndpointRejectsUnsupportedType(t *testing.T) {
	engine := previewRouter(stubStore{objectSize: 10})

	rec := doRawRequest(t, engine, "/datasets/demo/files/content?path=weights.bin", nil)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestContentEndpointRejectsPathTraversal(t *testing.T) {
	engine := previewRouter(stubStore{objectSize: 10, objectBody: "0123456789"})

	rec := doRawRequest(t, engine, "/datasets/demo/files/content?path=../../etc/passwd", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}
