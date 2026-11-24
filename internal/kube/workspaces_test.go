package kube

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func workspaceClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := NewAPIClient(server.URL, 2*time.Second)
	if err != nil {
		t.Fatalf("NewAPIClient: %v", err)
	}
	return client
}

func TestCreateDevEnvironmentPostsNamespacedObject(t *testing.T) {
	var gotMethod, gotPath, gotContentType string
	var gotBody map[string]any
	client := workspaceClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotContentType = r.Method, r.URL.Path, r.Header.Get("Content-Type")
		payload, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(payload, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"metadata":{"name":"pubmedqa-u1-lab"}}`))
	})

	spec := map[string]any{"type": "jupyter", "running": true}
	env, err := client.CreateDevEnvironment(context.Background(), "medrouter-workspace", "pubmedqa-u1-lab",
		map[string]string{"medrouter.io/owner": "1"}, spec)
	if err != nil {
		t.Fatalf("CreateDevEnvironment: %v", err)
	}
	if env.Metadata.Name != "pubmedqa-u1-lab" {
		t.Fatalf("name = %q", env.Metadata.Name)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q", gotMethod)
	}
	if gotPath != "/apis/ai.cubestack.io/v1alpha1/namespaces/medrouter-workspace/devenvironments" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotContentType != "application/json" {
		t.Fatalf("content type = %q", gotContentType)
	}
	metadata := gotBody["metadata"].(map[string]any)
	if metadata["name"] != "pubmedqa-u1-lab" {
		t.Fatalf("metadata = %+v", metadata)
	}
	labels := metadata["labels"].(map[string]any)
	if labels["medrouter.io/owner"] != "1" {
		t.Fatalf("labels = %+v", labels)
	}
	if gotBody["kind"] != "DevEnvironment" || gotBody["apiVersion"] != "ai.cubestack.io/v1alpha1" {
		t.Fatalf("body = %+v", gotBody)
	}
}

func TestSetDevEnvironmentRunningUsesMergePatch(t *testing.T) {
	var gotMethod, gotPath, gotContentType string
	var gotBody map[string]any
	client := workspaceClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotContentType = r.Method, r.URL.Path, r.Header.Get("Content-Type")
		payload, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(payload, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})

	if err := client.SetDevEnvironmentRunning(context.Background(), "ns", "env", false); err != nil {
		t.Fatalf("SetDevEnvironmentRunning: %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Fatalf("method = %q", gotMethod)
	}
	if gotPath != "/apis/ai.cubestack.io/v1alpha1/namespaces/ns/devenvironments/env" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotContentType != "application/merge-patch+json" {
		t.Fatalf("content type = %q", gotContentType)
	}
	spec := gotBody["spec"].(map[string]any)
	if spec["running"] != false {
		t.Fatalf("body = %+v", gotBody)
	}
}

func TestGetDevEnvironmentReportsNotFound(t *testing.T) {
	client := workspaceClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"kind":"Status","message":"not found"}`))
	})

	_, found, err := client.GetDevEnvironment(context.Background(), "ns", "missing")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if found {
		t.Fatal("found should be false on 404")
	}
}

func TestCreateDevEnvironmentSurfacesInvalidSpec(t *testing.T) {
	client := workspaceClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"kind":"Status","message":"spec.image: Required value"}`))
	})

	_, err := client.CreateDevEnvironment(context.Background(), "ns", "env", nil, map[string]any{})
	status, ok := err.(*StatusError)
	if !ok || status.Reason != "INVALID" || status.Detail() != "spec.image: Required value" {
		t.Fatalf("err = %+v", err)
	}
}

func TestDeleteDevEnvironment(t *testing.T) {
	var gotMethod, gotPath string
	client := workspaceClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"Success"}`))
	})

	if err := client.DeleteDevEnvironment(context.Background(), "ns", "env"); err != nil {
		t.Fatalf("DeleteDevEnvironment: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/apis/ai.cubestack.io/v1alpha1/namespaces/ns/devenvironments/env" {
		t.Fatalf("%s %s", gotMethod, gotPath)
	}
}
