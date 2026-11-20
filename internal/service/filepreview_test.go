package service

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/suanova/medrouter/internal/common"
	"github.com/suanova/medrouter/internal/config"
	"github.com/suanova/medrouter/internal/kube"
)

// newPreviewService builds a service with a tweaked configuration so tests can
// exercise limits without touching the environment.
func newPreviewService(t *testing.T, store ObjectStore, mutate func(*config.Config), opts ...FilesOption) *FilesService {
	t.Helper()
	cfg := filesTestConfig()
	if mutate != nil {
		mutate(cfg)
	}
	catalog := NewDatasetCatalog(cfg, &fakeSource{datasets: []kube.Dataset{
		filesTestDataset("S3", "s3://datasets/demo/2024"),
	}})
	return NewFilesService(cfg, catalog, store, opts...)
}

func TestPreviewReturnsTextWindowAndFlagsTruncation(t *testing.T) {
	const total = 1024 * 1024
	body := strings.Repeat("a", total)
	store := &fakeStore{objectSize: total, objectBody: []byte(body)}
	svc := newPreviewService(t, store, nil)

	preview, found := svc.Preview(context.Background(), "demo", "", "big.jsonl")
	if !found {
		t.Fatal("version should exist")
	}
	if !preview.Available || !preview.Previewable {
		t.Fatalf("preview = %+v", preview)
	}
	if preview.Kind != "text" || preview.Encoding != "utf-8" {
		t.Fatalf("kind/encoding = %q/%q", preview.Kind, preview.Encoding)
	}
	if int64(len(preview.Content)) != previewWindowForTest {
		t.Fatalf("content length = %d, want %d", len(preview.Content), previewWindowForTest)
	}
	if preview.WindowBytes != previewWindowForTest || !preview.Truncated {
		t.Fatalf("window/truncated = %d/%v", preview.WindowBytes, preview.Truncated)
	}
	if store.gotOffset != 0 || store.gotLength != previewWindowForTest {
		t.Fatalf("range = %d/%d", store.gotOffset, store.gotLength)
	}
}

// previewWindowForTest is the default window filesTestConfig produces.
const previewWindowForTest int64 = 256 << 10

func TestPreviewDoesNotTruncateSmallFiles(t *testing.T) {
	store := &fakeStore{objectSize: 12, objectBody: []byte("hello world\n")}
	svc := newPreviewService(t, store, nil)

	preview, _ := svc.Preview(context.Background(), "demo", "", "README.md")
	if !preview.Previewable || preview.Content != "hello world\n" || preview.Truncated {
		t.Fatalf("preview = %+v", preview)
	}
}

func TestPreviewClassifiesUnsupportedBinary(t *testing.T) {
	store := &fakeStore{objectSize: 5 << 30}
	svc := newPreviewService(t, store, nil)

	preview, _ := svc.Preview(context.Background(), "demo", "", "model.safetensors")
	if !preview.Available {
		t.Fatalf("the object exists, available should stay true: %+v", preview)
	}
	if preview.Previewable || preview.Kind != "unsupported" || preview.Reason != FilesPreviewUnsupported {
		t.Fatalf("preview = %+v", preview)
	}
	if store.rangeCalls != 0 {
		t.Fatal("an unsupported file must not be read")
	}
}

func TestPreviewRejectsOversizedImage(t *testing.T) {
	store := &fakeStore{objectSize: 11 << 20}
	svc := newPreviewService(t, store, nil)

	preview, _ := svc.Preview(context.Background(), "demo", "", "plot.png")
	if preview.Previewable || preview.Reason != FilesPreviewTooLarge || preview.Kind != "image" {
		t.Fatalf("preview = %+v", preview)
	}
}

func TestPreviewAllowsImageWithinLimit(t *testing.T) {
	store := &fakeStore{objectSize: 3 << 20}
	svc := newPreviewService(t, store, nil)

	preview, _ := svc.Preview(context.Background(), "demo", "", "plot.png")
	if !preview.Previewable || preview.Kind != "image" || preview.ContentType != "image/png" {
		t.Fatalf("preview = %+v", preview)
	}
}

func TestPreviewTreatsSVGAsTextNotImage(t *testing.T) {
	svg := `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`
	store := &fakeStore{objectSize: int64(len(svg)), objectBody: []byte(svg)}
	svc := newPreviewService(t, store, nil)

	preview, _ := svc.Preview(context.Background(), "demo", "", "logo.svg")
	if preview.Kind != "text" || !preview.Previewable {
		t.Fatalf("svg must be treated as text, got %+v", preview)
	}
}

func TestPreviewNonUTF8ContentIsUnsupported(t *testing.T) {
	store := &fakeStore{objectSize: 4, objectBody: []byte{0xff, 0xfe, 0x00, 0x01}}
	svc := newPreviewService(t, store, nil)

	preview, _ := svc.Preview(context.Background(), "demo", "", "data.txt")
	if preview.Previewable || preview.Reason != FilesPreviewUnsupported {
		t.Fatalf("preview = %+v", preview)
	}
}

func TestPreviewDisabledReportsReason(t *testing.T) {
	store := &fakeStore{objectSize: 4, objectBody: []byte("text")}
	svc := newPreviewService(t, store, func(cfg *config.Config) { cfg.S3PreviewEnabled = false })

	preview, _ := svc.Preview(context.Background(), "demo", "", "a.txt")
	if preview.Available || preview.Reason != FilesPreviewDisabled {
		t.Fatalf("preview = %+v", preview)
	}
}

func TestContentStreamsRangeAndReportsPartial(t *testing.T) {
	body := []byte("0123456789")
	store := &fakeStore{objectSize: int64(len(body)), objectBody: body}
	svc := newPreviewService(t, store, nil)

	content, reason, found := svc.Content(context.Background(), "user-1", "demo", "", "figure.png", 2, 3, false)
	if !found || reason != "" {
		t.Fatalf("found/reason = %v/%q", found, reason)
	}
	defer func() { _ = content.Body.Close() }()

	data, err := io.ReadAll(content.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "234" {
		t.Fatalf("body = %q", data)
	}
	if !content.Partial || content.Offset != 2 || content.Length != 3 || content.Size != 10 {
		t.Fatalf("content = %+v", content)
	}
	if store.gotOffset != 2 || store.gotLength != 3 {
		t.Fatalf("range = %d/%d", store.gotOffset, store.gotLength)
	}
}

func TestContentRefusesUnsupportedTypes(t *testing.T) {
	store := &fakeStore{objectSize: 10}
	svc := newPreviewService(t, store, nil)

	if _, reason, _ := svc.Content(context.Background(), "user-1", "demo", "", "weights.bin", 0, 0, false); reason != FilesPreviewUnsupported {
		t.Fatalf("reason = %q", reason)
	}
}

func TestDownloadRejectsFileAboveLimit(t *testing.T) {
	store := &fakeStore{objectSize: 100, presignURL: "https://example.com/x"}
	svc := newPreviewService(t, store, func(cfg *config.Config) { cfg.S3DownloadMax = 50 })

	download, _ := svc.Download(context.Background(), "user-1", "demo", "", "a.csv")
	if download.Available || download.Reason != FilesTooLarge || download.Retryable {
		t.Fatalf("download = %+v", download)
	}
	if store.presignCalls != 0 {
		t.Fatal("an oversized file must not be signed")
	}
}

func TestDownloadEnforcesQuota(t *testing.T) {
	store := &fakeStore{objectSize: 40, presignURL: "https://example.com/x"}
	svc := newPreviewService(t, store, nil, WithQuota(common.NewByteQuota(100, 24*time.Hour)))

	if download, _ := svc.Download(context.Background(), "user-1", "demo", "", "a.csv"); !download.Available {
		t.Fatalf("first download = %+v", download)
	}
	if download, _ := svc.Download(context.Background(), "user-1", "demo", "", "b.csv"); !download.Available {
		t.Fatalf("second download = %+v", download)
	}
	denied, _ := svc.Download(context.Background(), "user-1", "demo", "", "c.csv")
	if denied.Available || denied.Reason != FilesQuotaExceeded {
		t.Fatalf("third download = %+v", denied)
	}
	// Another user still has the full budget.
	if other, _ := svc.Download(context.Background(), "user-2", "demo", "", "a.csv"); !other.Available {
		t.Fatalf("other user = %+v", other)
	}
}

func TestDownloadUsesLongerTTLForLargeFiles(t *testing.T) {
	store := &fakeStore{objectSize: 3 << 30, presignURL: "https://example.com/x"}
	svc := newPreviewService(t, store, func(cfg *config.Config) {
		cfg.S3DownloadLargeThreshold = 1 << 30
		cfg.S3DownloadLargeTTL = time.Hour
	})

	download, _ := svc.Download(context.Background(), "user-1", "demo", "", "model.safetensors")
	if !download.Available || download.Mode != "presign" || download.Size != 3<<30 {
		t.Fatalf("download = %+v", download)
	}
	if store.gotTTL != time.Hour {
		t.Fatalf("ttl = %s", store.gotTTL)
	}
}

func TestDownloadProxyModeSkipsPresign(t *testing.T) {
	store := &fakeStore{objectSize: 10, presignURL: "https://example.com/x"}
	svc := newPreviewService(t, store, func(cfg *config.Config) { cfg.S3DownloadMode = "proxy" })

	download, _ := svc.Download(context.Background(), "user-1", "demo", "", "a.csv")
	if !download.Available || download.Mode != "proxy" || download.URL != "" {
		t.Fatalf("download = %+v", download)
	}
	if store.presignCalls != 0 {
		t.Fatal("proxy mode must not presign")
	}
}

func TestContentDownloadModeStreamsAnyType(t *testing.T) {
	body := []byte("BINARY")
	store := &fakeStore{objectSize: int64(len(body)), objectBody: body}
	svc := newPreviewService(t, store, nil)

	content, reason, found := svc.Content(context.Background(), "user-1", "demo", "", "weights.bin", 0, 0, true)
	if !found || reason != "" {
		t.Fatalf("found/reason = %v/%q", found, reason)
	}
	defer func() { _ = content.Body.Close() }()

	if !content.Attachment || content.ContentType != "application/octet-stream" {
		t.Fatalf("content = %+v", content)
	}
	data, err := io.ReadAll(content.Body)
	if err != nil || string(data) != "BINARY" {
		t.Fatalf("body = %q err=%v", data, err)
	}
}

func TestContentDownloadModeAppliesDownloadCap(t *testing.T) {
	store := &fakeStore{objectSize: 100}
	svc := newPreviewService(t, store, func(cfg *config.Config) { cfg.S3DownloadMax = 50 })

	if _, reason, _ := svc.Content(context.Background(), "user-1", "demo", "", "big.tar", 0, 0, true); reason != FilesTooLarge {
		t.Fatalf("reason = %q", reason)
	}
}
