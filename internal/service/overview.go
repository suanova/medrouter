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

	Description       string              `yaml:"description,omitempty" json:"description,omitempty"`
	IntendedUse       string              `yaml:"intendedUse,omitempty" json:"intended_use,omitempty"`
	Limitations       []string            `yaml:"limitations,omitempty" json:"limitations,omitempty"`
	Contraindications []string            `yaml:"contraindications,omitempty" json:"contraindications,omitempty"`
	OutOfScope        []string            `yaml:"outOfScope,omitempty" json:"out_of_scope,omitempty"`
	Benchmarks        []OverviewBenchmark `yaml:"benchmarks,omitempty" json:"benchmarks,omitempty"`
	RegulatoryStatus  string              `yaml:"regulatoryStatus,omitempty" json:"regulatory_status,omitempty"`
	RegulatoryNote    string              `yaml:"regulatoryNote,omitempty" json:"regulatory_note,omitempty"`
	Disclaimer        string              `yaml:"disclaimer,omitempty" json:"disclaimer,omitempty"`
	Links             map[string]any      `yaml:"links,omitempty" json:"links,omitempty"`
	Extra             map[string]any      `yaml:"extra,omitempty" json:"extra,omitempty"`

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

// DatasetOverview is the parsed content of a dataset overview.yaml ConfigMap. It
// powers the read-only "overview" area of a dataset detail page. Identity and
// filtering facets come from the Dataset CR annotations, never from here; this
// type only carries long-form prose, mirroring ModelOverview with `dataset` as
// the strong-validation anchor instead of `model`.
type DatasetOverview struct {
	// Dataset is the strong-validation anchor: must equal Dataset.spec.dataset.
	Dataset string `yaml:"dataset" json:"dataset"`

	Description       string              `yaml:"description,omitempty" json:"description,omitempty"`
	IntendedUse       string              `yaml:"intendedUse,omitempty" json:"intended_use,omitempty"`
	Limitations       []string            `yaml:"limitations,omitempty" json:"limitations,omitempty"`
	Contraindications []string            `yaml:"contraindications,omitempty" json:"contraindications,omitempty"`
	OutOfScope        []string            `yaml:"outOfScope,omitempty" json:"out_of_scope,omitempty"`
	Benchmarks        []OverviewBenchmark `yaml:"benchmarks,omitempty" json:"benchmarks,omitempty"`
	RegulatoryStatus  string              `yaml:"regulatoryStatus,omitempty" json:"regulatory_status,omitempty"`
	RegulatoryNote    string              `yaml:"regulatoryNote,omitempty" json:"regulatory_note,omitempty"`
	Disclaimer        string              `yaml:"disclaimer,omitempty" json:"disclaimer,omitempty"`
	Links             map[string]any      `yaml:"links,omitempty" json:"links,omitempty"`
	Extra             map[string]any      `yaml:"extra,omitempty" json:"extra,omitempty"`

	Valid bool   `yaml:"-" json:"-"`
	Error string `yaml:"-" json:"-"`
}

// ParseDatasetOverview parses a dataset overview ConfigMap body, anchored to
// dataset. When dataset does not match Dataset.spec.dataset the overview is
// rejected per the strong-validation rule.
func ParseDatasetOverview(body, dataset string) (*DatasetOverview, error) {
	var ov DatasetOverview
	if err := yaml.Unmarshal([]byte(body), &ov); err != nil {
		return nil, fmt.Errorf("invalid dataset overview: %w", err)
	}
	if ov.Dataset == "" {
		return nil, fmt.Errorf("invalid dataset overview: missing required field dataset")
	}
	if ov.Dataset != dataset {
		return nil, fmt.Errorf("INVALID_DATASET_OVERVIEW: overview.dataset %q != Dataset.spec.dataset %q", ov.Dataset, dataset)
	}
	ov.Valid = true
	return &ov, nil
}
