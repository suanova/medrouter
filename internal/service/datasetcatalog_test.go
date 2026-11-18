package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/suanova/medrouter/internal/kube"
)

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

func testDataset(name, dataset, version, format string, created time.Time, ann map[string]string) kube.Dataset {
	records := int64(120000)
	return kube.Dataset{
		Metadata: kube.ObjectMeta{
			Name:              name,
			Annotations:       ann,
			CreationTimestamp: created,
		},
		Spec: kube.DatasetSpec{
			Dataset:     dataset,
			Version:     version,
			Format:      format,
			Size:        "1.2Gi",
			RecordCount: &records,
			Storage: kube.ModelStorage{
				Strategy: "S3",
				S3: &kube.S3Storage{
					URI:            "s3://internal-bucket/datasets/demo",
					CredentialsRef: &kube.S3CredentialsRef{Name: "internal-credentials"},
				},
			},
		},
	}
}

func testDatasetCatalog() ([]kube.Dataset, *DatasetList) {
	base := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	ann := map[string]string{
		annotationDescription:      "Instruction tuning corpus",
		annotationDisplayName:      "Demo Instruction Set",
		annotationPublisher:        "Cube Labs",
		annotationTask:             "instruction-tuning",
		annotationLicense:          "CC-BY-4.0",
		annotationTags:             "sft, parquet, sft",
		annotationUsage:            "Training only",
		annotationFeatured:         "true",
		annotationAccess:           "public",
		annotationDeidentification: "PII removed",
		annotationEthics:           "Reviewed by the data board",
		annotationSourceRepo:       "https://example.com/demo-dataset",
	}
	v10 := testDataset("demo-dataset-v1-0-0", "demo-dataset", "v1.0.0", "Parquet", base, nil)
	v11 := testDataset("demo-dataset-v1-1-0", "demo-dataset", "v1.1.0", "Parquet", base.Add(48*time.Hour), ann)
	datasets := []kube.Dataset{v10, v11}
	return datasets, BuildDatasetList(datasets, base.Add(96*time.Hour))
}

func TestBuildDatasetListGroupsAndProjects(t *testing.T) {
	_, list := testDatasetCatalog()

	if !list.Available || list.Total != 1 || list.VersionTotal != 2 {
		t.Fatalf("list = %#v", list)
	}
	ds := list.Datasets[0]
	if ds.Dataset != "demo-dataset" || ds.DisplayName != "Demo Instruction Set" {
		t.Fatalf("dataset fields = %#v", ds)
	}
	if ds.LatestVersion != "v1.1.0" || ds.VersionCount != 2 {
		t.Fatalf("latest/version count = %s/%d", ds.LatestVersion, ds.VersionCount)
	}
	if ds.Publisher != "Cube Labs" || ds.PublisherInitials != "CL" {
		t.Fatalf("publisher = %q initials = %q", ds.Publisher, ds.PublisherInitials)
	}
	if ds.Format != "Parquet" || ds.Size != "1.2Gi" || ds.RecordCount == nil || *ds.RecordCount != 120000 {
		t.Fatalf("data facts = %#v", ds)
	}
	if ds.Task != "instruction-tuning" || ds.License != "CC-BY-4.0" || ds.Access != "public" {
		t.Fatalf("annotations = %#v", ds)
	}
	if ds.Deidentification != "PII removed" || ds.Ethics != "Reviewed by the data board" || ds.SourceRepo == "" {
		t.Fatalf("governance fields = %#v", ds)
	}
	if len(ds.Tags) != 2 || ds.Tags[0] != "sft" || ds.Tags[1] != "parquet" {
		t.Fatalf("tags = %#v", ds.Tags)
	}
	if !ds.Featured || ds.StorageStrategy != "S3" {
		t.Fatalf("storage/featured = %#v", ds)
	}
	if !ds.CreatedAt.Equal(baseTime()) || !ds.UpdatedAt.Equal(baseTime().Add(48*time.Hour)) {
		t.Fatalf("times = %s / %s", ds.CreatedAt, ds.UpdatedAt)
	}
}

func baseTime() time.Time {
	return time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
}

func TestBuildDatasetDetail(t *testing.T) {
	datasets, list := testDatasetCatalog()

	detail, ok := BuildDatasetDetail(list, datasets, "demo-dataset")
	if !ok {
		t.Fatal("dataset not found by spec.dataset")
	}
	if detail.LatestRef != "demo-dataset-v1-1-0" || len(detail.Versions) != 2 {
		t.Fatalf("detail = %#v", detail)
	}
	if detail.Versions[0].Version != "v1.1.0" || !detail.Versions[0].IsLatest {
		t.Fatalf("latest version = %#v", detail.Versions[0])
	}
	if detail.Versions[1].Version != "v1.0.0" || detail.Versions[1].IsLatest {
		t.Fatalf("second version = %#v", detail.Versions[1])
	}
	latest := detail.Versions[0]
	if latest.Storage.Strategy != "S3" || !latest.Storage.URIConfigured || !latest.Storage.CredentialsConfigured {
		t.Fatalf("storage summary = %#v", latest.Storage)
	}
	if latest.RecordCount == nil || *latest.RecordCount != 120000 {
		t.Fatalf("record count = %v", latest.RecordCount)
	}

	if _, ok := BuildDatasetDetail(list, datasets, "demo-dataset-v1-0-0"); !ok {
		t.Fatal("dataset not found by metadata.name")
	}
	if _, ok := BuildDatasetDetail(list, datasets, "missing"); ok {
		t.Fatal("missing dataset was found")
	}
}

func TestDatasetSummaryOmitsSecrets(t *testing.T) {
	datasets, _ := testDatasetCatalog()
	list := BuildDatasetList(datasets, time.Now())

	out := string(mustJSON(t, list))
	for _, secret := range []string{"internal-bucket", "internal-credentials", "s3://"} {
		if strings.Contains(out, secret) {
			t.Fatalf("response leaked %q: %s", secret, out)
		}
	}
}

func TestDatasetFallbacksWithoutAnnotations(t *testing.T) {
	ds := testDataset("tarball-v1", "tarball-set", "v1", "WebDataset", time.Now(), nil)
	ds.Spec.Size = ""
	ds.Spec.RecordCount = nil
	ds.Spec.Storage = kube.ModelStorage{
		Strategy: "Dynamic",
		Dynamic:  &kube.DynamicStorage{StorageClassName: "ceph", Capacity: "10Gi"},
	}
	list := BuildDatasetList([]kube.Dataset{ds}, time.Now())
	out := list.Datasets[0]

	if out.DisplayName != "tarball-set" || out.Publisher != "" || out.License != "" {
		t.Fatalf("fallbacks = %#v", out)
	}
	if out.PublisherInitials != "TA" {
		t.Fatalf("initials = %q", out.PublisherInitials)
	}
	if len(out.Tags) != 2 || out.Tags[0] != "WebDataset" || out.Tags[1] != "Dynamic" {
		t.Fatalf("derived tags = %#v", out.Tags)
	}
	if out.Size != "" || out.RecordCount != nil || out.Featured {
		t.Fatalf("optional fields = %#v", out)
	}

	detail, _ := BuildDatasetDetail(list, []kube.Dataset{ds}, "tarball-set")
	if detail.Versions[0].Storage.StorageClass != "ceph" || detail.Versions[0].Storage.Capacity != "10Gi" {
		t.Fatalf("storage = %#v", detail.Versions[0].Storage)
	}
}

func TestDatasetVersionNaturalOrder(t *testing.T) {
	base := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	older := testDataset("ds-v1-9", "ds", "v1.9", "JSONL", base.Add(time.Hour), nil)
	newer := testDataset("ds-v1-10", "ds", "v1.10", "Parquet", base, nil)
	list := BuildDatasetList([]kube.Dataset{older, newer}, base.Add(2*time.Hour))

	if got := list.Datasets[0].LatestVersion; got != "v1.10" {
		t.Fatalf("latest version = %q", got)
	}
	detail, _ := BuildDatasetDetail(list, []kube.Dataset{older, newer}, "ds")
	if detail.Versions[0].Ref != "ds-v1-10" || detail.Versions[1].Ref != "ds-v1-9" {
		t.Fatalf("order = %#v", detail.Versions)
	}
}

func TestDatasetGovernanceReadsAiCubestackAnnotations(t *testing.T) {
	base := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	ann := map[string]string{
		annotationAcquisitionMethod: "公开发布（HuggingFace 镜像 / 作者仓库）",
		annotationDeIdentification:  "不适用——不含患者个人信息",
		annotationEthicsBasis:       "无需 IRB；来源为公开医学考试题",
		annotationSourceRepository:  "https://huggingface.co/datasets/medmcqa/medmcqa",
	}
	ds := testDataset("medmcqa-v1", "medmcqa", "v1.0", "JSONL", base, ann)
	list := BuildDatasetList([]kube.Dataset{ds}, base.Add(time.Hour))
	got := list.Datasets[0]
	if got.Access != ann[annotationAcquisitionMethod] {
		t.Fatalf("access = %q", got.Access)
	}
	if got.Deidentification != ann[annotationDeIdentification] {
		t.Fatalf("deidentification = %q", got.Deidentification)
	}
	if got.Ethics != ann[annotationEthicsBasis] {
		t.Fatalf("ethics = %q", got.Ethics)
	}
	if got.SourceRepo != ann[annotationSourceRepository] {
		t.Fatalf("source_repo = %q", got.SourceRepo)
	}
}

func TestDatasetGovernancePrefersMedrouterKeys(t *testing.T) {
	base := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	ann := map[string]string{
		annotationAccess:           "public",
		annotationAcquisitionMethod: "should be ignored",
	}
	ds := testDataset("d-v1", "d", "v1.0", "JSONL", base, ann)
	list := BuildDatasetList([]kube.Dataset{ds}, base.Add(time.Hour))
	if got := list.Datasets[0].Access; got != "public" {
		t.Fatalf("access = %q", got)
	}
}

func TestParseDatasetOverview(t *testing.T) {
	body := `
dataset: demo-dataset
description: 一个用于指令微调的公开语料。
intendedUse: 医疗问答辅助。
limitations:
  - 覆盖度有限
benchmarks:
  - name: Demo-Bench
    metric: acc
    value: "0.91"
regulatoryStatus: 未注册
`
	ov, err := ParseDatasetOverview(body, "demo-dataset")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !ov.Valid {
		t.Fatalf("expected valid")
	}
	if ov.Description == "" || len(ov.Limitations) != 1 {
		t.Fatalf("unexpected parse: %+v", ov)
	}
}

func TestParseDatasetOverviewRejectsAnchorMismatch(t *testing.T) {
	body := "dataset: demo-a\ndescription: x\n"
	_, err := ParseDatasetOverview(body, "demo-b")
	if err == nil || !strings.Contains(err.Error(), "INVALID_DATASET_OVERVIEW") {
		t.Fatalf("expected INVALID_DATASET_OVERVIEW, got %v", err)
	}
}

func TestBuildDatasetOverviewsMatchesBySpecDataset(t *testing.T) {
	base := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	datasets := []kube.Dataset{
		testDataset("d1-v1", "d1", "v1.0", "JSONL", base, nil),
		testDataset("d2-v1", "d2", "v1.0", "JSONL", base, nil),
	}

	cms := []kube.OverviewConfigMap{
		// matches d1 -> kept
		{Metadata: kube.ObjectMeta{Name: "d1-overview"}, Data: map[string]string{
			"overview.yaml": "dataset: d1\ndescription: 匹配\n",
		}},
		// anchor mismatch (d3 does not exist) -> dropped
		{Metadata: kube.ObjectMeta{Name: "d3-overview"}, Data: map[string]string{
			"overview.yaml": "dataset: d3\ndescription: 未知\n",
		}},
		// no overview.yaml data -> dropped
		{Metadata: kube.ObjectMeta{Name: "d2-overview"}, Data: map[string]string{
			"other": "x",
		}},
	}

	m := buildDatasetOverviews(cms, datasets, nil)
	if len(m) != 1 {
		t.Fatalf("expected 1 kept overview, got %d", len(m))
	}
	if _, ok := m["d1"]; !ok {
		t.Fatalf("expected overview for d1, got %v", m)
	}
}
