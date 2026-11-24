package service

import (
	"context"
	"sort"
	"strings"

	"github.com/suanova/medrouter/internal/kube"
)

// Fine-tuning task vocabulary. Both Dataset.spec.meta.task and
// ModelVersion.spec.meta.tasks use these exact values; anything else is
// ignored rather than guessed.
const (
	TaskTextQA             = "text-qa"
	TaskTextClassification = "text-classification"
	TaskSequence           = "sequence"
	TaskStructure          = "structure"
	TaskMolecule           = "molecule"
	TaskScRNA              = "scrna"
	TaskVQA                = "vqa"
)

var knownTasks = map[string]struct{}{
	TaskTextQA:             {},
	TaskTextClassification: {},
	TaskSequence:           {},
	TaskStructure:          {},
	TaskMolecule:           {},
	TaskScRNA:              {},
	TaskVQA:                {},
}

// Where the capability lives differs by resource: DatasetSpec is mutable, but
// a ValidatingAdmissionPolicy makes ModelVersionSpec immutable, so model
// capability has to be expressed as annotations on existing records.
const (
	metaTasks    = "tasks"    // ModelVersion spec.meta: comma-separated tasks
	metaFinetune = "finetune" // ModelVersion spec.meta: comma-separated methods
	metaTask     = "task"     // Dataset spec.meta: one task value

	annotationFineTuneTasks = "medrouter.io/finetune-tasks"
	annotationFineTune      = "medrouter.io/finetune"
)

// fallbackModelTasks covers records that carry no meta.tasks yet. Only models
// we actually have a training recipe for are listed; every other architecture
// is reported as not fine-tunable instead of guessing.
var fallbackModelTasks = map[string][]string{
	"mistral":    {TaskTextQA, TaskTextClassification},
	"mixtral":    {TaskTextQA, TaskTextClassification},
	"llama":      {TaskTextQA, TaskTextClassification},
	"qwen":       {TaskTextQA, TaskTextClassification},
	"bert":       {TaskTextClassification, TaskTextQA},
	"distilbert": {TaskTextClassification},
	"deberta":    {TaskTextClassification},
	"deberta-v3": {TaskTextClassification},
}

// fallbackDatasetTask derives a task from the storage format when the record is
// unannotated. It uses the first format token: "SDF / TSV" is a molecule set,
// not tabular text.
var fallbackDatasetTask = map[string]string{
	"parquet": TaskTextQA,
	"jsonl":   TaskTextQA,
	"json":    TaskTextQA,
	"sdf":     TaskMolecule,
	"tsv":     TaskMolecule,
	"csv":     TaskMolecule,
	"fasta":   TaskSequence,
	"mmcif":   TaskStructure,
	"pdb":     TaskStructure,
	"h5ad":    TaskScRNA,
}

// FineTuneDataset is one dataset that can be used to fine-tune the model.
type FineTuneDataset struct {
	Name      string `json:"name"`
	Format    string `json:"format"`
	Size      string `json:"size,omitempty"`
	Records   *int64 `json:"record_count,omitempty"`
	Task      string `json:"task"`
	Annotated bool   `json:"annotated"`
}

// ExcludedDataset explains why a dataset is not offered. The reason is shown
// rather than hidden so a missing dataset never looks like a bug.
type ExcludedDataset struct {
	Name   string `json:"name"`
	Task   string `json:"task,omitempty"`
	Reason string `json:"reason"`
}

// FineTuneTargets answers "which datasets can I fine-tune this model with".
type FineTuneTargets struct {
	Model     string            `json:"model"`
	Available bool              `json:"available"`
	Reason    string            `json:"reason,omitempty"`
	Method    string            `json:"method,omitempty"`
	Tasks     []string          `json:"tasks"`
	Annotated bool              `json:"annotated"`
	Datasets  []FineTuneDataset `json:"datasets"`
	Excluded  []ExcludedDataset `json:"excluded"`
}

const (
	FineTuneNoRecipe     = "NO_RECIPE"
	FineTuneModelMissing = "MODEL_NOT_FOUND"
	FineTuneUnavailable  = "CATALOG_UNAVAILABLE"
)

// FineTuneService resolves the model/dataset compatibility needed by the
// studio's fine-tune picker.
type FineTuneService struct {
	models   *ModelCatalog
	datasets *DatasetCatalog
}

func NewFineTuneService(models *ModelCatalog, datasets *DatasetCatalog) *FineTuneService {
	return &FineTuneService{models: models, datasets: datasets}
}

// Targets returns the datasets compatible with one model. An unavailable or
// unknown model is reported in the payload rather than as an HTTP error, so
// the dialog can explain the state.
func (s *FineTuneService) Targets(ctx context.Context, model string) *FineTuneTargets {
	out := &FineTuneTargets{Model: model, Datasets: []FineTuneDataset{}, Excluded: []ExcludedDataset{}}

	modelSnap := s.models.snapshot(ctx)
	datasetSnap := s.datasets.snapshot(ctx)
	if !modelSnap.available || !datasetSnap.available {
		out.Reason = FineTuneUnavailable
		return out
	}

	versions := versionsForModel(modelSnap.mvs, model)
	if len(versions) == 0 {
		out.Reason = FineTuneModelMissing
		return out
	}

	methods, tasks, annotated := modelCapability(versions)
	if !containsString(methods, "lora") || len(tasks) == 0 {
		out.Reason = FineTuneNoRecipe
		return out
	}

	out.Available = true
	out.Method = "lora"
	out.Tasks = tasks
	out.Annotated = annotated

	allowed := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		allowed[task] = struct{}{}
	}

	for _, dataset := range datasetSnap.datasets {
		name := strings.TrimSpace(dataset.Spec.Dataset)
		if name == "" {
			continue
		}
		task, datasetAnnotated := datasetTask(dataset.Spec)
		if _, ok := allowed[task]; !ok || task == "" {
			out.Excluded = append(out.Excluded, ExcludedDataset{
				Name:   name,
				Task:   task,
				Reason: excludeReason(task),
			})
			continue
		}
		out.Datasets = append(out.Datasets, FineTuneDataset{
			Name:      name,
			Format:    dataset.Spec.Format,
			Size:      dataset.Spec.Size,
			Records:   dataset.Spec.RecordCount,
			Task:      task,
			Annotated: datasetAnnotated,
		})
	}

	sort.Slice(out.Datasets, func(i, j int) bool { return out.Datasets[i].Name < out.Datasets[j].Name })
	sort.Slice(out.Excluded, func(i, j int) bool { return out.Excluded[i].Name < out.Excluded[j].Name })
	return out
}

func excludeReason(task string) string {
	if task == "" {
		return "TASK_UNKNOWN"
	}
	return "TASK_MISMATCH"
}

// versionsForModel matches both the spec.model field and the CR name, because
// the catalog exposes the former but links use the latter.
func versionsForModel(versions []kube.ModelVersion, model string) []kube.ModelVersion {
	var out []kube.ModelVersion
	for _, version := range versions {
		if version.Spec.Model == model || version.Metadata.Name == model {
			out = append(out, version)
		}
	}
	return out
}

// modelCapability prefers an annotated version (meta.finetune/meta.tasks) and
// falls back to the architecture table. annotated is false when the fallback
// was used.
func modelCapability(versions []kube.ModelVersion) (methods, tasks []string, annotated bool) {
	for _, version := range versions {
		// Prefer spec.meta (only possible on records created after the field
		// existed), then annotations (mutable on any record).
		methods := metaWords(version.Spec.Meta, metaFinetune)
		tasks := metaList(version.Spec.Meta, metaTasks)
		if len(methods) == 0 {
			methods = metaWords(version.Metadata.Annotations, annotationFineTune)
			tasks = metaList(version.Metadata.Annotations, annotationFineTuneTasks)
		}
		if len(methods) == 0 {
			continue
		}
		return methods, tasks, true
	}
	architecture := strings.ToLower(strings.TrimSpace(versions[0].Spec.Architecture))
	fallback, ok := fallbackModelTasks[architecture]
	if !ok {
		return nil, nil, false
	}
	return []string{"lora"}, append([]string(nil), fallback...), false
}

// datasetTask reads meta.task, falling back to the storage format.
func datasetTask(spec kube.DatasetSpec) (task string, annotated bool) {
	if value := strings.ToLower(strings.TrimSpace(spec.Meta[metaTask])); value != "" {
		if _, ok := knownTasks[value]; ok {
			return value, true
		}
	}
	first := strings.ToLower(strings.TrimSpace(strings.Split(spec.Format, "/")[0]))
	if task, ok := fallbackDatasetTask[first]; ok {
		return task, false
	}
	return "", false
}

// metaWords splits a comma or whitespace separated meta value without
// filtering. Used for fields whose vocabulary is not the task list.
func metaWords(meta map[string]string, key string) []string {
	raw := strings.TrimSpace(meta[key])
	if raw == "" {
		return nil
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t'
	})
	out := make([]string, 0, len(fields))
	seen := map[string]struct{}{}
	for _, field := range fields {
		value := strings.ToLower(strings.TrimSpace(field))
		if value == "" {
			continue
		}
		if _, dup := seen[value]; dup {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

// metaList splits a comma or whitespace separated meta value and keeps only
// vocabulary values, so a typo in a CR cannot widen the match.
func metaList(meta map[string]string, key string) []string {
	raw := strings.TrimSpace(meta[key])
	if raw == "" {
		return nil
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t'
	})
	out := make([]string, 0, len(fields))
	seen := map[string]struct{}{}
	for _, field := range fields {
		value := strings.ToLower(strings.TrimSpace(field))
		if _, ok := knownTasks[value]; ok {
			if _, dup := seen[value]; dup {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}
