package kube

import (
	"context"
	"fmt"
	"net/http"
)

const deploymentsBase = "/apis/apps/v1/namespaces/%s/deployments"

// SetDeploymentPodAnnotation ensures one pod-template annotation has value,
// patching the Deployment only when it differs. Changing a pod-template
// annotation is what makes a Deployment roll, so this is how MedRouter tells
// the dsh runtime that the set of served models changed.
func (c *Client) SetDeploymentPodAnnotation(ctx context.Context, namespace, name, key, value string) error {
	path := fmt.Sprintf(deploymentsBase, namespace) + "/" + name
	var deployment struct {
		Spec struct {
			Template struct {
				Metadata struct {
					Annotations map[string]string `json:"annotations"`
				} `json:"metadata"`
			} `json:"template"`
		} `json:"spec"`
	}
	if err := c.send(ctx, http.MethodGet, path, "", nil, &deployment); err != nil {
		return err
	}
	if deployment.Spec.Template.Metadata.Annotations[key] == value {
		return nil
	}
	patch := map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]any{key: value},
				},
			},
		},
	}
	return c.send(ctx, http.MethodPatch, path, "application/merge-patch+json", patch, nil)
}
