package kube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

const (
	inferenceServiceBase = "/apis/ai.cubestack.io/v1alpha1/namespaces/%s/inferenceservices"
	modelVersionBase     = "/apis/ai.cubestack.io/v1alpha1/modelversions"
)

// InferenceService decodes the status the studio projects.
type InferenceService struct {
	Metadata ObjectMeta              `json:"metadata"`
	Status   InferenceServiceStatus  `json:"status"`
}

type InferenceServiceStatus struct {
	Conditions []Condition `json:"conditions"`
	// Endpoints is intentionally free-form: the operator has changed its shape
	// once already, and the studio only needs an address when one is present.
	Endpoints []map[string]any `json:"endpoints"`
}

// CreateInferenceService posts one service spec into a namespace.
func (c *Client) CreateInferenceService(ctx context.Context, namespace, name string, labels map[string]string, spec map[string]any) (InferenceService, error) {
	payload := map[string]any{
		"apiVersion": "ai.cubestack.io/v1alpha1",
		"kind":       "InferenceService",
		"metadata":   map[string]any{"name": name, "labels": labels},
		"spec":       spec,
	}
	var service InferenceService
	err := c.send(ctx, http.MethodPost, fmt.Sprintf(inferenceServiceBase, namespace), "application/json", payload, &service)
	return service, err
}

// GetInferenceService reads one service. found is false on 404.
func (c *Client) GetInferenceService(ctx context.Context, namespace, name string) (InferenceService, bool, error) {
	var service InferenceService
	err := c.send(ctx, http.MethodGet, fmt.Sprintf(inferenceServiceBase, namespace)+"/"+name, "", nil, &service)
	if err != nil {
		var status *StatusError
		if errors.As(err, &status) && status.Status == http.StatusNotFound {
			return InferenceService{}, false, nil
		}
		return InferenceService{}, false, err
	}
	return service, true, nil
}

// DeleteInferenceService removes one service.
func (c *Client) DeleteInferenceService(ctx context.Context, namespace, name string) error {
	return c.send(ctx, http.MethodDelete, fmt.Sprintf(inferenceServiceBase, namespace)+"/"+name, "", nil, nil)
}

// CreateModelVersion registers a new cluster-scoped ModelVersion.
func (c *Client) CreateModelVersion(ctx context.Context, name string, labels, annotations map[string]string, spec map[string]any) error {
	payload := map[string]any{
		"apiVersion": "ai.cubestack.io/v1alpha1",
		"kind":       "ModelVersion",
		"metadata": map[string]any{
			"name":        name,
			"labels":      labels,
			"annotations": annotations,
		},
		"spec": spec,
	}
	return c.send(ctx, http.MethodPost, modelVersionBase, "application/json", payload, nil)
}

// ModelVersionExists reports whether a ModelVersion is already registered.
func (c *Client) ModelVersionExists(ctx context.Context, name string) (bool, error) {
	var out json.RawMessage
	err := c.send(ctx, http.MethodGet, modelVersionBase+"/"+name, "", nil, &out)
	if err != nil {
		var status *StatusError
		if errors.As(err, &status) && status.Status == http.StatusNotFound {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// InferenceServiceReady reports whether the service has a Ready condition.
func InferenceServiceReady(service InferenceService) bool {
	for _, condition := range service.Status.Conditions {
		if condition.Type == "Ready" {
			return condition.Status == "True"
		}
	}
	return false
}

// InferenceServiceReason returns the first non-True condition reason, if any.
func InferenceServiceReason(service InferenceService) string {
	for _, condition := range service.Status.Conditions {
		if condition.Status != "True" && condition.Reason != "" {
			return condition.Reason
		}
	}
	return ""
}

// InferenceServiceEndpoint returns the first endpoint address the operator
// published, if any.
func InferenceServiceEndpoint(service InferenceService) string {
	for _, endpoint := range service.Status.Endpoints {
		for _, key := range []string{"address", "url"} {
			if value, ok := endpoint[key].(string); ok && value != "" {
				return value
			}
		}
	}
	return ""
}
