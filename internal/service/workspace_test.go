package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/suanova/medrouter/internal/config"
	"github.com/suanova/medrouter/internal/kube"
	"github.com/suanova/medrouter/internal/model"
)

type fakeNotebooks struct {
	created                 [][3]string // namespace, name, spec JSON is checked separately
	specs                   []map[string]any
	labels                  []map[string]string
	state                   kube.NotebookState
	found                   bool
	err                     error
	deleted                 []string
	running                 []bool
	jobPhase                string
	inferenceReady          bool
	inferencePhase          string
	inferenceReason         string
	modelVersions           []string
	modelVersionSpecs       []map[string]any
	modelVersionAnnotations []map[string]string
	// baseModelVersion is what GetModelVersion returns; nil means not found.
	baseModelVersion *kube.ModelVersion
	// jobStates is per-job-name; when nil the fake falls back to (found, jobPhase).
	jobStates map[string]string
	// jupyterSecret is the token Secret the fake environment's status names;
	// token is what reading it yields.
	jupyterSecret string
	token         string
}

func (f *fakeNotebooks) CreateDevEnvironment(_ context.Context, namespace, name string, labels map[string]string, spec map[string]any) (kube.DevEnvironment, error) {
	if f.err != nil {
		return kube.DevEnvironment{}, f.err
	}
	f.created = append(f.created, [3]string{namespace, name, ""})
	f.specs = append(f.specs, spec)
	f.labels = append(f.labels, labels)
	return kube.DevEnvironment{}, nil
}

func (f *fakeNotebooks) GetDevEnvironment(_ context.Context, _, _ string) (kube.DevEnvironment, bool, error) {
	if f.err != nil {
		return kube.DevEnvironment{}, false, f.err
	}
	env := kube.DevEnvironment{}
	env.Status.Phase.Name = f.state.Phase
	if f.state.Endpoint != "" {
		env.Status.Endpoints = []kube.DevEnvironmentEndpoint{{Name: "jupyter", Address: f.state.Endpoint}}
	}
	if f.jupyterSecret != "" {
		env.Status.JupyterTokenSecret = &kube.SecretRef{Name: f.jupyterSecret}
	}
	return env, f.found, nil
}

func (f *fakeNotebooks) GetSecretValue(_ context.Context, _, name, key string) (string, bool, error) {
	if f.err != nil {
		return "", false, f.err
	}
	if name != f.jupyterSecret || key != "token" || f.token == "" {
		return "", false, nil
	}
	return f.token, true, nil
}

func (f *fakeNotebooks) SetDevEnvironmentRunning(_ context.Context, _, _ string, running bool) error {
	f.running = append(f.running, running)
	return f.err
}

func (f *fakeNotebooks) DeleteDevEnvironment(_ context.Context, _, name string) error {
	f.deleted = append(f.deleted, name)
	return f.err
}

func (f *fakeNotebooks) CreateJob(_ context.Context, namespace, name string, labels map[string]string, spec map[string]any) (kube.Job, error) {
	if f.err != nil {
		return kube.Job{}, f.err
	}
	f.created = append(f.created, [3]string{namespace, name, "job"})
	f.specs = append(f.specs, spec)
	f.labels = append(f.labels, labels)
	return kube.Job{}, nil
}

func (f *fakeNotebooks) GetJob(_ context.Context, _, name string) (kube.Job, bool, error) {
	job := kube.Job{}
	phase := f.jobPhase
	if f.jobStates != nil {
		value, ok := f.jobStates[name]
		if !ok {
			return kube.Job{}, false, f.err
		}
		phase = value
	} else if !f.found {
		return kube.Job{}, false, f.err
	}
	switch phase {
	case "Running":
		job.Status.Active = 1
	case "Succeeded":
		job.Status.Succeeded = 1
		job.Status.Conditions = []kube.Condition{{Type: "Complete", Status: "True"}}
	case "Failed":
		job.Status.Failed = 1
		job.Status.Conditions = []kube.Condition{{Type: "Failed", Status: "True", Reason: "BackoffLimitExceeded"}}
	}
	return job, true, f.err
}

func (f *fakeNotebooks) DeleteJob(_ context.Context, _, name string) error {
	f.deleted = append(f.deleted, name)
	return f.err
}

func (f *fakeNotebooks) CreateInferenceService(_ context.Context, namespace, name string, labels map[string]string, spec map[string]any) (kube.InferenceService, error) {
	if f.err != nil {
		return kube.InferenceService{}, f.err
	}
	f.created = append(f.created, [3]string{namespace, name, "inference"})
	f.specs = append(f.specs, spec)
	f.labels = append(f.labels, labels)
	return kube.InferenceService{}, nil
}

func (f *fakeNotebooks) GetInferenceService(_ context.Context, _, _ string) (kube.InferenceService, bool, error) {
	service := kube.InferenceService{}
	if f.inferenceReady {
		service.Status.Conditions = []kube.Condition{{Type: "Ready", Status: "True"}}
	}
	if f.inferencePhase != "" {
		service.Status.Phase = &kube.InferenceServicePhase{Name: f.inferencePhase, Reason: f.inferenceReason}
	}
	return service, f.found, f.err
}

func (f *fakeNotebooks) DeleteInferenceService(_ context.Context, _, name string) error {
	f.deleted = append(f.deleted, name)
	return f.err
}

func (f *fakeNotebooks) CreateModelVersion(_ context.Context, name string, _ map[string]string, annotations map[string]string, spec map[string]any) error {
	f.modelVersions = append(f.modelVersions, name)
	f.modelVersionSpecs = append(f.modelVersionSpecs, spec)
	f.modelVersionAnnotations = append(f.modelVersionAnnotations, annotations)
	return f.err
}

func (f *fakeNotebooks) ModelVersionExists(context.Context, string) (bool, error) {
	return false, f.err
}

func (f *fakeNotebooks) GetModelVersion(_ context.Context, name string) (kube.ModelVersion, bool, error) {
	if f.err != nil {
		return kube.ModelVersion{}, false, f.err
	}
	if f.baseModelVersion != nil && f.baseModelVersion.Metadata.Name == name {
		return *f.baseModelVersion, true, nil
	}
	return kube.ModelVersion{}, false, nil
}

func studioConfig(dryRun bool) *config.Config {
	return &config.Config{
		StudioEnabled:            true,
		StudioDryRun:             dryRun,
		StudioNamespace:          "medrouter-workspace",
		StudioNotebookImage:      "harbor.isuanova.com/suanova/jupyter-minimal:latest",
		StudioNotebookEgress:     true,
		StudioDatasetPVCPattern:  "%s-data",
		StudioMaxWorkspaces:      3,
		StudioFinetuneImage:      "harbor.example/medrouter-lora@sha256:abc",
		StudioFinetuneDeadline:   12 * time.Hour,
		StudioFinetuneMaxSamples: 20000,
		StudioGatewayRouter:      "cubestack-ai-gateway-private",
	}
}

// starDataset stars one dataset for the user so Create accepts it.
func starDataset(t *testing.T, userID int64, name string) {
	t.Helper()
	if _, apiErr := NewFavoriteService().Add(userID, FavoriteDataset, name); apiErr != nil {
		t.Fatalf("star: %v", apiErr)
	}
}

func TestWorkspaceCreateRequiresFavorite(t *testing.T) {
	favoriteTestDB(t)
	svc := NewWorkspaceService(studioConfig(true), nil, nil)

	_, apiErr := svc.Create(context.Background(), 1, FavoriteDataset, "pubmedqa", "")
	if apiErr == nil || apiErr.Code != "STAR_REQUIRED" {
		t.Fatalf("apiErr = %+v", apiErr)
	}
}

func TestWorkspaceDryRunReturnsPlan(t *testing.T) {
	favoriteTestDB(t)
	starDataset(t, 1, "pubmedqa")
	svc := NewWorkspaceService(studioConfig(true), nil, func(context.Context, string) (string, bool) {
		return "287Mi", true
	})

	plan, apiErr := svc.Create(context.Background(), 1, FavoriteDataset, "pubmedqa", "")
	if apiErr != nil {
		t.Fatalf("Create: %v", apiErr)
	}
	if !plan.DryRun || plan.Phase != WorkspacePlanned {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.Matched.CPU != "4" || plan.Matched.Memory != "8Gi" {
		t.Fatalf("matched = %+v", plan.Matched)
	}
	if plan.Matched.Volume != "pubmedqa-data" || plan.Matched.DatasetSize != "287Mi" {
		t.Fatalf("matched = %+v", plan.Matched)
	}
	if plan.Namespace != "medrouter-workspace" {
		t.Fatalf("namespace = %q", plan.Namespace)
	}
	// A dry run must not persist anything.
	if list, _ := svc.List(1); len(list) != 0 {
		t.Fatalf("dry run persisted %+v", list)
	}
}

func TestWorkspaceSizeTiers(t *testing.T) {
	cases := []struct{ size, cpu, memory string }{
		{"287Mi", "4", "8Gi"},
		{"9Gi", "4", "8Gi"},
		{"10Gi", "8", "16Gi"},
		{"99Gi", "8", "16Gi"},
		{"100Gi", "12", "24Gi"},
		{"2Ti", "12", "24Gi"},
		{"unknown", "4", "8Gi"},
	}
	for _, tc := range cases {
		cpu, memory := notebookTier(tc.size)
		if cpu != tc.cpu || memory != tc.memory {
			t.Fatalf("%s -> %s/%s, want %s/%s", tc.size, cpu, memory, tc.cpu, tc.memory)
		}
	}
}

func TestWorkspaceCreateCallsClusterAndRecordsRow(t *testing.T) {
	favoriteTestDB(t)
	starDataset(t, 7, "pubmedqa")
	cluster := &fakeNotebooks{}
	svc := NewWorkspaceService(studioConfig(false), cluster, nil)

	workspace, apiErr := svc.Create(context.Background(), 7, FavoriteDataset, "pubmedqa", "")
	if apiErr != nil {
		t.Fatalf("Create: %v", apiErr)
	}
	if workspace.Phase != model.WorkspaceCreating {
		t.Fatalf("phase = %q", workspace.Phase)
	}
	if len(cluster.created) != 1 || cluster.created[0][0] != "medrouter-workspace" {
		t.Fatalf("cluster calls = %+v", cluster.created)
	}
	if cluster.labels[0]["medrouter.io/owner"] != "7" {
		t.Fatalf("labels = %+v", cluster.labels[0])
	}
	// The dataset PVC is mounted read-only below the jupyter home, which is the
	// file browser's root, so it can be opened without leaving the UI.
	spec := cluster.specs[0]
	volumes, ok := spec["volumes"].([]any)
	if !ok || len(volumes) != 1 {
		t.Fatalf("volumes = %v", spec["volumes"])
	}
	volume := volumes[0].(map[string]any)
	if volume["pvcName"] != "pubmedqa-data" || volume["readOnly"] != true {
		t.Fatalf("volume = %+v", volume)
	}
	if volume["mountPath"] != "/home/jovyan/data/pubmedqa" {
		t.Fatalf("mountPath = %v", volume["mountPath"])
	}
	// Notebooks install packages on their own behalf, so egress is open by
	// default; the platform restricts it unless asked otherwise.
	network := spec["network"].(map[string]any)
	if network["egress"].(map[string]any)["unrestricted"] != true {
		t.Fatalf("network = %+v", network)
	}

	// Launching again returns the same workspace instead of creating a second.
	again, apiErr := svc.Create(context.Background(), 7, FavoriteDataset, "pubmedqa", "")
	if apiErr != nil || again.ID != workspace.ID {
		t.Fatalf("idempotent create = %+v / %v", again, apiErr)
	}
	if len(cluster.created) != 1 {
		t.Fatalf("cluster calls = %+v", cluster.created)
	}
}

func TestWorkspaceQuota(t *testing.T) {
	favoriteTestDB(t)
	cfg := studioConfig(false)
	cfg.StudioMaxWorkspaces = 1
	starDataset(t, 1, "pubmedqa")
	if _, apiErr := NewFavoriteService().Add(1, FavoriteModel, "biomistral-7b"); apiErr != nil {
		t.Fatalf("star model: %v", apiErr)
	}
	cluster := &fakeNotebooks{}
	svc := NewWorkspaceService(cfg, cluster, nil).
		WithDeployment(deployableModel, runtimeProfiles)

	if _, apiErr := svc.Create(context.Background(), 1, FavoriteDataset, "pubmedqa", ""); apiErr != nil {
		t.Fatalf("first: %v", apiErr)
	}
	// A different kind is allowed by the one-per-kind rule, so the per-user
	// quota is what stops it.
	_, apiErr := svc.Create(context.Background(), 1, FavoriteModel, "biomistral-7b", "")
	if apiErr == nil || apiErr.Code != "QUOTA_EXCEEDED" {
		t.Fatalf("apiErr = %+v", apiErr)
	}
}

func TestWorkspaceOnePerKind(t *testing.T) {
	favoriteTestDB(t)
	if !model.DB.Migrator().HasIndex(&model.Workspace{}, "idx_workspaces_owner_kind") {
		t.Fatal("the one-per-kind unique index was not created")
	}
	starDataset(t, 1, "pubmedqa")
	starDataset(t, 1, "chembl")
	cluster := &fakeNotebooks{}
	svc := NewWorkspaceService(studioConfig(false), cluster, nil)

	first, apiErr := svc.Create(context.Background(), 1, FavoriteDataset, "pubmedqa", "")
	if apiErr != nil {
		t.Fatalf("first notebook: %v", apiErr)
	}
	// A second environment is refused, whatever the dataset.
	_, apiErr = svc.Create(context.Background(), 1, FavoriteDataset, "chembl", "")
	if apiErr == nil || apiErr.Code != "WORKSPACE_LIMIT_REACHED" {
		t.Fatalf("second notebook: %+v", apiErr)
	}
	// Launching the same favorite again stays idempotent.
	again, apiErr := svc.Create(context.Background(), 1, FavoriteDataset, "pubmedqa", "")
	if apiErr != nil || again.ID != first.ID {
		t.Fatalf("idempotent notebook = %+v / %v", again, apiErr)
	}

	// A different kind is allowed (dry-run avoids a second cluster object).
	if _, apiErr := NewFavoriteService().Add(1, FavoriteModel, "biomistral-7b"); apiErr != nil {
		t.Fatalf("star model: %v", apiErr)
	}
	planSvc := NewWorkspaceService(studioConfig(true), nil, nil).
		WithDeployment(deployableModel, runtimeProfiles)
	if _, apiErr := planSvc.Create(context.Background(), 1, FavoriteModel, "biomistral-7b", ""); apiErr != nil {
		t.Fatalf("inference plan: %v", apiErr)
	}
}

func TestWorkspaceDryRunBlocksLifecycle(t *testing.T) {
	favoriteTestDB(t)
	starDataset(t, 1, "pubmedqa")
	svc := NewWorkspaceService(studioConfig(true), nil, nil)

	plan, apiErr := svc.Create(context.Background(), 1, FavoriteDataset, "pubmedqa", "")
	if apiErr != nil {
		t.Fatalf("Create: %v", apiErr)
	}
	if _, apiErr := svc.SetRunning(context.Background(), 1, plan.ID, true); apiErr == nil {
		t.Fatal("dry run should refuse to change anything")
	}
}

func TestWorkspaceReconcileProjectsStatus(t *testing.T) {
	favoriteTestDB(t)
	starDataset(t, 1, "pubmedqa")
	cluster := &fakeNotebooks{found: true, state: kube.NotebookState{Phase: "Running", Endpoint: "https://dev.example/ws/x/y"}}
	svc := NewWorkspaceService(studioConfig(false), cluster, nil)

	workspace, apiErr := svc.Create(context.Background(), 1, FavoriteDataset, "pubmedqa", "")
	if apiErr != nil {
		t.Fatalf("Create: %v", apiErr)
	}

	// The fake returns the state attached at construction; make it visible.
	cluster.state = kube.NotebookState{Phase: "Running", Endpoint: "https://dev.example/ws/x/y"}
	svc.Reconcile(context.Background())

	list, _ := svc.List(1)
	if len(list) != 1 || list[0].ID != workspace.ID {
		t.Fatalf("list = %+v", list)
	}
	if list[0].Phase != model.WorkspaceRunning || list[0].Endpoint != "https://dev.example/ws/x/y" {
		t.Fatalf("workspace = %+v", list[0])
	}
}

func TestWorkspaceDerivePhase(t *testing.T) {
	cases := []struct{ phase, want string }{
		{"Running", model.WorkspaceRunning},
		{"Pending", model.WorkspaceCreating},
		{"Stopped", model.WorkspaceStopped},
		{"Failed", model.WorkspaceFailed},
	}
	for _, tc := range cases {
		got, _ := derivePhase(kube.NotebookState{Phase: tc.phase})
		if got != tc.want {
			t.Fatalf("%s -> %s, want %s", tc.phase, got, tc.want)
		}
	}
}

func TestParseByteSize(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"287Mi", 287 << 20, true},
		{"10Gi", 10 << 30, true},
		{"1.5Gi", int64(1.5 * float64(1<<30)), true},
		{"512M", 512_000_000, true},
		{"42", 42, true},
		{"", 0, false},
		{"many", 0, false},
	}
	for _, tc := range cases {
		got, ok := parseByteSize(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("%q -> %d/%v, want %d/%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func finetuneWorkspaceService(t *testing.T, dryRun bool, cluster *fakeNotebooks, storage kube.ModelStorage) *WorkspaceService {
	t.Helper()
	mvs := []kube.ModelVersion{modelVersion("biomistral-7b", "mistral", map[string]string{
		metaFinetune: "lora",
		metaTasks:    "text-qa",
	})}
	datasets := []kube.Dataset{
		dataset("medmcqa", "Parquet", TaskTextQA),
		dataset("chembl", "SDF / TSV", TaskMolecule),
	}
	lookup := func(context.Context, string) (kube.ModelStorage, bool) { return storage, true }
	return NewWorkspaceService(studioConfig(dryRun), cluster, nil).
		WithFineTune(lookup, finetuneService(mvs, datasets))
}

func hostPathStorage() kube.ModelStorage {
	return kube.ModelStorage{Strategy: "HostPath", HostPath: &kube.HostPathStorage{Path: "/mnt/cephfs/models/BioMistral-7B"}}
}

func TestWorkspaceFineTuneDryRun(t *testing.T) {
	favoriteTestDB(t)
	if _, apiErr := NewFavoriteService().Add(1, FavoriteModel, "biomistral-7b"); apiErr != nil {
		t.Fatalf("star: %v", apiErr)
	}
	svc := finetuneWorkspaceService(t, true, nil, hostPathStorage())

	plan, apiErr := svc.Create(context.Background(), 1, FavoriteModel, "biomistral-7b", "medmcqa")
	if apiErr != nil {
		t.Fatalf("Create: %v", apiErr)
	}
	if plan.Kind != WorkspaceFinetune || plan.Phase != WorkspacePlanned || !plan.DryRun {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.Matched.GPU != 1 || plan.Matched.BaseModel != "/mnt/cephfs/models/BioMistral-7B" {
		t.Fatalf("matched = %+v", plan.Matched)
	}
	if plan.Matched.Dataset != "medmcqa" || plan.Matched.Volume != "medmcqa-data" {
		t.Fatalf("matched = %+v", plan.Matched)
	}
	if plan.Matched.Output != "/mnt/cephfs/models/BioMistral-7B/.adapters/"+plan.Name {
		t.Fatalf("output = %q", plan.Matched.Output)
	}
	if list, _ := svc.List(1); len(list) != 0 {
		t.Fatalf("dry run persisted %+v", list)
	}
}

func TestWorkspaceFineTuneCreatesJob(t *testing.T) {
	favoriteTestDB(t)
	if _, apiErr := NewFavoriteService().Add(1, FavoriteModel, "biomistral-7b"); apiErr != nil {
		t.Fatalf("star: %v", apiErr)
	}
	cluster := &fakeNotebooks{}
	svc := finetuneWorkspaceService(t, false, cluster, hostPathStorage())

	workspace, apiErr := svc.Create(context.Background(), 1, FavoriteModel, "biomistral-7b", "medmcqa")
	if apiErr != nil {
		t.Fatalf("Create: %v", apiErr)
	}
	if workspace.Kind != WorkspaceFinetune || workspace.Phase != model.WorkspaceCreating {
		t.Fatalf("workspace = %+v", workspace)
	}
	if len(cluster.created) != 1 || cluster.created[0][2] != "job" {
		t.Fatalf("cluster calls = %+v", cluster.created)
	}
	spec := cluster.specs[0]
	if spec["activeDeadlineSeconds"] != int64(12*3600) {
		t.Fatalf("deadline = %v", spec["activeDeadlineSeconds"])
	}
	template := spec["template"].(map[string]any)["spec"].(map[string]any)
	container := template["containers"].([]any)[0].(map[string]any)
	args := container["args"].([]string)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--dataset-dir /data/medmcqa") ||
		!strings.Contains(joined, "--output-dir /models/base/.adapters/"+workspace.Name) ||
		!strings.Contains(joined, "--max-samples 20000") {
		t.Fatalf("args = %v", args)
	}
	resources := container["resources"].(map[string]any)["limits"].(map[string]any)
	if resources["metax-tech.com/gpu"] != "1" {
		t.Fatalf("limits = %+v", resources)
	}
	volumes := template["volumes"].([]any)
	if len(volumes) != 2 {
		t.Fatalf("volumes = %+v", volumes)
	}
}

func TestWorkspaceFineTuneRejectsIncompatibleDataset(t *testing.T) {
	favoriteTestDB(t)
	if _, apiErr := NewFavoriteService().Add(1, FavoriteModel, "biomistral-7b"); apiErr != nil {
		t.Fatalf("star: %v", apiErr)
	}
	svc := finetuneWorkspaceService(t, true, nil, hostPathStorage())

	_, apiErr := svc.Create(context.Background(), 1, FavoriteModel, "biomistral-7b", "chembl")
	if apiErr == nil || apiErr.Code != "DATASET_NOT_COMPATIBLE" {
		t.Fatalf("apiErr = %+v", apiErr)
	}
}

func TestWorkspaceFineTuneNeedsHostPathWeights(t *testing.T) {
	favoriteTestDB(t)
	if _, apiErr := NewFavoriteService().Add(1, FavoriteModel, "biomistral-7b"); apiErr != nil {
		t.Fatalf("star: %v", apiErr)
	}
	svc := finetuneWorkspaceService(t, true, nil, kube.ModelStorage{Strategy: "S3"})

	_, apiErr := svc.Create(context.Background(), 1, FavoriteModel, "biomistral-7b", "medmcqa")
	if apiErr == nil || apiErr.Code != "MODEL_STORAGE_UNSUPPORTED" {
		t.Fatalf("apiErr = %+v", apiErr)
	}
}

func TestWorkspaceFineTuneReconcileProjectsJob(t *testing.T) {
	favoriteTestDB(t)
	if _, apiErr := NewFavoriteService().Add(1, FavoriteModel, "biomistral-7b"); apiErr != nil {
		t.Fatalf("star: %v", apiErr)
	}
	cluster := &fakeNotebooks{found: true, jobPhase: "Succeeded"}
	svc := finetuneWorkspaceService(t, false, cluster, hostPathStorage())

	workspace, apiErr := svc.Create(context.Background(), 1, FavoriteModel, "biomistral-7b", "medmcqa")
	if apiErr != nil {
		t.Fatalf("Create: %v", apiErr)
	}
	svc.Reconcile(context.Background())

	list, _ := svc.List(1)
	if len(list) != 1 || list[0].ID != workspace.ID || list[0].Phase != model.WorkspaceSucceeded {
		t.Fatalf("list = %+v", list)
	}
}

func TestDeriveJobPhase(t *testing.T) {
	complete := kube.JobStatus{Conditions: []kube.Condition{{Type: "Complete", Status: "True"}}}
	if phase, _ := deriveJobPhase(complete); phase != model.WorkspaceSucceeded {
		t.Fatalf("complete -> %s", phase)
	}
	failed := kube.JobStatus{Conditions: []kube.Condition{{Type: "Failed", Status: "True", Reason: "BackoffLimitExceeded"}}}
	if phase, reason := deriveJobPhase(failed); phase != model.WorkspaceFailed || reason != "BackoffLimitExceeded" {
		t.Fatalf("failed -> %s/%s", phase, reason)
	}
	// Jobs expose counters, not a phase.
	if phase, _ := deriveJobPhase(kube.JobStatus{Active: 1}); phase != model.WorkspaceRunning {
		t.Fatalf("active -> %s", phase)
	}
	if phase, _ := deriveJobPhase(kube.JobStatus{Succeeded: 1}); phase != model.WorkspaceSucceeded {
		t.Fatalf("succeeded counter -> %s", phase)
	}
	if phase, reason := deriveJobPhase(kube.JobStatus{Failed: 1, Reason: "DeadlineExceeded"}); phase != model.WorkspaceFailed || reason != "DeadlineExceeded" {
		t.Fatalf("failed counter -> %s/%s", phase, reason)
	}
}

func deployableModel(context.Context, string) (ModelArtifact, bool) {
	return ModelArtifact{
		Name: "biomistral-7b-bf16", Model: "biomistral-7b",
		Architecture: "mistral", Quantization: "bf16",
		Storage: hostPathStorage(),
	}, true
}

func runtimeProfiles(context.Context) []kube.RuntimeProfile {
	return []kube.RuntimeProfile{
		{Metadata: kube.ObjectMeta{Name: "metax-vllm-biomistral"},
			Spec: kube.RuntimeProfileSpec{
				Engine:            kube.Engine{Name: "vllm", Version: "0.8"},
				ModelRequirements: kube.ModelRequirements{Architectures: []string{"mistral"}, Quantization: []string{"bf16"}},
			}},
		{Metadata: kube.ObjectMeta{Name: "metax-sglang-biomistral"},
			Spec: kube.RuntimeProfileSpec{
				Engine:            kube.Engine{Name: "sglang", Version: "0.5.14"},
				ModelRequirements: kube.ModelRequirements{Architectures: []string{"mistral"}, Quantization: []string{"bf16"}},
			}},
	}
}

func TestMatchProfilePrefersSGLang(t *testing.T) {
	profile, ok := matchProfile(runtimeProfiles(context.Background()), "mistral", "bf16")
	if !ok || profile.Metadata.Name != "metax-sglang-biomistral" {
		t.Fatalf("profile = %+v ok=%v", profile, ok)
	}
	if _, ok := matchProfile(runtimeProfiles(context.Background()), "mistral", "fp16"); ok {
		t.Fatal("quantization mismatch should not match")
	}
	if _, ok := matchProfile(nil, "mistral", "bf16"); ok {
		t.Fatal("no profiles should not match")
	}
}

func TestWorkspaceDeployDryRun(t *testing.T) {
	favoriteTestDB(t)
	if _, apiErr := NewFavoriteService().Add(1, FavoriteModel, "biomistral-7b"); apiErr != nil {
		t.Fatalf("star: %v", apiErr)
	}
	svc := NewWorkspaceService(studioConfig(true), nil, nil).
		WithDeployment(deployableModel, runtimeProfiles)

	plan, apiErr := svc.Create(context.Background(), 1, FavoriteModel, "biomistral-7b", "")
	if apiErr != nil {
		t.Fatalf("Create: %v", apiErr)
	}
	if plan.Kind != WorkspaceInference || plan.Phase != WorkspacePlanned || !plan.DryRun {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.Matched.Profile != "metax-sglang-biomistral" || plan.Matched.ModelVersion != "biomistral-7b-bf16" {
		t.Fatalf("matched = %+v", plan.Matched)
	}
	if plan.Matched.Router != "cubestack-ai-gateway-private" {
		t.Fatalf("router = %q", plan.Matched.Router)
	}
}

func TestWorkspaceDeployCreatesInferenceService(t *testing.T) {
	favoriteTestDB(t)
	if _, apiErr := NewFavoriteService().Add(1, FavoriteModel, "biomistral-7b"); apiErr != nil {
		t.Fatalf("star: %v", apiErr)
	}
	cluster := &fakeNotebooks{}
	svc := NewWorkspaceService(studioConfig(false), cluster, nil).
		WithDeployment(deployableModel, runtimeProfiles)

	workspace, apiErr := svc.Create(context.Background(), 1, FavoriteModel, "biomistral-7b", "")
	if apiErr != nil {
		t.Fatalf("Create: %v", apiErr)
	}
	if workspace.Kind != WorkspaceInference || workspace.Phase != model.WorkspaceCreating {
		t.Fatalf("workspace = %+v", workspace)
	}
	if len(cluster.created) != 1 || cluster.created[0][2] != "inference" {
		t.Fatalf("cluster calls = %+v", cluster.created)
	}
	spec := cluster.specs[0]
	if spec["modelRef"] != "biomistral-7b-bf16" || spec["profileRef"] != "metax-sglang-biomistral" {
		t.Fatalf("spec = %+v", spec)
	}
	route := spec["route"].(map[string]any)
	if route["aigatewayRouter"] != "cubestack-ai-gateway-private" {
		t.Fatalf("route = %+v", route)
	}
}

func TestWorkspaceDeployNeedsMatchingProfile(t *testing.T) {
	favoriteTestDB(t)
	if _, apiErr := NewFavoriteService().Add(1, FavoriteModel, "esm2"); apiErr != nil {
		t.Fatalf("star: %v", apiErr)
	}
	unknown := func(context.Context, string) (ModelArtifact, bool) {
		return ModelArtifact{Name: "esm2-fp32", Model: "esm2", Architecture: "esm2", Quantization: "fp32", Storage: hostPathStorage()}, true
	}
	svc := NewWorkspaceService(studioConfig(true), nil, nil).WithDeployment(unknown, runtimeProfiles)

	_, apiErr := svc.Create(context.Background(), 1, FavoriteModel, "esm2", "")
	if apiErr == nil || apiErr.Code != "NO_PROFILE" {
		t.Fatalf("apiErr = %+v", apiErr)
	}
}

func TestWorkspaceFineTuneRegistersModelVersion(t *testing.T) {
	favoriteTestDB(t)
	if _, apiErr := NewFavoriteService().Add(1, FavoriteModel, "biomistral-7b"); apiErr != nil {
		t.Fatalf("star: %v", apiErr)
	}
	base := modelVersion("biomistral-7b", "mistral", map[string]string{
		metaFinetune: "lora", metaTasks: "text-qa",
	})
	base.Metadata.Name = "biomistral-7b-bf16"
	base.Metadata.Annotations = map[string]string{
		"ai.cubestack.io/publisher":   "BioMistral",
		"ai.cubestack.io/description": "Bio-Mistral-7B base description",
		"medrouter.io/display-name":   "Bio-Mistral-7B",
		"medrouter.io/task":           "文本生成",
		"medrouter.io/usage-boundary": "研究用途",
		"medrouter.io/lora-adapter":   "must-not-win",
	}
	mvs := []kube.ModelVersion{base}
	datasets := []kube.Dataset{dataset("medmcqa", "Parquet", TaskTextQA)}
	lookup := func(context.Context, string) (kube.ModelStorage, bool) { return hostPathStorage(), true }

	cluster := &fakeNotebooks{jobStates: map[string]string{}, baseModelVersion: &base}
	svc := NewWorkspaceService(studioConfig(false), cluster, nil).
		WithFineTune(lookup, finetuneService(mvs, datasets)).
		WithDeployment(deployableModel, runtimeProfiles)

	workspace, apiErr := svc.Create(context.Background(), 1, FavoriteModel, "biomistral-7b", "medmcqa")
	if apiErr != nil {
		t.Fatalf("Create: %v", apiErr)
	}

	// Training succeeded: the adapter is registered directly, no merge job.
	cluster.jobStates[workspace.Name] = "Succeeded"
	svc.Reconcile(context.Background())

	list, _ := svc.List(1)
	if len(list) != 1 || list[0].Phase != model.WorkspaceReady || list[0].ModelVersion != workspace.Name {
		t.Fatalf("workspace = %+v", list)
	}
	if len(cluster.modelVersions) != 1 || cluster.modelVersions[0] != workspace.Name {
		t.Fatalf("model versions = %+v", cluster.modelVersions)
	}
	// The platform requires metadata.name == <model>-<version>, so the version
	// is the part that distinguishes this adapter, not a repeat of the name.
	spec := cluster.modelVersionSpecs[0]
	if spec["version"] != "medmcqa-u1-lora" {
		t.Fatalf("version = %v", spec["version"])
	}
	if spec["model"].(string)+"-"+spec["version"].(string) != cluster.modelVersions[0] {
		t.Fatalf("name %q is not <model>-<version> (%v)", cluster.modelVersions[0], spec)
	}
	if cluster.modelVersionAnnotations[0]["medrouter.io/lora-adapter"] != ".adapters/"+workspace.Name {
		t.Fatalf("annotations = %+v", cluster.modelVersionAnnotations[0])
	}
	// The catalog card is built from the newest version's annotations, so the
	// adapter must inherit the base version's catalog/display facts.
	annotations := cluster.modelVersionAnnotations[0]
	for key, want := range map[string]string{
		"ai.cubestack.io/publisher":   "BioMistral",
		"ai.cubestack.io/description": "Bio-Mistral-7B base description",
		"medrouter.io/display-name":   "Bio-Mistral-7B",
		"medrouter.io/task":           "文本生成",
		"medrouter.io/usage-boundary": "研究用途",
	} {
		if annotations[key] != want {
			t.Fatalf("annotation %s = %q, want %q (%+v)", key, annotations[key], want, annotations)
		}
	}
	if cluster.modelVersionSpecs[0]["storage"].(map[string]any)["hostPath"].(map[string]any)["path"] != "/mnt/cephfs/models/BioMistral-7B" {
		t.Fatalf("storage = %+v", cluster.modelVersionSpecs[0]["storage"])
	}
}

func TestWorkspaceDeployFineTunedVersionUsesLoRA(t *testing.T) {
	favoriteTestDB(t)
	if _, apiErr := NewFavoriteService().Add(1, FavoriteModel, "biomistral-7b"); apiErr != nil {
		t.Fatalf("star: %v", apiErr)
	}
	adapterModel := func(context.Context, string) (ModelArtifact, bool) {
		return ModelArtifact{
			Name: "biomistral-7b-medmcqa-u1-lora", Model: "biomistral-7b",
			Architecture: "mistral", Quantization: "bf16",
			Storage: hostPathStorage(),
			Adapter: ".adapters/biomistral-7b-medmcqa-u1-lora",
		}, true
	}
	cluster := &fakeNotebooks{}
	svc := NewWorkspaceService(studioConfig(false), cluster, nil).WithDeployment(adapterModel, runtimeProfiles)

	workspace, apiErr := svc.Create(context.Background(), 1, FavoriteModel, "biomistral-7b", "")
	if apiErr != nil {
		t.Fatalf("Create: %v", apiErr)
	}
	if workspace.Matched.Adapter != ".adapters/biomistral-7b-medmcqa-u1-lora" {
		t.Fatalf("matched = %+v", workspace.Matched)
	}
	/// The adapter is addressed through ${MODEL_PATH}, which the entrypoint
	/// expands, so the value does not depend on the operator's mount path.
	overrides := cluster.specs[0]["overrides"].(map[string]any)
	want := "biomistral-7b-medmcqa-u1-lora=${MODEL_PATH}/.adapters/biomistral-7b-medmcqa-u1-lora"
	if overrides["lora_paths"] != want {
		t.Fatalf("lora_paths = %v", overrides["lora_paths"])
	}
}

func TestWorkspaceDeployBaseHasNoLoRAOverride(t *testing.T) {
	favoriteTestDB(t)
	if _, apiErr := NewFavoriteService().Add(1, FavoriteModel, "biomistral-7b"); apiErr != nil {
		t.Fatalf("star: %v", apiErr)
	}
	cluster := &fakeNotebooks{}
	svc := NewWorkspaceService(studioConfig(false), cluster, nil).WithDeployment(deployableModel, runtimeProfiles)

	if _, apiErr := svc.Create(context.Background(), 1, FavoriteModel, "biomistral-7b", ""); apiErr != nil {
		t.Fatalf("Create: %v", apiErr)
	}
	overrides := cluster.specs[0]["overrides"].(map[string]any)
	if _, ok := overrides["lora_paths"]; ok {
		t.Fatalf("base deployment must not set lora_paths: %+v", overrides)
	}
}

func TestPublicEndpointRewritesHostAndKeepsPath(t *testing.T) {
	cases := []struct{ endpoint, base, want string }{
		{"http://106.75.251.248/dev/medrouter-workspace/lab/", "https://www.medrouter.com:8443", "https://www.medrouter.com:8443/dev/medrouter-workspace/lab/"},
		{"http://106.75.251.248/dev/medrouter-workspace/lab/", "https://www.medrouter.com:8443/", "https://www.medrouter.com:8443/dev/medrouter-workspace/lab/"},
		{"http://10.66.3.166:8080/dev/ns/lab", "https://www.medrouter.com", "https://www.medrouter.com/dev/ns/lab"},
		{"http://10.66.3.166/dev/ns/lab?token=abc", "https://www.medrouter.com", "https://www.medrouter.com/dev/ns/lab?token=abc"},
		{"http://106.75.251.248/dev/ns/lab/", "", "http://106.75.251.248/dev/ns/lab/"},
		{"", "https://www.medrouter.com", ""},
		{"not a url", "https://www.medrouter.com", "not a url"},
	}
	for _, tc := range cases {
		if got := publicEndpoint(tc.endpoint, tc.base); got != tc.want {
			t.Fatalf("publicEndpoint(%q, %q) = %q, want %q", tc.endpoint, tc.base, got, tc.want)
		}
	}
}

func TestWorkspaceReconcileRetriesRegistrationAfterSuccess(t *testing.T) {
	favoriteTestDB(t)
	if _, apiErr := NewFavoriteService().Add(1, FavoriteModel, "biomistral-7b"); apiErr != nil {
		t.Fatalf("star: %v", apiErr)
	}
	mvs := []kube.ModelVersion{modelVersion("biomistral-7b", "mistral", map[string]string{
		metaFinetune: "lora", metaTasks: "text-qa",
	})}
	mvs[0].Metadata.Name = "biomistral-7b-bf16"
	datasets := []kube.Dataset{dataset("medmcqa", "Parquet", TaskTextQA)}
	lookup := func(context.Context, string) (kube.ModelStorage, bool) { return hostPathStorage(), true }

	cluster := &fakeNotebooks{jobStates: map[string]string{}}
	svc := NewWorkspaceService(studioConfig(false), cluster, nil).
		WithFineTune(lookup, finetuneService(mvs, datasets)).
		WithDeployment(deployableModel, runtimeProfiles)

	workspace, apiErr := svc.Create(context.Background(), 1, FavoriteModel, "biomistral-7b", "medmcqa")
	if apiErr != nil {
		t.Fatalf("Create: %v", apiErr)
	}

	// The Job finished but the registration attempt was missed (reconciler
	// down, transient API failure). A row parked at succeeded must still be
	// picked up on the next pass.
	cluster.jobStates[workspace.Name] = "Succeeded"
	if err := model.DB.Model(&model.Workspace{}).Where("id = ?", workspace.ID).
		Update("phase", model.WorkspaceSucceeded).Error; err != nil {
		t.Fatalf("park row: %v", err)
	}
	svc.Reconcile(context.Background())

	list, _ := svc.List(1)
	if len(list) != 1 || list[0].Phase != model.WorkspaceReady || list[0].ModelVersion != workspace.Name {
		t.Fatalf("workspace = %+v", list)
	}
	if len(cluster.modelVersions) != 1 || cluster.modelVersions[0] != workspace.Name {
		t.Fatalf("model versions = %+v", cluster.modelVersions)
	}
}

func TestNotebookSessionUsesPublicEndpoint(t *testing.T) {
	favoriteTestDB(t)
	starDataset(t, 7, "medmcqa")
	cluster := &fakeNotebooks{}
	cfg := studioConfig(false)
	cfg.PublicOrigin = "https://www.medrouter.com:8443"
	svc := NewWorkspaceService(cfg, cluster, nil)

	workspace, apiErr := svc.Create(context.Background(), 7, FavoriteDataset, "medmcqa", "")
	if apiErr != nil {
		t.Fatalf("Create: %v", apiErr)
	}

	// The operator publishes the node address and a token Secret.
	cluster.found = true
	cluster.state = kube.NotebookState{Phase: "Running", Endpoint: "http://106.75.251.248/dev/medrouter-workspace/" + workspace.Name + "/"}
	cluster.jupyterSecret = workspace.Name + "-jupyter-token"
	cluster.token = "server-token"

	session, apiErr := svc.NotebookSession(context.Background(), 7, workspace.ID)
	if apiErr != nil {
		t.Fatalf("NotebookSession: %v", apiErr)
	}
	wantPublic := "https://www.medrouter.com:8443/dev/medrouter-workspace/" + workspace.Name + "/lab"
	if session.URL != wantPublic {
		t.Fatalf("url = %q, want %q", session.URL, wantPublic)
	}
	// The login must address the same host as the browser, because Jupyter
	// names its identity cookie after the Host it was asked with.
	if session.Upstream != wantPublic {
		t.Fatalf("upstream = %q, want %q", session.Upstream, wantPublic)
	}
	if session.Token != "server-token" {
		t.Fatalf("token = %q", session.Token)
	}

	// Another user cannot open it.
	if _, apiErr := svc.NotebookSession(context.Background(), 8, workspace.ID); apiErr == nil {
		t.Fatal("another user must not open the notebook")
	}
}

func TestNotebookSessionRequiresRunningEnvironment(t *testing.T) {
	favoriteTestDB(t)
	starDataset(t, 7, "medmcqa")
	cluster := &fakeNotebooks{}
	svc := NewWorkspaceService(studioConfig(false), cluster, nil)

	workspace, apiErr := svc.Create(context.Background(), 7, FavoriteDataset, "medmcqa", "")
	if apiErr != nil {
		t.Fatalf("Create: %v", apiErr)
	}
	cluster.found = true
	cluster.state = kube.NotebookState{Phase: "Pending"}
	if _, apiErr := svc.NotebookSession(context.Background(), 7, workspace.ID); apiErr == nil || apiErr.Code != "NOTEBOOK_NOT_RUNNING" {
		t.Fatalf("apiErr = %+v", apiErr)
	}

	cluster.state = kube.NotebookState{Phase: "Running", Endpoint: "http://10.0.0.1/dev/ns/lab/"}
	if _, apiErr := svc.NotebookSession(context.Background(), 7, workspace.ID); apiErr == nil || apiErr.Code != "NOTEBOOK_TOKEN_MISSING" {
		t.Fatalf("apiErr = %+v", apiErr)
	}
}

func TestWorkspaceFineTuneRetriesRegistrationOnceBaseResolves(t *testing.T) {
	favoriteTestDB(t)
	if _, apiErr := NewFavoriteService().Add(1, FavoriteModel, "biomistral-7b"); apiErr != nil {
		t.Fatalf("star: %v", apiErr)
	}
	mvs := []kube.ModelVersion{modelVersion("biomistral-7b", "mistral", map[string]string{
		metaFinetune: "lora", metaTasks: "text-qa",
	})}
	mvs[0].Metadata.Name = "biomistral-7b-bf16"
	datasets := []kube.Dataset{dataset("medmcqa", "Parquet", TaskTextQA)}
	lookup := func(context.Context, string) (kube.ModelStorage, bool) { return hostPathStorage(), true }

	// The catalog is loaded after the first reconcile, so the base model can be
	// unresolved for a moment while an already-finished Job is being registered.
	catalogReady := false
	modelInfo := func(ctx context.Context, ref string) (ModelArtifact, bool) {
		if !catalogReady {
			return ModelArtifact{}, false
		}
		return deployableModel(ctx, ref)
	}

	cluster := &fakeNotebooks{jobStates: map[string]string{}}
	svc := NewWorkspaceService(studioConfig(false), cluster, nil).
		WithFineTune(lookup, finetuneService(mvs, datasets)).
		WithDeployment(modelInfo, runtimeProfiles)

	workspace, apiErr := svc.Create(context.Background(), 1, FavoriteModel, "biomistral-7b", "medmcqa")
	if apiErr != nil {
		t.Fatalf("Create: %v", apiErr)
	}
	cluster.jobStates[workspace.Name] = "Succeeded"

	// Training finished, registration cannot run yet: the run is reported as
	// finished with the reason, not as a failed training.
	svc.Reconcile(context.Background())
	list, _ := svc.List(1)
	if len(list) != 1 || list[0].Phase != model.WorkspaceSucceeded || list[0].Reason != "BASE_MODEL_MISSING" {
		t.Fatalf("workspace = %+v", list)
	}

	// The catalog arrives; the next pass registers the adapter.
	catalogReady = true
	svc.Reconcile(context.Background())
	list, _ = svc.List(1)
	if len(list) != 1 || list[0].Phase != model.WorkspaceReady || list[0].ModelVersion != workspace.Name {
		t.Fatalf("workspace = %+v", list)
	}
	if len(cluster.modelVersions) != 1 || cluster.modelVersions[0] != workspace.Name {
		t.Fatalf("model versions = %+v", cluster.modelVersions)
	}
}

func TestWorkspaceDeployAfterFineTuneCreatesInference(t *testing.T) {
	favoriteTestDB(t)
	if _, apiErr := NewFavoriteService().Add(1, FavoriteModel, "biomistral-7b"); apiErr != nil {
		t.Fatalf("star: %v", apiErr)
	}
	mvs := []kube.ModelVersion{modelVersion("biomistral-7b", "mistral", map[string]string{
		metaFinetune: "lora", metaTasks: "text-qa",
	})}
	mvs[0].Metadata.Name = "biomistral-7b-bf16"
	datasets := []kube.Dataset{dataset("medmcqa", "Parquet", TaskTextQA)}
	lookup := func(context.Context, string) (kube.ModelStorage, bool) { return hostPathStorage(), true }

	cluster := &fakeNotebooks{jobStates: map[string]string{}}
	svc := NewWorkspaceService(studioConfig(false), cluster, nil).
		WithFineTune(lookup, finetuneService(mvs, datasets)).
		WithDeployment(deployableModel, runtimeProfiles)

	finetune, apiErr := svc.Create(context.Background(), 1, FavoriteModel, "biomistral-7b", "medmcqa")
	if apiErr != nil {
		t.Fatalf("Create finetune: %v", apiErr)
	}

	// Deploying the same model must not return the fine-tune: they are two
	// workspaces, and the launch that asks for a deployment has to create one.
	deploy, apiErr := svc.Create(context.Background(), 1, FavoriteModel, "biomistral-7b", "")
	if apiErr != nil {
		t.Fatalf("Create inference: %v", apiErr)
	}
	if deploy.ID == finetune.ID || deploy.Kind != WorkspaceInference {
		t.Fatalf("deploy = %+v, finetune = %+v", deploy, finetune)
	}
	if len(cluster.created) != 2 || cluster.created[1][2] != "inference" {
		t.Fatalf("cluster calls = %+v", cluster.created)
	}

	// Asking again is still idempotent for that kind.
	again, apiErr := svc.Create(context.Background(), 1, FavoriteModel, "biomistral-7b", "")
	if apiErr != nil || again.ID != deploy.ID {
		t.Fatalf("idempotent deploy = %+v / %v", again, apiErr)
	}
}

func TestWorkspaceDeployChosenVersion(t *testing.T) {
	favoriteTestDB(t)
	if _, apiErr := NewFavoriteService().Add(1, FavoriteModel, "biomistral-7b"); apiErr != nil {
		t.Fatalf("star: %v", apiErr)
	}
	versions := func(_ context.Context, ref string) (ModelArtifact, bool) {
		switch ref {
		case "biomistral-7b":
			return ModelArtifact{
				Name: "biomistral-7b-medmcqa-u1-lora", Model: "biomistral-7b",
				Architecture: "mistral", Quantization: "bf16", Storage: hostPathStorage(),
				Adapter: ".adapters/biomistral-7b-medmcqa-u1-lora",
			}, true
		case "biomistral-7b-bf16":
			return ModelArtifact{
				Name: "biomistral-7b-bf16", Model: "biomistral-7b",
				Architecture: "mistral", Quantization: "bf16", Storage: hostPathStorage(),
			}, true
		case "lavamed-7b-bf16":
			return ModelArtifact{
				Name: "lavamed-7b-bf16", Model: "lavamed-7b",
				Architecture: "llava-1.5", Quantization: "bf16", Storage: hostPathStorage(),
			}, true
		}
		return ModelArtifact{}, false
	}

	cluster := &fakeNotebooks{}
	svc := NewWorkspaceService(studioConfig(false), cluster, nil).WithDeployment(versions, runtimeProfiles)

	// Deploy the base version explicitly, even though a newer (fine-tuned) one
	// would be picked by default.
	workspace, apiErr := svc.CreateVersion(context.Background(), 1, FavoriteModel, "biomistral-7b", "", "biomistral-7b-bf16")
	if apiErr != nil {
		t.Fatalf("CreateVersion: %v", apiErr)
	}
	spec := cluster.specs[0]
	if spec["modelRef"] != "biomistral-7b-bf16" {
		t.Fatalf("modelRef = %v", spec["modelRef"])
	}
	if overrides, _ := spec["overrides"].(map[string]any); len(overrides) != 0 {
		t.Fatalf("the base version must not carry a lora override: %+v", overrides)
	}
	if workspace.Matched.ModelVersion != "biomistral-7b-bf16" {
		t.Fatalf("matched = %+v", workspace.Matched)
	}

	// Deploying the newest version (no explicit ref) picks the adapter.
	other := &fakeNotebooks{}
	svc2 := NewWorkspaceService(studioConfig(false), other, nil).WithDeployment(versions, runtimeProfiles)
	if _, apiErr := NewFavoriteService().Add(2, FavoriteModel, "biomistral-7b"); apiErr != nil {
		t.Fatalf("star: %v", apiErr)
	}
	if _, apiErr := svc2.CreateVersion(context.Background(), 2, FavoriteModel, "biomistral-7b", "", ""); apiErr != nil {
		t.Fatalf("CreateVersion: %v", apiErr)
	}
	if other.specs[0]["modelRef"] != "biomistral-7b-medmcqa-u1-lora" {
		t.Fatalf("modelRef = %v", other.specs[0]["modelRef"])
	}

	// A version of another model is refused.
	if _, apiErr := NewFavoriteService().Add(3, FavoriteModel, "biomistral-7b"); apiErr != nil {
		t.Fatalf("star: %v", apiErr)
	}
	svc3 := NewWorkspaceService(studioConfig(false), &fakeNotebooks{}, nil).WithDeployment(versions, runtimeProfiles)
	if _, apiErr := svc3.CreateVersion(context.Background(), 3, FavoriteModel, "biomistral-7b", "", "lavamed-7b-bf16"); apiErr == nil || apiErr.Code != "VERSION_MISMATCH" {
		t.Fatalf("apiErr = %+v", apiErr)
	}
}

func TestWorkspaceInferenceUsesOperatorPhase(t *testing.T) {
	favoriteTestDB(t)
	if _, apiErr := NewFavoriteService().Add(1, FavoriteModel, "biomistral-7b"); apiErr != nil {
		t.Fatalf("star: %v", apiErr)
	}
	cluster := &fakeNotebooks{}
	svc := NewWorkspaceService(studioConfig(false), cluster, nil).WithDeployment(deployableModel, runtimeProfiles)

	if _, apiErr := svc.Create(context.Background(), 1, FavoriteModel, "biomistral-7b", ""); apiErr != nil {
		t.Fatalf("Create: %v", apiErr)
	}
	cluster.found = true

	// A rolling-out service reports several False conditions that mean nothing
	// is wrong (Customized, ProfileDrifted, ProfileDeprecated). The phase is
	// what says it is progressing, so the workspace must stay "creating".
	cluster.inferencePhase = "Progressing"
	cluster.inferenceReason = "Reconciling"
	svc.Reconcile(context.Background())
	list, _ := svc.List(1)
	if list[0].Phase != model.WorkspaceCreating || list[0].Reason != "" {
		t.Fatalf("progressing workspace = %+v", list[0])
	}

	cluster.inferencePhase = "Ready"
	cluster.inferenceReason = ""
	svc.Reconcile(context.Background())
	list, _ = svc.List(1)
	if list[0].Phase != model.WorkspaceRunning {
		t.Fatalf("ready workspace = %+v", list[0])
	}

	cluster.inferencePhase = "Failed"
	cluster.inferenceReason = "ModelStorageIncompatible"
	svc.Reconcile(context.Background())
	list, _ = svc.List(1)
	if list[0].Phase != model.WorkspaceFailed || list[0].Reason != "ModelStorageIncompatible" {
		t.Fatalf("failed workspace = %+v", list[0])
	}
}
