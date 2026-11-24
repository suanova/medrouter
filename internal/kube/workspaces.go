package kube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// devEnvironmentBase is the namespaced collection path for DevEnvironments.
const devEnvironmentBase = "/apis/ai.cubestack.io/v1alpha1/namespaces/%s/devenvironments"

// DevEnvironment decodes only the parts of a CubeStack dev environment the
// studio mirrors. The full spec is written as a map so new fields do not
// require a type change here.
type DevEnvironment struct {
	Metadata ObjectMeta           `json:"metadata"`
	Status   DevEnvironmentStatus `json:"status"`
}

// DevEnvironmentStatus is the subset that projects into a workspace row.
type DevEnvironmentStatus struct {
	Phase struct {
		Name string `json:"name"`
	} `json:"phase"`
	Conditions []Condition              `json:"conditions"`
	Endpoints  []DevEnvironmentEndpoint `json:"endpoints"`
}

type DevEnvironmentEndpoint struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

// NotebookState is the flattened status the studio service consumes.
type NotebookState struct {
	Phase    string
	Reason   string
	Endpoint string
}

// CreateDevEnvironment posts one environment spec into a namespace.
func (c *Client) CreateDevEnvironment(ctx context.Context, namespace, name string, labels map[string]string, spec map[string]any) (DevEnvironment, error) {
	payload := map[string]any{
		"apiVersion": "ai.cubestack.io/v1alpha1",
		"kind":       "DevEnvironment",
		"metadata":   map[string]any{"name": name, "labels": labels},
		"spec":       spec,
	}
	var created DevEnvironment
	err := c.send(ctx, http.MethodPost, fmt.Sprintf(devEnvironmentBase, namespace), "application/json", payload, &created)
	return created, err
}

// GetDevEnvironment reads one environment. found is false on 404.
func (c *Client) GetDevEnvironment(ctx context.Context, namespace, name string) (DevEnvironment, bool, error) {
	var env DevEnvironment
	err := c.send(ctx, http.MethodGet, fmt.Sprintf(devEnvironmentBase, namespace)+"/"+name, "", nil, &env)
	if err != nil {
		var status *StatusError
		if errors.As(err, &status) && status.Status == http.StatusNotFound {
			return DevEnvironment{}, false, nil
		}
		return DevEnvironment{}, false, err
	}
	return env, true, nil
}

// SetDevEnvironmentRunning starts or stops an environment without deleting it.
func (c *Client) SetDevEnvironmentRunning(ctx context.Context, namespace, name string, running bool) error {
	patch := map[string]any{"spec": map[string]any{"running": running}}
	return c.send(ctx, http.MethodPatch, fmt.Sprintf(devEnvironmentBase, namespace)+"/"+name,
		"application/merge-patch+json", patch, nil)
}

// DeleteDevEnvironment removes an environment. The PVC is kept or dropped by
// the environment's own pvcRetention setting.
func (c *Client) DeleteDevEnvironment(ctx context.Context, namespace, name string) error {
	return c.send(ctx, http.MethodDelete, fmt.Sprintf(devEnvironmentBase, namespace)+"/"+name, "", nil, nil)
}

// NotebookStateOf flattens a DevEnvironment into the fields the studio needs.
func NotebookStateOf(env DevEnvironment) NotebookState {
	state := NotebookState{Phase: env.Status.Phase.Name}
	for _, endpoint := range env.Status.Endpoints {
		if endpoint.Address != "" {
			state.Endpoint = endpoint.Address
			break
		}
	}
	return state
}

// send performs one request against the Kubernetes REST API. contentType is
// only used when body is non-nil; into may be nil when the response is not
// needed.
func (c *Client) send(ctx context.Context, method, path, contentType string, body, into any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return &StatusError{Status: 0, Reason: "API_ERROR", Retryable: false, detail: err.Error()}
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return &StatusError{Status: 0, Reason: "API_ERROR", Retryable: false, detail: err.Error()}
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", contentType)
	}
	if token, err := c.bearerToken(); err != nil {
		return err
	} else if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return &StatusError{Status: 0, Reason: "TIMEOUT", Retryable: true}
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return &StatusError{Status: 0, Reason: "TIMEOUT", Retryable: true}
		}
		return &StatusError{Status: 0, Reason: "API_ERROR", Retryable: true, detail: "request failed"}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		status := classifyStatus(resp.StatusCode)
		if message := errorMessage(detail); message != "" {
			status.detail = message
		}
		return status
	}
	if into == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBodySize))
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodySize)).Decode(into); err != nil {
		return &StatusError{Status: resp.StatusCode, Reason: "API_ERROR", Retryable: true, detail: "invalid response"}
	}
	return nil
}

// errorMessage extracts a Kubernetes Status message without leaking the body.
func errorMessage(body []byte) string {
	var status struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &status); err != nil {
		return ""
	}
	message := strings.TrimSpace(status.Message)
	if len(message) > 240 {
		message = message[:240]
	}
	return message
}
