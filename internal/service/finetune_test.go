package service

import (
	"context"
	"testing"
	"time"

	"github.com/suanova/medrouter/internal/config"
	"github.com/suanova/medrouter/internal/kube"
)

func finetuneService(mvs []kube.ModelVersion, datasets []kube.Dataset) *FineTuneService {
	cfg := &config.Config{
		ModelLibraryCacheTTL:   time.Minute,
		ModelLibraryTimeout:    time.Second,
		DatasetLibraryCacheTTL: time.Minute,
		DatasetLibraryTimeout:  time.Second,
	}
	source := &fakeSource{mvs: mvs, datasets: datasets}
	return NewFineTuneService(NewModelCatalog(cfg, source), NewDatasetCatalog(cfg, source))
}

func dataset(name, format, task string) kube.Dataset {
	meta := map[string]string{}
	if task != "" {
		meta[metaTask] = task
	}
	return kube.Dataset{
		Metadata: kube.ObjectMeta{Name: name},
		Spec:     kube.DatasetSpec{Dataset: name, Format: format, Size: "84Mi", Meta: meta},
	}
}

func modelVersion(model, architecture string, meta map[string]string) kube.ModelVersion {
	return kube.ModelVersion{
		Metadata: kube.ObjectMeta{Name: model},
		Spec:     kube.ModelVersionSpec{Model: model, Architecture: architecture, Meta: meta},
	}
}

func TestFineTuneTargetsUsesAnnotations(t *testing.T) {
	mvs := []kube.ModelVersion{modelVersion("biomistral-7b", "mistral", map[string]string{
		metaFinetune: "lora",
		metaTasks:    "text-qa, text-classification",
	})}
	datasets := []kube.Dataset{
		dataset("medmcqa", "Parquet", TaskTextQA),
		dataset("chembl", "SDF / TSV", TaskMolecule),
		dataset("uniprot", "FASTA", ""), // unannotated, falls back to the format
	}

	targets := finetuneService(mvs, datasets).Targets(context.Background(), "biomistral-7b")
	if !targets.Available || targets.Method != "lora" || !targets.Annotated {
		t.Fatalf("targets = %+v", targets)
	}
	if len(targets.Datasets) != 1 || targets.Datasets[0].Name != "medmcqa" {
		t.Fatalf("datasets = %+v", targets.Datasets)
	}
	if !targets.Datasets[0].Annotated {
		t.Fatal("medmcqa should be reported as annotated")
	}
	if len(targets.Excluded) != 2 {
		t.Fatalf("excluded = %+v", targets.Excluded)
	}
	for _, excluded := range targets.Excluded {
		if excluded.Reason != "TASK_MISMATCH" {
			t.Fatalf("reason = %+v", excluded)
		}
	}
}

func TestFineTuneTargetsFallsBackToArchitecture(t *testing.T) {
	mvs := []kube.ModelVersion{modelVersion("biomistral-7b", "mistral", nil)}
	datasets := []kube.Dataset{
		dataset("medmcqa", "Parquet", ""),
		dataset("chembl", "SDF / TSV", ""),
	}

	targets := finetuneService(mvs, datasets).Targets(context.Background(), "biomistral-7b")
	if !targets.Available || targets.Annotated {
		t.Fatalf("targets = %+v", targets)
	}
	if len(targets.Datasets) != 1 || targets.Datasets[0].Name != "medmcqa" || targets.Datasets[0].Annotated {
		t.Fatalf("datasets = %+v", targets.Datasets)
	}
}

func TestFineTuneTargetsNoRecipe(t *testing.T) {
	mvs := []kube.ModelVersion{modelVersion("esm2", "esm2", nil)}
	targets := finetuneService(mvs, nil).Targets(context.Background(), "esm2")
	if targets.Available || targets.Reason != FineTuneNoRecipe {
		t.Fatalf("targets = %+v", targets)
	}
}

func TestFineTuneTargetsExplicitEmptyFinetune(t *testing.T) {
	// An annotated "no training recipe" must beat the architecture fallback.
	mvs := []kube.ModelVersion{modelVersion("biomistral-7b", "mistral", map[string]string{
		metaFinetune: "none",
		metaTasks:    "text-qa",
	})}
	targets := finetuneService(mvs, nil).Targets(context.Background(), "biomistral-7b")
	if targets.Available || targets.Reason != FineTuneNoRecipe {
		t.Fatalf("targets = %+v", targets)
	}
}

func TestFineTuneTargetsUnknownModel(t *testing.T) {
	targets := finetuneService(nil, nil).Targets(context.Background(), "missing")
	if targets.Available || targets.Reason != FineTuneModelMissing {
		t.Fatalf("targets = %+v", targets)
	}
}

func TestFineTuneTargetsCatalogUnavailable(t *testing.T) {
	cfg := &config.Config{ModelLibraryCacheTTL: time.Minute, ModelLibraryTimeout: time.Second}
	svc := NewFineTuneService(
		NewModelCatalog(cfg, kube.DisabledSource),
		NewDatasetCatalog(cfg, kube.DisabledSource),
	)
	targets := svc.Targets(context.Background(), "biomistral-7b")
	if targets.Available || targets.Reason != FineTuneUnavailable {
		t.Fatalf("targets = %+v", targets)
	}
}

func TestFineTuneIgnoresUnknownVocabulary(t *testing.T) {
	mvs := []kube.ModelVersion{modelVersion("biomistral-7b", "mistral", map[string]string{
		metaFinetune: "lora",
		metaTasks:    "text-qa, bogus-task",
	})}
	targets := finetuneService(mvs, nil).Targets(context.Background(), "biomistral-7b")
	if len(targets.Tasks) != 1 || targets.Tasks[0] != TaskTextQA {
		t.Fatalf("tasks = %+v", targets.Tasks)
	}
}

func TestFineTuneDatasetTaskPrecedence(t *testing.T) {
	// meta.task wins over the format fallback: a TSV text set can be text-qa.
	task, annotated := datasetTask(kube.DatasetSpec{
		Format: "TSV",
		Meta:   map[string]string{metaTask: TaskTextQA},
	})
	if task != TaskTextQA || !annotated {
		t.Fatalf("task = %q annotated=%v", task, annotated)
	}
	task, annotated = datasetTask(kube.DatasetSpec{Format: "SDF / TSV"})
	if task != TaskMolecule || annotated {
		t.Fatalf("fallback task = %q annotated=%v", task, annotated)
	}
}

func TestFineTuneTargetsReadsAnnotations(t *testing.T) {
	// ModelVersion.spec is immutable in the cluster, so existing records carry
	// the capability as annotations instead.
	mv := modelVersion("biomistral-7b", "mistral", nil)
	mv.Metadata.Annotations = map[string]string{
		annotationFineTune:      "lora",
		annotationFineTuneTasks: "text-qa",
	}
	targets := finetuneService([]kube.ModelVersion{mv}, []kube.Dataset{dataset("medmcqa", "Parquet", "")}).
		Targets(context.Background(), "biomistral-7b")
	if !targets.Available || !targets.Annotated {
		t.Fatalf("targets = %+v", targets)
	}
	if len(targets.Datasets) != 1 || targets.Datasets[0].Name != "medmcqa" {
		t.Fatalf("datasets = %+v", targets.Datasets)
	}
}

func TestFineTuneSpecMetaWinsOverAnnotations(t *testing.T) {
	mv := modelVersion("biomistral-7b", "mistral", map[string]string{
		metaFinetune: "none",
		metaTasks:    "text-qa",
	})
	mv.Metadata.Annotations = map[string]string{
		annotationFineTune:      "lora",
		annotationFineTuneTasks: "text-qa",
	}
	targets := finetuneService([]kube.ModelVersion{mv}, nil).Targets(context.Background(), "biomistral-7b")
	if targets.Available {
		t.Fatalf("spec.meta.finetune=none should win: %+v", targets)
	}
}
