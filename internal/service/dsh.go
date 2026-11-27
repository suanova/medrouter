package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/suanova/medrouter/internal/config"
)

// DeepSeek Harness (dsh) task states reported by dsh-api. The gateway forwards
// them as-is; the UI decides whether a turn counts as done or failed.
const (
	DshStatusRunning   = "running"
	DshStatusCompleted = "completed"
	DshStatusMaxTokens = "max-tokens"
	DshStatusCanceled  = "canceled"
	DshStatusFailed    = "failed"
	DshStatusTimeout   = "timeout"
)

const (
	dshTasksPath      = "/api/v1/tasks"
	dshHealthPath     = "/api/v1/health"
	dshEndpointTTL    = 30 * time.Second
	dshDefaultPort    = "3080"
	dshMaxAttachments = 10
)

// dshIdentifierPattern is the alphabet dsh-api uses for task ids and artifact
// keys. Rejecting anything else keeps a client-supplied id from changing the
// upstream path.
var dshIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{1,200}$`)

// DshError is an error envelope returned by dsh-api. Status is the upstream
// HTTP status; Code and Message come from {ok:false,error:{code,message}}.
type DshError struct {
	Status  int
	Code    string
	Message string
}

func (e *DshError) Error() string {
	if e == nil {
		return ""
	}
	switch {
	case e.Code != "" && e.Message != "":
		return "dsh " + e.Code + ": " + e.Message
	case e.Message != "":
		return "dsh: " + e.Message
	case e.Code != "":
		return "dsh " + e.Code
	default:
		return fmt.Sprintf("dsh returned HTTP %d", e.Status)
	}
}

// DshUnavailableError means the runtime could not be reached at all (DNS,
// connection, timeout) or rejected the gateway's API key. It should surface as
// a 502/503 rather than as a user-correctable request error.
type DshUnavailableError struct{ Err error }

func (e *DshUnavailableError) Error() string {
	if e == nil || e.Err == nil {
		return "dsh is unavailable"
	}
	return "dsh is unavailable: " + e.Err.Error()
}

func (e *DshUnavailableError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// DshTask is the subset of dsh-api's task summary MedRouter needs. Transcript
// and tool calls stay raw so the UI can render whatever the harness emits
// without the gateway having to track its event schema.
type DshTask struct {
	TaskID       string            `json:"taskId"`
	SessionID    string            `json:"sessionId"`
	Status       string            `json:"status"`
	Continuation bool              `json:"continuation"`
	CreatedAt    int64             `json:"createdAt"`
	StartedAt    *int64            `json:"startedAt"`
	FinishedAt   *int64            `json:"finishedAt"`
	Preset       string            `json:"preset"`
	Provider     string            `json:"provider"`
	Model        string            `json:"model"`
	Prompt       string            `json:"prompt"`
	Reply        string            `json:"reply"`
	Transcript   []json.RawMessage `json:"transcript"`
	ToolCalls    []json.RawMessage `json:"toolCalls"`
	Artifacts    []DshArtifact     `json:"artifacts"`
	TurnEnd      string            `json:"turnEnd"`
	Error        *DshTaskError     `json:"error"`
}

type DshTaskError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// DshArtifact is a file the turn produced. Key is the opaque download id; Name
// is the original filename the agent wrote.
type DshArtifact struct {
	Key      string `json:"key"`
	Type     string `json:"type"`
	MimeType string `json:"mimeType"`
	Name     string `json:"name"`
}

// DshAttachment is a file already stored in the volume shared with the dsh
// pod. Both paths are resolved by the upload service before submit.
type DshAttachment struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	MimeType string `json:"mimeType,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

// DshSubmitInput is one turn. SessionID continues an existing harness session;
// Preset selects the tool set (empty keeps the runtime's default).
type DshSubmitInput struct {
	Prompt      string
	SessionID   string
	Preset      string
	Model       string
	Provider    string
	Attachments []DshAttachment
}

type dshEnvelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *DshTaskError   `json:"error"`
}

// DshClient talks to the dsh-api task endpoint. Every call carries the
// authenticated user id so the runtime can scope the workspace, enforce
// session ownership and fail closed on another user's task.
//
// When HeadlessHost is set, requests are pinned to one pod by hashing the user
// id over the pod IPs behind the headless Service. dsh-api keeps tasks and live
// agents in memory, so without this a turn polled through a Service load
// balancer could reach a pod that never saw the submit.
type DshClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
	pods    *dshPodResolver
}

func NewDshClient(cfg *config.Config) *DshClient {
	return &DshClient{
		baseURL: cfg.DshBaseURL,
		apiKey:  cfg.DshAPIKey,
		http: &http.Client{
			Timeout: cfg.DshRequestTimeout,
		},
		pods: newDshPodResolver(cfg.DshHeadlessHost, dshEndpointTTL),
	}
}

// Enabled reports whether the runtime is configured at all.
func (c *DshClient) Enabled() bool { return c != nil && c.baseURL != "" }

// Submit starts one turn and returns the task id/session id dsh-api created.
func (c *DshClient) Submit(ctx context.Context, userID int64, input DshSubmitInput) (*DshTask, error) {
	body := map[string]any{"prompt": dshPromptWithAttachments(input.Prompt, input.Attachments)}
	if input.SessionID != "" {
		body["sessionId"] = input.SessionID
	}
	if input.Preset != "" {
		body["preset"] = input.Preset
	}
	// dsh-api pins the turn to this id; switching between turns is allowed and
	// leaves a model-change notice in the transcript.
	if input.Model != "" {
		body["model"] = input.Model
	}
	// The pi-ai route name generated for the deployed inference service.
	if input.Provider != "" {
		body["provider"] = input.Provider
	}
	data, err := c.call(ctx, http.MethodPost, userID, dshTasksPath, body)
	if err != nil {
		return nil, err
	}
	var task DshTask
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, fmt.Errorf("dsh returned an invalid task: %w", err)
	}
	if task.TaskID == "" {
		return nil, fmt.Errorf("dsh did not return a task id")
	}
	return &task, nil
}

// Task polls one task. The owner check is enforced upstream with the same user
// id used for the submit, so a task id from another account is not readable.
func (c *DshClient) Task(ctx context.Context, userID int64, taskID string) (*DshTask, error) {
	if !dshIdentifierPattern.MatchString(taskID) {
		return nil, &DshError{Status: http.StatusBadRequest, Code: "invalid_task", Message: "invalid task id"}
	}
	data, err := c.call(ctx, http.MethodGet, userID, dshTasksPath+"/"+url.PathEscape(taskID), nil)
	if err != nil {
		return nil, err
	}
	var task DshTask
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, fmt.Errorf("dsh returned an invalid task: %w", err)
	}
	return &task, nil
}

// Cancel asks the runtime to stop a running turn. The task settles as canceled;
// it is never resubmitted.
func (c *DshClient) Cancel(ctx context.Context, userID int64, taskID string) error {
	if !dshIdentifierPattern.MatchString(taskID) {
		return &DshError{Status: http.StatusBadRequest, Code: "invalid_task", Message: "invalid task id"}
	}
	_, err := c.call(ctx, http.MethodDelete, userID, dshTasksPath+"/"+url.PathEscape(taskID), nil)
	return err
}

// DownloadArtifact streams one produced file. The caller must close the body.
func (c *DshClient) DownloadArtifact(ctx context.Context, userID int64, taskID, key string) (*http.Response, error) {
	if !dshIdentifierPattern.MatchString(taskID) || !dshIdentifierPattern.MatchString(key) {
		return nil, &DshError{Status: http.StatusBadRequest, Code: "invalid_artifact", Message: "invalid artifact id"}
	}
	url := c.endpoint(userID) + dshTasksPath + "/" + url.PathEscape(taskID) + "/files/" + url.PathEscape(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	c.authenticate(req, userID)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &DshUnavailableError{Err: err}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, decodeDshError(resp)
	}
	return resp, nil
}

func (c *DshClient) call(ctx context.Context, method string, userID int64, path string, body any) (json.RawMessage, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint(userID)+path, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.authenticate(req, userID)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &DshUnavailableError{Err: err}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, &DshUnavailableError{Err: err}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, decodeDshErrorBytes(resp.StatusCode, raw)
	}
	var envelope dshEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("dsh returned an invalid response")
	}
	if !envelope.OK {
		return nil, decodeDshErrorBytes(resp.StatusCode, raw)
	}
	return envelope.Data, nil
}

func (c *DshClient) authenticate(req *http.Request, userID int64) {
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	if userID > 0 {
		req.Header.Set("X-DSH-User-Id", strconv.FormatInt(userID, 10))
	}
}

// endpoint picks the pod for this user. Without a headless Service there is a
// single address and hashing is unnecessary.
func (c *DshClient) endpoint(userID int64) string {
	if c.pods == nil {
		return c.baseURL
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hosts := c.pods.hosts(ctx)
	if len(hosts) == 0 {
		return c.baseURL
	}
	index := dshPodIndex(userID, len(hosts))
	return "http://" + net.JoinHostPort(hosts[index], c.pods.port)
}

// dshPodIndex picks a pod for a user. Hashing the id keeps every request from
// one account (submit, poll, cancel, artifact download) on the pod that holds
// its in-memory task and live agent.
func dshPodIndex(userID int64, podCount int) int {
	if podCount <= 1 {
		return 0
	}
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(strconv.FormatInt(userID, 10)))
	return int(hasher.Sum32() % uint32(podCount))
}

// dshPromptWithAttachments appends the manifest dsh-api has no field for. The
// agent reads the files with its own tools, so only the path and name are
// needed; a newline would break the manifest and is rejected at upload time.
func dshPromptWithAttachments(prompt string, attachments []DshAttachment) string {
	if len(attachments) == 0 {
		return prompt
	}
	lines := make([]string, 0, len(attachments))
	for _, attachment := range attachments {
		path := strings.TrimSpace(attachment.Path)
		if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\r\n") {
			continue
		}
		// The name is rendered into a prompt line; a newline or control
		// character would forge extra manifest entries.
		name := strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
				return -1
			}
			return r
		}, strings.TrimSpace(attachment.Name))
		if name == "" {
			name = path[strings.LastIndex(path, "/")+1:]
		}
		lines = append(lines, "- "+path+" ("+name+")")
		if len(lines) >= dshMaxAttachments {
			break
		}
	}
	if len(lines) == 0 {
		return prompt
	}
	return prompt + "\n\n[Uploaded files]\n" +
		"The user attached these files; they are readable on disk:\n" +
		strings.Join(lines, "\n")
}

func decodeDshError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	return decodeDshErrorBytes(resp.StatusCode, raw)
}

// decodeDshErrorBytes prefers the {ok:false,error:{code,message}} envelope and
// falls back to a status-only error when the body is not JSON.
func decodeDshErrorBytes(status int, raw []byte) error {
	var envelope dshEnvelope
	if err := json.Unmarshal(raw, &envelope); err == nil && envelope.Error != nil {
		return &DshError{Status: status, Code: envelope.Error.Code, Message: envelope.Error.Message}
	}
	return &DshError{Status: status}
}

// dshPodResolver caches the pod IPs behind the headless Service. A lookup
// failure keeps the last known set, and an empty set falls back to the Service
// address, so a DNS hiccup degrades to plain load balancing instead of failing
// every turn.
type dshPodResolver struct {
	host string
	port string
	ttl  time.Duration

	lookup func(ctx context.Context, host string) ([]string, error)

	mu      sync.Mutex
	cached  []string
	expires time.Time
}

func newDshPodResolver(rawHostport string, ttl time.Duration) *dshPodResolver {
	rawHostport = strings.TrimSpace(rawHostport)
	if rawHostport == "" {
		return nil
	}
	host, port := rawHostport, dshDefaultPort
	if parsedHost, parsedPort, err := net.SplitHostPort(rawHostport); err == nil {
		host, port = parsedHost, parsedPort
	}
	return &dshPodResolver{
		host:   host,
		port:   port,
		ttl:    ttl,
		lookup: net.DefaultResolver.LookupHost,
	}
}

func (r *dshPodResolver) hosts(ctx context.Context) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if time.Now().Before(r.expires) {
		return r.cached
	}
	if found, err := r.lookup(ctx, r.host); err == nil && len(found) > 0 {
		// DNS returns the pod set in no guaranteed order; indexing into an
		// unstable order would move a user to another pod on refresh.
		sort.Strings(found)
		r.cached = found
	}
	r.expires = time.Now().Add(r.ttl)
	return r.cached
}
