package kube

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// jobBase is the namespaced collection path for batch Jobs.
const jobBase = "/apis/batch/v1/namespaces/%s/jobs"

// Job decodes the status fields the studio projects into a workspace row.
type Job struct {
	Metadata ObjectMeta `json:"metadata"`
	Status   JobStatus  `json:"status"`
}

type JobStatus struct {
	Phase      string      `json:"phase"`
	Reason     string      `json:"reason"`
	Message    string      `json:"message"`
	Conditions []Condition `json:"conditions"`
	Succeeded  int32       `json:"succeeded"`
	Failed     int32       `json:"failed"`
}

// CreateJob posts one batch Job. spec is the Job's spec object.
func (c *Client) CreateJob(ctx context.Context, namespace, name string, labels map[string]string, spec map[string]any) (Job, error) {
	payload := map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata":   map[string]any{"name": name, "labels": labels},
		"spec":       spec,
	}
	var job Job
	err := c.send(ctx, http.MethodPost, fmt.Sprintf(jobBase, namespace), "application/json", payload, &job)
	return job, err
}

// GetJob reads one Job. found is false on 404.
func (c *Client) GetJob(ctx context.Context, namespace, name string) (Job, bool, error) {
	var job Job
	err := c.send(ctx, http.MethodGet, fmt.Sprintf(jobBase, namespace)+"/"+name, "", nil, &job)
	if err != nil {
		var status *StatusError
		if errors.As(err, &status) && status.Status == http.StatusNotFound {
			return Job{}, false, nil
		}
		return Job{}, false, err
	}
	return job, true, nil
}

// DeleteJob removes a Job and its pods. Background propagation keeps the delete
// responsive when the training pod is mid-step.
func (c *Client) DeleteJob(ctx context.Context, namespace, name string) error {
	return c.send(ctx, http.MethodDelete,
		fmt.Sprintf(jobBase, namespace)+"/"+name+"?propagationPolicy=Background", "", nil, nil)
}
