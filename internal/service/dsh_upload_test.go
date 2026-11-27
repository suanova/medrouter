package service

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func dshUploadTestConfig(t *testing.T, maxBytes int64, maxFiles int) DshUploadConfig {
	t.Helper()
	root := filepath.Join(t.TempDir(), "dsh")
	return DshUploadConfig{
		Dir:      filepath.Join(root, dshUploadsDirName),
		Mount:    "/data/uploads",
		MaxBytes: maxBytes,
		MaxFiles: maxFiles,
	}
}

func multipartHeader(t *testing.T, filename, content string) *multipart.FileHeader {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	req := httptest.NewRequest("POST", "/chat/uploads", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if err := req.ParseMultipartForm(1 << 20); err != nil {
		t.Fatalf("ParseMultipartForm: %v", err)
	}
	return req.MultipartForm.File["file"][0]
}

func TestSaveDshAttachmentStoresInsideUserDir(t *testing.T) {
	upload := dshUploadTestConfig(t, 1<<20, 5)
	attachment, err := SaveDshAttachment(upload, 42, multipartHeader(t, "报告.pdf", "pdf-bytes"))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if attachment.Name != "报告.pdf" {
		t.Fatalf("name = %q", attachment.Name)
	}
	if !strings.HasPrefix(attachment.Path, "/data/uploads/42/") {
		t.Fatalf("path = %q", attachment.Path)
	}
	stored := filepath.Join(upload.Dir, "42", filepath.Base(attachment.Path))
	if info, err := os.Stat(stored); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("stored file missing: %v", err)
	}
	if err := ValidateDshAttachments(upload, 42, []DshAttachment{attachment}); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateDshAttachmentsRejectsOtherUsersAndTraversal(t *testing.T) {
	upload := dshUploadTestConfig(t, 1<<20, 5)
	owned, err := SaveDshAttachment(upload, 1, multipartHeader(t, "notes.txt", "hi"))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	other, err := SaveDshAttachment(upload, 2, multipartHeader(t, "notes.txt", "hi"))
	if err != nil {
		t.Fatalf("Save other: %v", err)
	}

	cases := []struct {
		name   string
		userID int64
		path   string
	}{
		{name: "another user's file", userID: 1, path: other.Path},
		{name: "traversal out of the user dir", userID: 1, path: "/data/uploads/1/../../2/" + filepath.Base(other.Path)},
		{name: "host file", userID: 1, path: "/etc/passwd"},
		{name: "missing file", userID: 1, path: "/data/uploads/1/missing.pdf"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateDshAttachments(upload, tc.userID, []DshAttachment{{Path: tc.path}}); err == nil {
				t.Fatalf("expected rejection for %q", tc.path)
			}
		})
	}
	// The owner's own file still passes after all those rejections.
	if err := ValidateDshAttachments(upload, 1, []DshAttachment{owned}); err != nil {
		t.Fatalf("owner validation: %v", err)
	}
}

func TestSaveDshAttachmentEnforcesLimits(t *testing.T) {
	upload := dshUploadTestConfig(t, 8, 5)
	if _, err := SaveDshAttachment(upload, 1, multipartHeader(t, "big.txt", "more than eight bytes")); err == nil {
		t.Fatal("expected an oversize rejection")
	}
	if _, err := SaveDshAttachment(upload, 1, multipartHeader(t, "evil.exe", "x")); err == nil {
		t.Fatal("expected an unsupported-extension rejection")
	}
	if _, err := SaveDshAttachment(upload, 1, multipartHeader(t, "../../escape.txt", "x")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(upload.Dir, "1"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d", len(entries))
	}
	// The basename is kept, the separators are not.
	if strings.Contains(entries[0].Name(), "/") {
		t.Fatalf("stored name = %q", entries[0].Name())
	}
}

func TestCleanupDshUploadsHonoursAgeAndCap(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "7")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	now := time.Now()
	ages := map[string]time.Duration{
		"old-1.txt": 48 * time.Hour,
		"old-2.txt": 24 * time.Hour,
		"new-1.txt": time.Hour,
		"new-2.txt": time.Minute,
	}
	for name, age := range ages {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if err := os.Chtimes(path, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatalf("Chtimes: %v", err)
		}
	}
	// Unrelated files and directories must survive.
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "not-a-user"), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	removed, err := CleanupDshUploads(root, DshUploadRetention{Age: 30 * time.Hour, MaxFiles: 1}, now)
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	// old-1 exceeds the age, and everything after the newest one exceeds the cap.
	if removed != 3 {
		t.Fatalf("removed = %d, want 3", removed)
	}
	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(left) != 1 || left[0].Name() != "new-2.txt" {
		t.Fatalf("left = %v", left)
	}
	if _, err := os.Stat(filepath.Join(root, "keep.txt")); err != nil {
		t.Fatalf("unrelated file was removed: %v", err)
	}
}
