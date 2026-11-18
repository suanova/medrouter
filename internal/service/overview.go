package service

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// ModelOverview is the parsed content of an overview.yaml ConfigMap. It powers
// the read-only "overview" tab. Identity and filter facets come from the
// ModelVersion annotations, never from here; this file only carries long-form
// prose. Keys are pure-English; values are natural language (often Chinese).

type OverviewBenchmark struct {
	Name   string `yaml:"name" json:"name"`
	Metric string `yaml:"metric" json:"metric"`
	Value  string `yaml:"value" json:"value"`
	Split  string `yaml:"split,omitempty" json:"split,omitempty"`
	Source string `yaml:"source,omitempty" json:"source,omitempty"`
}

type ModelOverview struct {
	// Model is the strong-validation anchor: must equal ModelVersion.spec.model.
	Model string `yaml:"model" json:"model"`

	Description       string            `yaml:"description,omitempty" json:"description,omitempty"`
	IntendedUse       string            `yaml:"intendedUse,omitempty" json:"intended_use,omitempty"`
	Limitations       []string          `yaml:"limitations,omitempty" json:"limitations,omitempty"`
	Contraindications []string          `yaml:"contraindications,omitempty" json:"contraindications,omitempty"`
	OutOfScope        []string          `yaml:"outOfScope,omitempty" json:"out_of_scope,omitempty"`
	Benchmarks        []OverviewBenchmark `yaml:"benchmarks,omitempty" json:"benchmarks,omitempty"`
	RegulatoryStatus  string            `yaml:"regulatoryStatus,omitempty" json:"regulatory_status,omitempty"`
	RegulatoryNote    string            `yaml:"regulatoryNote,omitempty" json:"regulatory_note,omitempty"`
	Disclaimer        string            `yaml:"disclaimer,omitempty" json:"disclaimer,omitempty"`
	Links             map[string]any    `yaml:"links,omitempty" json:"links,omitempty"`
	Extra             map[string]any    `yaml:"extra,omitempty" json:"extra,omitempty"`

	Valid bool   `yaml:"-" json:"-"`
	Error string `yaml:"-" json:"-"`
}

// ParseModelOverview parses an overview ConfigMap body, anchored to model. When
// model does not match ModelVersion.spec.model the overview is rejected per the
// strong-validation rule (Valid=false, Error=INVALID_MODEL_OVERVIEW).
func ParseModelOverview(body, model string) (*ModelOverview, error) {
	var ov ModelOverview
	if err := yaml.Unmarshal([]byte(body), &ov); err != nil {
		return nil, fmt.Errorf("invalid overview: %w", err)
	}
	if ov.Model == "" {
		return nil, fmt.Errorf("invalid overview: missing required field model")
	}
	if ov.Model != model {
		return nil, fmt.Errorf("INVALID_MODEL_OVERVIEW: overview.model %q != ModelVersion.spec.model %q", ov.Model, model)
	}
	ov.Valid = true
	return &ov, nil
}