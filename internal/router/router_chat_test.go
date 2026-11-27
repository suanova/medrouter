package router_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/suanova/medrouter/internal/config"
	"github.com/suanova/medrouter/internal/model"
	"github.com/suanova/medrouter/internal/service"
)

// dshFake records what MedRouter forwarded to the agent runtime and answers
// the dsh-api task endpoints.
type dshFake struct {
	mu           sync.Mutex
	lastUser     string
	lastPrompt   string
	lastSession  string
	lastPreset   string
	lastModel    string
	lastProvider string
	cancelCalled bool
}

func (f *dshFake) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Record the identity on every call: polling and download must carry the
		// same user as the submit.
		if user := r.Header.Get("X-DSH-User-Id"); user != "" {
			f.mu.Lock()
			f.lastUser = user
			f.mu.Unlock()
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/tasks":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			f.lastPrompt, _ = body["prompt"].(string)
			f.lastSession, _ = body["sessionId"].(string)
			f.lastPreset, _ = body["preset"].(string)
			f.lastModel, _ = body["model"].(string)
			f.lastProvider, _ = body["provider"].(string)
			f.mu.Unlock()
			// Simulates a session that lived on a dsh pod which no longer
			// holds it (scale-in or restart).
			if f.lastSession == "sess-missing" {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"ok":false,"error":{"code":"unknown_session","message":"session sess-missing was not created through this API"}}`))
				return
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"ok":true,"data":{"taskId":"task-1","sessionId":"sess-1","status":"running"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/tasks/task-1":
			_, _ = w.Write([]byte(`{"ok":true,"data":{"taskId":"task-1","sessionId":"sess-1","status":"completed","reply":"done","artifacts":[{"key":"report.pdf","type":"file","mimeType":"application/pdf","name":"report.pdf"}]}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/tasks/task-1":
			f.mu.Lock()
			f.cancelCalled = true
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"ok":true,"data":{"taskId":"task-1","status":"cancel-requested"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/tasks/task-1/files/report.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Disposition", `attachment; filename="report.pdf"`)
			_, _ = w.Write([]byte("pdf-bytes"))
		default:
			http.NotFound(w, r)
		}
	})
}

func (f *dshFake) snapshot() (user, prompt, session, preset, model, provider string, canceled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastUser, f.lastPrompt, f.lastSession, f.lastPreset, f.lastModel, f.lastProvider, f.cancelCalled
}

func newChatTestApp(t *testing.T, fake *dshFake) *testApp {
	t.Helper()
	server := httptest.NewServer(fake.handler())
	t.Cleanup(server.Close)
	return newTestApp(t, func(cfg *config.Config) {
		cfg.DshEnabled = true
		cfg.DshBaseURL = server.URL
		cfg.DshAPIKey = "secret"
		cfg.DshDefaultPreset = "standard"
		cfg.DshMaxPromptChars = 10000
		cfg.DshProviderPrefix = "mr-"
		cfg.DshUploadRoot = filepath.Join(t.TempDir(), "dsh")
		cfg.DshUploadMount = "/data/uploads"
		cfg.DshUploadMaxBytes = 1 << 20
		cfg.DshUploadMaxFiles = 5
	})
}

// seedInference adds one deployment row the way the Studio reconciler would.
func seedInference(t *testing.T, ownerID int64, name, source, phase string) {
	t.Helper()
	row := model.Workspace{
		OwnerID:    ownerID,
		Kind:       service.WorkspaceInference,
		SourceKind: "model",
		SourceName: source,
		Name:       name,
		Namespace:  "medrouter-workspace",
		Phase:      phase,
		Matched:    "{}",
		Spec:       `{"modelRef":"` + source + `"}`,
	}
	if err := model.DB.Create(&row).Error; err != nil {
		t.Fatalf("seed inference %s: %v", name, err)
	}
}

// upload posts one multipart file through the authenticated session.
func (a *testApp) upload(filename, content string) (*http.Response, envelope) {
	a.t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		a.t.Fatalf("CreateFormFile: %v", err)
	}
	_, _ = part.Write([]byte(content))
	if err := writer.Close(); err != nil {
		a.t.Fatalf("close writer: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, a.server.URL+a.apiPath("/api/chat/uploads"), body)
	if err != nil {
		a.t.Fatalf("new upload request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Origin", a.cfg.PublicOrigin)
	resp, err := a.client.Do(req)
	if err != nil {
		a.t.Fatalf("upload: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		a.t.Fatalf("read upload response: %v", err)
	}
	var env envelope
	if len(raw) > 0 && strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		if err := json.Unmarshal(raw, &env); err != nil {
			a.t.Fatalf("decode upload envelope: %v (%s)", err, raw)
		}
	}
	return resp, env
}

func TestChatRequiresSignIn(t *testing.T) {
	app := newChatTestApp(t, &dshFake{})
	resp, env := app.request(http.MethodGet, "/api/chat/status", nil)
	app.mustCode(resp, env, http.StatusUnauthorized, "UNAUTHENTICATED", "chat status without a session")
}

func TestChatFlowScopesEveryCallToTheSignedInUser(t *testing.T) {
	fake := &dshFake{}
	app := newChatTestApp(t, fake)

	// Unauthenticated status is checked first so the route is proven closed.
	if resp, env := app.request(http.MethodGet, "/api/chat/status", nil); resp.StatusCode != http.StatusUnauthorized || env.Success {
		t.Fatalf("unauthenticated status: %d %+v", resp.StatusCode, env)
	}

	app.registerOrFail("alice", "alice@example.com", "Password1abc")
	if _, env := app.login("alice", "Password1abc"); !env.Success {
		t.Fatalf("login alice: %+v", env)
	}
	_, selfEnv := app.self()
	aliceID := decodeUser(t, selfEnv).ID

	// Alice has one ready deployment and one stopped one. Only the running
	// service may be offered and used.
	const aliceService = "biomistral-7b-u1-svc"
	seedInference(t, aliceID, aliceService, "biomistral-7b-medmcqa-u3-lora", model.WorkspaceRunning)
	seedInference(t, aliceID, "stopped-u1-svc", "stopped", model.WorkspaceStopped)

	_, env := app.request(http.MethodGet, "/api/chat/status", nil)
	app.mustSuccess(env, "chat status")
	var status struct {
		Enabled      bool   `json:"enabled"`
		DefaultModel string `json:"default_model"`
		Models       []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"models"`
		Uploads struct {
			Enabled bool `json:"enabled"`
		} `json:"uploads"`
	}
	if err := json.Unmarshal(env.Data, &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !status.Enabled || !status.Uploads.Enabled {
		t.Fatalf("status = %+v", status)
	}
	if len(status.Models) != 1 || status.Models[0].ID != aliceService || status.DefaultModel != aliceService {
		t.Fatalf("models = %+v, default = %q", status.Models, status.DefaultModel)
	}
	if status.Models[0].Name != "biomistral-7b-medmcqa-u3-lora" {
		t.Fatalf("display name = %q", status.Models[0].Name)
	}

	// Alice uploads one file and attaches it.
	_, uploadEnv := app.upload("deck.pdf", "pdf")
	app.mustSuccess(uploadEnv, "alice upload")
	var uploaded struct {
		Attachment struct {
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"attachment"`
	}
	if err := json.Unmarshal(uploadEnv.Data, &uploaded); err != nil {
		t.Fatalf("decode upload: %v", err)
	}
	if !strings.HasPrefix(uploaded.Attachment.Path, "/data/uploads/") || !strings.HasSuffix(uploaded.Attachment.Path, "deck.pdf") {
		t.Fatalf("attachment = %+v", uploaded.Attachment)
	}

	_, submitEnv := app.request(http.MethodPost, "/api/chat/tasks", map[string]any{
		"prompt":      "build a deck",
		"model":       aliceService,
		"attachments": []map[string]any{{"name": "deck.pdf", "path": uploaded.Attachment.Path}},
	})
	app.mustSuccess(submitEnv, "alice submit")
	user, prompt, _, preset, modelName, provider, _ := fake.snapshot()
	if user != jsonNumber(aliceID) {
		t.Fatalf("forwarded user = %q, want %d", user, aliceID)
	}
	if !strings.Contains(prompt, uploaded.Attachment.Path) || preset != "standard" {
		t.Fatalf("prompt = %q preset = %q", prompt, preset)
	}
	if modelName != aliceService || provider != "mr-"+aliceService {
		t.Fatalf("forwarded model/provider = %q/%q", modelName, provider)
	}

	// A model outside the caller's running set is rejected before it reaches
	// the runtime: unknown, stopped, or another account's.
	for _, bad := range []string{"not-a-model", "stopped-u1-svc"} {
		_, badEnv := app.request(http.MethodPost, "/api/chat/tasks", map[string]any{
			"prompt": "hi",
			"model":  bad,
		})
		if badEnv.Success || badEnv.Code != "MODEL_NOT_AVAILABLE" {
			t.Fatalf("model %q: %+v", bad, badEnv)
		}
	}
	_, missingEnv := app.request(http.MethodPost, "/api/chat/tasks", map[string]any{"prompt": "hi"})
	if missingEnv.Success || missingEnv.Code != "INVALID_REQUEST" {
		t.Fatalf("missing model: %+v", missingEnv)
	}

	// Polling and artifact download use the same identity.
	_, taskEnv := app.request(http.MethodGet, "/api/chat/tasks/task-1", nil)
	app.mustSuccess(taskEnv, "alice poll")
	if !bytes.Contains(taskEnv.Data, []byte(`"reply":"done"`)) {
		t.Fatalf("task = %s", taskEnv.Data)
	}
	resp, err := app.client.Get(app.server.URL + app.apiPath("/api/chat/tasks/task-1/files/report.pdf"))
	if err != nil {
		t.Fatalf("artifact: %v", err)
	}
	artifactBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(artifactBody) != "pdf-bytes" {
		t.Fatalf("artifact status=%d body=%q", resp.StatusCode, artifactBody)
	}
	if resp.Header.Get("Content-Disposition") == "" {
		t.Fatal("artifact response lost the download filename")
	}

	// Cancel reaches the runtime.
	_, cancelEnv := app.request(http.MethodDelete, "/api/chat/tasks/task-1", nil)
	app.mustSuccess(cancelEnv, "alice cancel")
	if _, _, _, _, _, _, canceled := fake.snapshot(); !canceled {
		t.Fatal("cancel did not reach dsh")
	}

	// Bob cannot attach Alice's file, use her model, or read her task.
	app.registerOrFail("bob", "bob@example.com", "Password1abc")
	if _, env := app.login("bob", "Password1abc"); !env.Success {
		t.Fatalf("login bob: %+v", env)
	}
	_, bobSelfEnv := app.self()
	bobID := decodeUser(t, bobSelfEnv).ID
	const bobService = "qwen3-27b-u2-svc"
	seedInference(t, bobID, bobService, "qwen3-27b", model.WorkspaceRunning)

	_, bobStatusEnv := app.request(http.MethodGet, "/api/chat/status", nil)
	app.mustSuccess(bobStatusEnv, "bob status")
	var bobStatus struct {
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
	}
	if err := json.Unmarshal(bobStatusEnv.Data, &bobStatus); err != nil {
		t.Fatalf("decode bob status: %v", err)
	}
	if len(bobStatus.Models) != 1 || bobStatus.Models[0].ID != bobService {
		t.Fatalf("bob models = %+v", bobStatus.Models)
	}

	_, stolenEnv := app.request(http.MethodPost, "/api/chat/tasks", map[string]any{
		"prompt":      "steal",
		"model":       aliceService,
		"attachments": []map[string]any{{"path": uploaded.Attachment.Path}},
	})
	if stolenEnv.Success || stolenEnv.Code != "INVALID_ATTACHMENT" {
		t.Fatalf("bob attaching alice's file: %+v", stolenEnv)
	}
	if user, _, _, _, _, _, _ := fake.snapshot(); user != jsonNumber(aliceID) {
		t.Fatalf("a rejected submit must not forward a bad identity: %q", user)
	}

	_, bobUploadEnv := app.upload("notes.txt", "hi")
	app.mustSuccess(bobUploadEnv, "bob upload")
	_, bobSubmitEnv := app.request(http.MethodPost, "/api/chat/tasks", map[string]any{
		"prompt": "hello",
		"model":  bobService,
	})
	app.mustSuccess(bobSubmitEnv, "bob submit")
	user, _, _, _, modelName, provider, _ = fake.snapshot()
	if user != jsonNumber(bobID) || modelName != bobService || provider != "mr-"+bobService {
		t.Fatalf("bob submit forwarded user/model/provider = %q/%q/%q", user, modelName, provider)
	}

	// The runtime catalog endpoint is for the dsh container: shared key only,
	// and it lists every running service, not one user's.
	if resp, env := app.request(http.MethodGet, "/api/chat/runtime/models", nil); resp.StatusCode != http.StatusUnauthorized || env.Success {
		t.Fatalf("catalog without a key: %d %+v", resp.StatusCode, env)
	}
	resp, env = app.request(http.MethodGet, "/api/chat/runtime/models", nil, func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer wrong")
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("catalog with a wrong key: %d", resp.StatusCode)
	}
	_, catalogEnv := app.request(http.MethodGet, "/api/chat/runtime/models", nil, func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer secret")
	})
	app.mustSuccess(catalogEnv, "catalog")
	var catalog struct {
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
	}
	if err := json.Unmarshal(catalogEnv.Data, &catalog); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if len(catalog.Models) != 2 {
		t.Fatalf("catalog = %+v", catalog.Models)
	}
	for _, entry := range catalog.Models {
		if entry.ID == "stopped-u1-svc" {
			t.Fatalf("catalog must not include stopped services: %+v", catalog.Models)
		}
	}
}

func TestChatSubmitReportsExpiredSession(t *testing.T) {
	app := newChatTestApp(t, &dshFake{})
	app.registerOrFail("dave", "dave@example.com", "Password1abc")
	if _, env := app.login("dave", "Password1abc"); !env.Success {
		t.Fatalf("login dave: %+v", env)
	}
	_, selfEnv := app.self()
	seedInference(t, decodeUser(t, selfEnv).ID, "dave-u3-svc", "dave-model", model.WorkspaceRunning)

	resp, env := app.request(http.MethodPost, "/api/chat/tasks", map[string]any{
		"prompt":    "continue",
		"model":     "dave-u3-svc",
		"sessionId": "sess-missing",
	})
	// The page clears the stored session on this code and starts a new one.
	app.mustCode(resp, env, http.StatusNotFound, "SESSION_EXPIRED", "expired session submit")

	// Without a session id the same prompt starts a fresh session.
	_, fresh := app.request(http.MethodPost, "/api/chat/tasks", map[string]any{
		"prompt": "start over",
		"model":  "dave-u3-svc",
	})
	app.mustSuccess(fresh, "fresh submit")
}

func TestChatUploadRejectsWrongContentType(t *testing.T) {
	app := newChatTestApp(t, &dshFake{})
	app.registerOrFail("carol", "carol@example.com", "Password1abc")
	if _, env := app.login("carol", "Password1abc"); !env.Success {
		t.Fatalf("login carol: %+v", env)
	}
	resp, env := app.request(http.MethodPost, "/api/chat/uploads", map[string]any{"file": "x"})
	app.mustCode(resp, env, http.StatusUnsupportedMediaType, "INVALID_REQUEST", "JSON body on the upload route")
}

func jsonNumber(id int64) string {
	raw, _ := json.Marshal(id)
	return string(raw)
}
