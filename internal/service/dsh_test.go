package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/suanova/medrouter/internal/config"
)

func dshTestConfig(baseURL string) *config.Config {
	return &config.Config{
		DshEnabled:        true,
		DshBaseURL:        baseURL,
		DshAPIKey:         "test-key",
		DshRequestTimeout: 2 * time.Second,
	}
}

func TestDshClientSubmitForwardsIdentityAndOptions(t *testing.T) {
	var gotUser, gotKey string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		gotUser = r.Header.Get("X-DSH-User-Id")
		gotKey = r.Header.Get("X-API-Key")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"ok":true,"data":{"taskId":"task-1","sessionId":"sess-1","status":"running"}}`))
	}))
	defer server.Close()

	client := NewDshClient(dshTestConfig(server.URL))
	task, err := client.Submit(context.Background(), 42, DshSubmitInput{
		Prompt:    "make a deck",
		SessionID: "sess-0",
		Preset:    "standard",
		Model:     "deepseek-v4-pro",
		Provider:  "mr-deepseek-v4-pro",
		Attachments: []DshAttachment{
			{Name: "report.pdf", Path: "/data/uploads/42/abc-report.pdf"},
		},
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if task.TaskID != "task-1" || task.SessionID != "sess-1" {
		t.Fatalf("task = %+v", task)
	}
	// User isolation is carried by the identity header, never by the body.
	if gotUser != "42" {
		t.Fatalf("X-DSH-User-Id = %q", gotUser)
	}
	if gotKey != "test-key" {
		t.Fatalf("X-API-Key = %q", gotKey)
	}
	if gotBody["sessionId"] != "sess-0" || gotBody["preset"] != "standard" {
		t.Fatalf("body = %+v", gotBody)
	}
	if gotBody["model"] != "deepseek-v4-pro" || gotBody["provider"] != "mr-deepseek-v4-pro" {
		t.Fatalf("model/provider = %v/%v", gotBody["model"], gotBody["provider"])
	}
	prompt, _ := gotBody["prompt"].(string)
	if !strings.Contains(prompt, "[Uploaded files]") || !strings.Contains(prompt, "/data/uploads/42/abc-report.pdf (report.pdf)") {
		t.Fatalf("prompt = %q", prompt)
	}
	if _, leaked := gotBody["userId"]; leaked {
		t.Fatalf("userId must not be sent in the body")
	}
}

func TestDshClientMapsErrorEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"unknown_session","message":"session nope was not created through this API"}}`))
	}))
	defer server.Close()

	client := NewDshClient(dshTestConfig(server.URL))
	_, err := client.Task(context.Background(), 7, "task-9")
	var dshErr *DshError
	if !errors.As(err, &dshErr) {
		t.Fatalf("err = %v, want DshError", err)
	}
	if dshErr.Status != http.StatusNotFound || dshErr.Code != "unknown_session" {
		t.Fatalf("dshErr = %+v", dshErr)
	}
	if !strings.Contains(dshErr.Error(), "unknown_session") {
		t.Fatalf("message = %q", dshErr.Error())
	}
}

func TestDshClientRejectsUnsafeIdentifiers(t *testing.T) {
	client := NewDshClient(dshTestConfig("http://127.0.0.1:1"))
	if _, err := client.Task(context.Background(), 1, "../tasks"); err == nil {
		t.Fatal("expected a rejection for a task id containing a path separator")
	}
	if _, err := client.DownloadArtifact(context.Background(), 1, "task-1", "/etc/passwd"); err == nil {
		t.Fatal("expected a rejection for an unsafe artifact key")
	}
}

func TestDshClientUnreachableIsTyped(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	baseURL := server.URL
	server.Close()

	client := NewDshClient(dshTestConfig(baseURL))
	_, err := client.Task(context.Background(), 1, "task-1")
	var unavailable *DshUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("err = %v, want DshUnavailableError", err)
	}
}

func TestDshClientDownloadsArtifact(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/task-1/files/report.pdf" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("X-DSH-User-Id") != "5" {
			t.Errorf("user header = %q", r.Header.Get("X-DSH-User-Id"))
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="report.pdf"`)
		_, _ = w.Write([]byte("pdf"))
	}))
	defer server.Close()

	client := NewDshClient(dshTestConfig(server.URL))
	resp, err := client.DownloadArtifact(context.Background(), 5, "task-1", "report.pdf")
	if err != nil {
		t.Fatalf("DownloadArtifact: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "pdf" || resp.Header.Get("Content-Type") != "application/pdf" {
		t.Fatalf("body=%q type=%q", body, resp.Header.Get("Content-Type"))
	}
}

func TestDshPodIndexIsStablePerUser(t *testing.T) {
	for userID := int64(1); userID <= 50; userID++ {
		first := dshPodIndex(userID, 4)
		if first < 0 || first >= 4 {
			t.Fatalf("index %d out of range", first)
		}
		for range 5 {
			if again := dshPodIndex(userID, 4); again != first {
				t.Fatalf("user %d moved from %d to %d", userID, first, again)
			}
		}
	}
	if dshPodIndex(7, 1) != 0 {
		t.Fatal("a single pod must always be index 0")
	}
}

func TestDshPodResolverCachesLookups(t *testing.T) {
	lookups := 0
	resolver := &dshPodResolver{
		host: "dsh-headless.test.svc",
		port: "3080",
		ttl:  time.Hour,
		lookup: func(context.Context, string) ([]string, error) {
			lookups++
			return []string{"10.0.0.2", "10.0.0.1"}, nil
		},
	}
	first := resolver.hosts(context.Background())
	second := resolver.hosts(context.Background())
	if lookups != 1 {
		t.Fatalf("lookups = %d, want 1", lookups)
	}
	if len(first) != 2 || first[0] != "10.0.0.1" {
		t.Fatalf("hosts = %v (want sorted)", first)
	}
	if len(second) != 2 {
		t.Fatalf("second = %v", second)
	}
	resolver.lookup = func(context.Context, string) ([]string, error) {
		return nil, errors.New("boom")
	}
	resolver.expires = time.Time{}
	if cached := resolver.hosts(context.Background()); len(cached) != 2 {
		t.Fatalf("a lookup failure should keep the last set, got %v", cached)
	}
}
