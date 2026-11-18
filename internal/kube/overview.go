package kube

import (
	"context"
	"net/url"
	"strings"
)

// OverviewConfigMap decodes only the fields MedRouter needs from a ConfigMap
// that carries a model overview (the "source file" for the overview tab).
type OverviewConfigMap struct {
	Metadata ObjectMeta        `json:"metadata"`
	Data     map[string]string `json:"data"`
}

// OverviewOverviewKey is the data key holding the overview.yaml body.
const OverviewOverviewKey = "overview.yaml"

// OverviewSource is an optional capability a Source may implement to provide
// model overviews read from ConfigMaps. It is kept separate from the core
// Source so the catalog can degrade to "no overview" without touching every
// source implementation.
type OverviewSource interface {
	ListOverviewConfigMaps(ctx context.Context) ([]OverviewConfigMap, error)
}

const (
	overviewConfigMapPrefix = "/api/v1/namespaces/"
	overviewConfigMapSuffix = "/configmaps"
	overviewSelectorKey     = "medrouter.io/overview"
)

// ListOverviewConfigMaps returns the overview ConfigMaps, selected by
// medrouter.io/overview=true, from the namespace configured on the client.
func (c *Client) ListOverviewConfigMaps(ctx context.Context) ([]OverviewConfigMap, error) {
	query := url.Values{
		"labelSelector": {overviewSelectorKey + "=true"},
		"limit":         {"500"},
	}
	path := overviewConfigMapPrefix + c.overviewNamespace + overviewConfigMapSuffix
	var body rawList[OverviewConfigMap]
	if err := c.getJSON(ctx, path+"?"+query.Encode(), &body); err != nil {
		return nil, err
	}
	return body.Items, nil
}

// UnavailableSource implements OverviewSource so disabled / no-cluster sources
// still satisfy the optional assertion and return the classified error.
func (s UnavailableSource) ListOverviewConfigMaps(context.Context) ([]OverviewConfigMap, error) {
	return nil, &StatusError{Reason: s.Reason, Retryable: s.Retryable}
}

// OverviewBody returns the overview.yaml body keyed by model, if present.
func (c *OverviewConfigMap) OverviewBody() (string, bool) {
	if c == nil {
		return "", false
	}
	body, ok := c.Data[OverviewOverviewKey]
	return strings.TrimSpace(body), ok
}
