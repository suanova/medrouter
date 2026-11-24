package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/suanova/medrouter/internal/common"
	"github.com/suanova/medrouter/internal/config"
	"github.com/suanova/medrouter/internal/kube"
	"github.com/suanova/medrouter/internal/model"
)

// Workspace kinds and the phases the studio exposes. "planned" only appears in
// a dry-run response and is never persisted.
const (
	WorkspaceNotebook  = "notebook"
	WorkspaceInference = "inference"
	WorkspaceFinetune  = "finetune"
	WorkspacePlanned   = "planned"
)

// WorkspaceCluster is the slice of the Kubernetes API the studio writes.
type WorkspaceCluster interface {
	CreateDevEnvironment(ctx context.Context, namespace, name string, labels map[string]string, spec map[string]any) (kube.DevEnvironment, error)
	GetDevEnvironment(ctx context.Context, namespace, name string) (kube.DevEnvironment, bool, error)
	SetDevEnvironmentRunning(ctx context.Context, namespace, name string, running bool) error
	DeleteDevEnvironment(ctx context.Context, namespace, name string) error

	CreateJob(ctx context.Context, namespace, name string, labels map[string]string, spec map[string]any) (kube.Job, error)
	GetJob(ctx context.Context, namespace, name string) (kube.Job, bool, error)
	DeleteJob(ctx context.Context, namespace, name string) error
}

// DatasetSizeFunc reports a dataset's declared size, for example "287Mi". The
// catalog is injected as a function so the studio does not depend on it.
type DatasetSizeFunc func(ctx context.Context, name string) (string, bool)

// ModelStorageFunc reports where a model's weights live. Fine-tuning can only
// mount HostPath weights today.
type ModelStorageFunc func(ctx context.Context, model string) (kube.ModelStorage, bool)

// MatchedResources is what the backend chose on the user's behalf. It is shown
// on the card so the automatic decision stays visible.
type MatchedResources struct {
	Image       string `json:"image,omitempty"`
	CPU         string `json:"cpu,omitempty"`
	Memory      string `json:"memory,omitempty"`
	GPU         int    `json:"gpu"`
	Storage     string `json:"storage,omitempty"`
	Volume      string `json:"volume,omitempty"`
	DatasetSize string `json:"dataset_size,omitempty"`
	IdleTimeout int    `json:"idle_timeout_seconds,omitempty"`

	// Fine-tune plan fields.
	BaseModel  string `json:"base_model,omitempty"`
	Dataset    string `json:"dataset,omitempty"`
	Output     string `json:"output,omitempty"`
	Deadline   int    `json:"deadline_seconds,omitempty"`
	MaxSamples int    `json:"max_samples,omitempty"`
}

// WorkspaceDTO is the API shape of one workspace.
type WorkspaceDTO struct {
	ID         int64            `json:"id"`
	Kind       string           `json:"kind"`
	SourceKind string           `json:"source_kind"`
	SourceName string           `json:"source_name"`
	Name       string           `json:"name"`
	Namespace  string           `json:"namespace"`
	Phase      string           `json:"phase"`
	Reason     string           `json:"reason,omitempty"`
	Endpoint   string           `json:"endpoint,omitempty"`
	Matched    MatchedResources `json:"matched"`
	DryRun     bool             `json:"dry_run,omitempty"`
	CreatedAt  string           `json:"created_at,omitempty"`
}

// WorkspaceService turns a favorite into a running environment. It owns the
// resource matching: the caller supplies only which favorite to start.
type WorkspaceService struct {
	cfg       *config.Config
	cluster   WorkspaceCluster
	datasetSz DatasetSizeFunc
	modelStor ModelStorageFunc
	finetune  *FineTuneService
	now       func() time.Time
}

func NewWorkspaceService(cfg *config.Config, cluster WorkspaceCluster, datasetSize DatasetSizeFunc) *WorkspaceService {
	return &WorkspaceService{cfg: cfg, cluster: cluster, datasetSz: datasetSize, now: time.Now}
}

// WithFineTune wires the parts the fine-tune path needs: model storage lookup
// and the compatibility rules that the picker already uses.
func (s *WorkspaceService) WithFineTune(models ModelStorageFunc, finetune *FineTuneService) *WorkspaceService {
	s.modelStor = models
	s.finetune = finetune
	return s
}

var dnsNamePattern = regexp.MustCompile(`[^a-z0-9-]+`)

// Create launches the environment for one star. It is idempotent per favorite:
// an existing workspace is returned instead of creating a second one.
func (s *WorkspaceService) Create(ctx context.Context, ownerID int64, sourceKind, sourceName, datasetName string) (*WorkspaceDTO, *common.APIError) {
	if !s.cfg.StudioEnabled {
		return nil, common.NewAPIErrorStatus(http.StatusServiceUnavailable, "STUDIO_DISABLED",
			"The studio is not enabled for this deployment")
	}
	if err := validateFavorite(sourceKind, sourceName); err != nil {
		return nil, err
	}
	sourceName = strings.TrimSpace(sourceName)

	// The contract is "favorite first": an entry must be starred before it can
	// be launched, which also bounds what a user can start.
	var favorite model.Favorite
	err := model.DB.Where("user_id = ? AND kind = ? AND name = ?", ownerID, sourceKind, sourceName).
		First(&favorite).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, common.NewAPIErrorStatus(http.StatusConflict, "STAR_REQUIRED",
			"Star this entry before launching it")
	}
	if err != nil {
		return nil, common.NewAPIErrorStatus(http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to read the favorite")
	}

	var existing model.Workspace
	err = model.DB.Where("owner_id = ? AND source_kind = ? AND source_name = ?", ownerID, sourceKind, sourceName).
		First(&existing).Error
	if err == nil {
		dto := toWorkspaceDTO(existing)
		return &dto, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, common.NewAPIErrorStatus(http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to read workspaces")
	}

	if sourceKind == FavoriteModel {
		return s.createFinetune(ctx, ownerID, sourceName, datasetName)
	}

	if apiErr := s.checkQuota(ownerID); apiErr != nil {
		return nil, apiErr
	}

	name := workspaceName(sourceName, ownerID)
	matched, spec := s.notebookPlan(ctx, sourceName)

	if s.cfg.StudioDryRun {
		return &WorkspaceDTO{
			Kind:       WorkspaceNotebook,
			SourceKind: sourceKind,
			SourceName: sourceName,
			Name:       name,
			Namespace:  s.cfg.StudioNamespace,
			Phase:      WorkspacePlanned,
			Matched:    matched,
			DryRun:     true,
		}, nil
	}
	if s.cluster == nil {
		return nil, common.NewAPIErrorStatus(http.StatusServiceUnavailable, "STUDIO_UNAVAILABLE",
			"The cluster is not reachable from this deployment")
	}

	labels := map[string]string{
		"app.kubernetes.io/managed-by": "medrouter",
		"medrouter.io/owner":           strconv.FormatInt(ownerID, 10),
		"medrouter.io/source-kind":     sourceKind,
		"medrouter.io/source-name":     sourceName,
	}
	if _, err := s.cluster.CreateDevEnvironment(ctx, s.cfg.StudioNamespace, name, labels, spec); err != nil {
		return nil, workspaceAPIError(err)
	}

	matchedJSON, _ := json.Marshal(matched)
	specJSON, _ := json.Marshal(spec)
	row := model.Workspace{
		OwnerID:    ownerID,
		SourceKind: sourceKind,
		SourceName: sourceName,
		Kind:       WorkspaceNotebook,
		Name:       name,
		Namespace:  s.cfg.StudioNamespace,
		Phase:      model.WorkspaceCreating,
		Matched:    string(matchedJSON),
		Spec:       string(specJSON),
	}
	if err := model.DB.Create(&row).Error; err != nil {
		// The environment exists but is untracked. Delete it so a retry starts
		// clean rather than leaving an orphan nobody can manage.
		_ = s.cluster.DeleteDevEnvironment(context.WithoutCancel(ctx), s.cfg.StudioNamespace, name)
		return nil, common.NewAPIErrorStatus(http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to record the workspace")
	}
	dto := toWorkspaceDTO(row)
	return &dto, nil
}

// List returns the caller's workspaces, newest first.
func (s *WorkspaceService) List(ownerID int64) ([]WorkspaceDTO, *common.APIError) {
	var rows []model.Workspace
	if err := model.DB.Where("owner_id = ?", ownerID).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, common.NewAPIErrorStatus(http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to load workspaces")
	}
	out := make([]WorkspaceDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, toWorkspaceDTO(row))
	}
	return out, nil
}

// SetRunning starts or stops one workspace.
func (s *WorkspaceService) SetRunning(ctx context.Context, ownerID, id int64, running bool) (*WorkspaceDTO, *common.APIError) {
	row, apiErr := s.owned(ownerID, id)
	if apiErr != nil {
		return nil, apiErr
	}
	if row.Kind == WorkspaceFinetune {
		return nil, common.NewAPIErrorStatus(http.StatusConflict, "NOT_SUPPORTED",
			"A training job cannot be paused; delete it to stop it")
	}
	if s.cfg.StudioDryRun || s.cluster == nil {
		return nil, common.NewAPIErrorStatus(http.StatusServiceUnavailable, "STUDIO_DRY_RUN",
			"The studio is running in dry-run mode; nothing was changed")
	}
	if err := s.cluster.SetDevEnvironmentRunning(ctx, row.Namespace, row.Name, running); err != nil {
		return nil, workspaceAPIError(err)
	}
	phase := model.WorkspaceStopped
	if running {
		phase = model.WorkspaceCreating
	}
	model.DB.Model(&model.Workspace{}).Where("id = ?", row.ID).
		Updates(map[string]any{"phase": phase, "reason": ""})
	row.Phase, row.Reason = phase, ""
	dto := toWorkspaceDTO(*row)
	return &dto, nil
}

// Delete removes the environment and the record.
func (s *WorkspaceService) Delete(ctx context.Context, ownerID, id int64) *common.APIError {
	row, apiErr := s.owned(ownerID, id)
	if apiErr != nil {
		return apiErr
	}
	if !s.cfg.StudioDryRun && s.cluster != nil {
		var err error
		if row.Kind == WorkspaceFinetune {
			err = s.cluster.DeleteJob(ctx, row.Namespace, row.Name)
		} else {
			err = s.cluster.DeleteDevEnvironment(ctx, row.Namespace, row.Name)
		}
		if err != nil {
			var status *kube.StatusError
			if !errors.As(err, &status) || status.Status != http.StatusNotFound {
				return workspaceAPIError(err)
			}
		}
	}
	if err := model.DB.Delete(&model.Workspace{}, row.ID).Error; err != nil {
		return common.NewAPIErrorStatus(http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to delete the workspace")
	}
	return nil
}

// Reconcile projects the underlying resources onto the stored rows. It is a
// poll loop rather than a watch so the process keeps its single HTTP client
// and does not depend on client-go.
func (s *WorkspaceService) Reconcile(ctx context.Context) {
	if s.cluster == nil {
		return
	}
	var rows []model.Workspace
	err := model.DB.Where("phase IN ?", []string{model.WorkspaceCreating, model.WorkspaceRunning, model.WorkspaceStopped}).
		Find(&rows).Error
	if err != nil {
		return
	}
	for _, row := range rows {
		if row.Kind == WorkspaceFinetune {
			job, found, err := s.cluster.GetJob(ctx, row.Namespace, row.Name)
			if err != nil || !found {
				continue
			}
			phase, reason := deriveJobPhase(job.Status)
			if phase == row.Phase && reason == row.Reason {
				continue
			}
			model.DB.Model(&model.Workspace{}).Where("id = ?", row.ID).
				Updates(map[string]any{"phase": phase, "reason": reason})
			continue
		}
		env, found, err := s.cluster.GetDevEnvironment(ctx, row.Namespace, row.Name)
		if err != nil || !found {
			continue
		}
		state := kube.NotebookStateOf(env)
		phase, reason := derivePhase(state)
		if phase == row.Phase && reason == row.Reason && state.Endpoint == row.Endpoint {
			continue
		}
		model.DB.Model(&model.Workspace{}).Where("id = ?", row.ID).
			Updates(map[string]any{"phase": phase, "reason": reason, "endpoint": state.Endpoint})
	}
}

// RunReconciler polls until ctx is done.
func (s *WorkspaceService) RunReconciler(ctx context.Context) {
	interval := s.cfg.StudioReconcileInterval
	if interval <= 0 {
		interval = 3 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Reconcile(ctx)
		}
	}
}

func (s *WorkspaceService) owned(ownerID, id int64) (*model.Workspace, *common.APIError) {
	var row model.Workspace
	err := model.DB.Where("id = ? AND owner_id = ?", id, ownerID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, common.NewAPIErrorStatus(http.StatusNotFound, "WORKSPACE_NOT_FOUND", "Workspace not found")
	}
	if err != nil {
		return nil, common.NewAPIErrorStatus(http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to read the workspace")
	}
	return &row, nil
}

// notebookPlan is the automatic resource choice: size tiers by dataset size,
// a fixed image, and the dataset PVC mounted read-only.
func (s *WorkspaceService) notebookPlan(ctx context.Context, dataset string) (MatchedResources, map[string]any) {
	sizeLabel := ""
	if s.datasetSz != nil {
		sizeLabel, _ = s.datasetSz(ctx, dataset)
	}
	cpu, memory, storage := notebookTier(sizeLabel)
	volume := fmt.Sprintf(s.cfg.StudioDatasetPVCPattern, dataset)

	matched := MatchedResources{
		Image:       s.cfg.StudioNotebookImage,
		CPU:         cpu,
		Memory:      memory,
		Storage:     storage,
		Volume:      volume,
		DatasetSize: sizeLabel,
		IdleTimeout: 7200,
	}
	spec := map[string]any{
		"type":    "jupyter",
		"image":   matched.Image,
		"running": true,
		"resources": map[string]any{
			"cpu":    cpu,
			"memory": memory,
		},
		"storage": map[string]any{
			"size":         storage,
			"mountPath":    "/home/ubuntu",
			"pvcRetention": "retain",
		},
		"ports": []any{
			map[string]any{"name": "jupyter", "containerPort": 8888, "type": "http"},
		},
		"lifecycle": map[string]any{"idleTimeout": matched.IdleTimeout},
		"volumes": []any{
			map[string]any{
				"name":      "dataset",
				"mountPath": "/data/" + dataset,
				"pvcName":   volume,
				"readOnly":  true,
			},
		},
	}
	return matched, spec
}

// notebookTier maps a declared dataset size onto a size tier. Unknown sizes
// take the middle tier rather than failing the launch.
func notebookTier(sizeLabel string) (cpu, memory, storage string) {
	const gi = int64(1) << 30
	bytes, ok := parseByteSize(sizeLabel)
	switch {
	case !ok || bytes < 10*gi:
		return "8", "32Gi", "50Gi"
	case bytes < 100*gi:
		return "16", "64Gi", "100Gi"
	default:
		return "32", "128Gi", "200Gi"
	}
}

var sizePattern = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)\s*([KMGTPE]i?|B)?$`)

func parseByteSize(raw string) (int64, bool) {
	match := sizePattern.FindStringSubmatch(strings.TrimSpace(raw))
	if match == nil {
		return 0, false
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0, false
	}
	multiplier := float64(1)
	switch strings.ToUpper(match[2]) {
	case "", "B":
	case "K":
		multiplier = 1e3
	case "M":
		multiplier = 1e6
	case "G":
		multiplier = 1e9
	case "T":
		multiplier = 1e12
	case "KI":
		multiplier = 1 << 10
	case "MI":
		multiplier = 1 << 20
	case "GI":
		multiplier = 1 << 30
	case "TI":
		multiplier = 1 << 40
	default:
		return 0, false
	}
	return int64(value * multiplier), true
}

// workspaceName derives a DNS-1123 name that is unique per user, because all
// workspaces share one namespace.
func workspaceName(source string, ownerID int64) string {
	slug := dnsNamePattern.ReplaceAllString(strings.ToLower(source), "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "workspace"
	}
	suffix := fmt.Sprintf("-u%d-lab", ownerID)
	if len(slug)+len(suffix) > 63 {
		slug = slug[:63-len(suffix)]
		slug = strings.TrimRight(slug, "-")
	}
	return slug + suffix
}

func derivePhase(state kube.NotebookState) (phase, reason string) {
	switch strings.ToLower(state.Phase) {
	case "failed", "error":
		return model.WorkspaceFailed, state.Reason
	case "stopped":
		return model.WorkspaceStopped, ""
	case "running":
		return model.WorkspaceRunning, ""
	default:
		return model.WorkspaceCreating, state.Reason
	}
}

func workspaceAPIError(err error) *common.APIError {
	var status *kube.StatusError
	if errors.As(err, &status) {
		switch status.Reason {
		case "ALREADY_EXISTS":
			return common.NewAPIErrorStatus(http.StatusConflict, "ALREADY_EXISTS", "The environment already exists")
		case "INVALID":
			return common.NewAPIErrorStatus(http.StatusBadRequest, "INVALID_SPEC", status.Detail())
		case "FORBIDDEN", "UNAUTHORIZED":
			return common.NewAPIErrorStatus(http.StatusServiceUnavailable, "STUDIO_FORBIDDEN",
				"MedRouter is not allowed to manage workspaces in the cluster")
		case "CRD_MISSING":
			return common.NewAPIErrorStatus(http.StatusServiceUnavailable, "STUDIO_UNAVAILABLE",
				"DevEnvironment is not installed in this cluster")
		case "TIMEOUT":
			return common.NewAPIErrorStatus(http.StatusGatewayTimeout, "TIMEOUT", "The cluster did not respond in time")
		default:
			return common.NewAPIErrorStatus(http.StatusServiceUnavailable, "STUDIO_UNAVAILABLE",
				"The environment could not be created")
		}
	}
	return common.NewAPIErrorStatus(http.StatusServiceUnavailable, "STUDIO_UNAVAILABLE",
		"The environment could not be created")
}

func toWorkspaceDTO(row model.Workspace) WorkspaceDTO {
	dto := WorkspaceDTO{
		ID:         row.ID,
		Kind:       row.Kind,
		SourceKind: row.SourceKind,
		SourceName: row.SourceName,
		Name:       row.Name,
		Namespace:  row.Namespace,
		Phase:      row.Phase,
		Reason:     row.Reason,
		Endpoint:   row.Endpoint,
	}
	if row.Matched != "" {
		_ = json.Unmarshal([]byte(row.Matched), &dto.Matched)
	}
	if !row.CreatedAt.IsZero() {
		dto.CreatedAt = row.CreatedAt.UTC().Format(time.RFC3339)
	}
	return dto
}

// createFinetune builds a training Job for one model × dataset pair. The pair
// is re-validated here even though the picker already filtered: the UI is a
// convenience, not the boundary.
func (s *WorkspaceService) createFinetune(ctx context.Context, ownerID int64, modelName, datasetName string) (*WorkspaceDTO, *common.APIError) {
	datasetName = strings.TrimSpace(datasetName)
	if datasetName == "" {
		return nil, common.NewAPIErrorStatus(http.StatusBadRequest, "DATASET_REQUIRED",
			"Choose a dataset to fine-tune with")
	}
	if s.finetune == nil || s.modelStor == nil {
		return nil, common.NewAPIErrorStatus(http.StatusServiceUnavailable, "STUDIO_UNAVAILABLE",
			"Fine-tuning is not configured for this deployment")
	}

	targets := s.finetune.Targets(ctx, modelName)
	if !targets.Available {
		return nil, common.NewAPIErrorStatus(http.StatusConflict, "FINE_TUNE_UNAVAILABLE",
			"This model cannot be fine-tuned: "+targets.Reason)
	}
	found := false
	for _, candidate := range targets.Datasets {
		if candidate.Name == datasetName {
			found = true
			break
		}
	}
	if !found {
		return nil, common.NewAPIErrorStatus(http.StatusConflict, "DATASET_NOT_COMPATIBLE",
			"This dataset does not match the model's fine-tuning tasks")
	}

	storage, ok := s.modelStor(ctx, modelName)
	if !ok || storage.Strategy != "HostPath" || storage.HostPath == nil || storage.HostPath.Path == "" {
		return nil, common.NewAPIErrorStatus(http.StatusConflict, "MODEL_STORAGE_UNSUPPORTED",
			"Fine-tuning currently needs the base weights on a host path")
	}

	if apiErr := s.checkQuota(ownerID); apiErr != nil {
		return nil, apiErr
	}

	name := finetuneName(modelName, datasetName, ownerID)
	matched, spec := s.finetunePlan(name, modelName, datasetName, storage.HostPath.Path)
	if s.cfg.StudioDryRun {
		return &WorkspaceDTO{
			Kind:       WorkspaceFinetune,
			SourceKind: FavoriteModel,
			SourceName: modelName,
			Name:       name,
			Namespace:  s.cfg.StudioNamespace,
			Phase:      WorkspacePlanned,
			Matched:    matched,
			DryRun:     true,
		}, nil
	}
	if s.cluster == nil {
		return nil, common.NewAPIErrorStatus(http.StatusServiceUnavailable, "STUDIO_UNAVAILABLE",
			"The cluster is not reachable from this deployment")
	}

	labels := map[string]string{
		"app.kubernetes.io/managed-by": "medrouter",
		"medrouter.io/owner":           strconv.FormatInt(ownerID, 10),
		"medrouter.io/kind":            WorkspaceFinetune,
		"medrouter.io/source-name":     modelName,
		"medrouter.io/dataset":         datasetName,
	}
	if _, err := s.cluster.CreateJob(ctx, s.cfg.StudioNamespace, name, labels, spec); err != nil {
		return nil, workspaceAPIError(err)
	}

	matchedJSON, _ := json.Marshal(matched)
	specJSON, _ := json.Marshal(spec)
	row := model.Workspace{
		OwnerID:    ownerID,
		SourceKind: FavoriteModel,
		SourceName: modelName,
		Kind:       WorkspaceFinetune,
		Name:       name,
		Namespace:  s.cfg.StudioNamespace,
		Phase:      model.WorkspaceCreating,
		Matched:    string(matchedJSON),
		Spec:       string(specJSON),
	}
	if err := model.DB.Create(&row).Error; err != nil {
		_ = s.cluster.DeleteJob(context.WithoutCancel(ctx), s.cfg.StudioNamespace, name)
		return nil, common.NewAPIErrorStatus(http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to record the workspace")
	}
	dto := toWorkspaceDTO(row)
	return &dto, nil
}

func (s *WorkspaceService) checkQuota(ownerID int64) *common.APIError {
	var active int64
	if err := model.DB.Model(&model.Workspace{}).Where("owner_id = ?", ownerID).Count(&active).Error; err != nil {
		return common.NewAPIErrorStatus(http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to count workspaces")
	}
	if s.cfg.StudioMaxWorkspaces > 0 && active >= int64(s.cfg.StudioMaxWorkspaces) {
		return common.NewAPIErrorStatus(http.StatusConflict, "QUOTA_EXCEEDED",
			"You have reached the workspace limit; stop or delete one first")
	}
	return nil
}

// finetunePlan is the fixed recipe: one accelerator, the dataset PVC read-only,
// the base weights read-only, and the adapter under the output path.
func (s *WorkspaceService) finetunePlan(name, model, dataset, basePath string) (MatchedResources, map[string]any) {
	datasetPVC := fmt.Sprintf(s.cfg.StudioDatasetPVCPattern, dataset)
	output := s.cfg.StudioFinetuneOutput + "/" + name
	deadline := int(s.cfg.StudioFinetuneDeadline.Seconds())
	if deadline <= 0 {
		deadline = 43200
	}

	matched := MatchedResources{
		Image:      s.cfg.StudioFinetuneImage,
		CPU:        "8",
		Memory:     "20Gi",
		GPU:        1,
		BaseModel:  basePath,
		Dataset:    dataset,
		Volume:     datasetPVC,
		Output:     output,
		Deadline:   deadline,
		MaxSamples: s.cfg.StudioFinetuneMaxSamples,
	}

	env := []any{
		map[string]any{"name": "HF_HUB_OFFLINE", "value": "1"},
		map[string]any{"name": "TRANSFORMERS_OFFLINE", "value": "1"},
		map[string]any{"name": "PYTHONUNBUFFERED", "value": "1"},
		// Same MACA tuning the serving profiles use; cheap and no-ops if the
		// runtime ignores them.
		map[string]any{"name": "MACA_SMALL_PAGESIZE_ENABLE", "value": "1"},
		map[string]any{"name": "PYTORCH_ENABLE_PG_HIGH_PRIORITY_STREAM", "value": "1"},
	}
	container := map[string]any{
		"name":  "train",
		"image": s.cfg.StudioFinetuneImage,
		// hostPath volumes are created by kubelet as root, and the base image
		// runs as uid 1000; the job writes to /out, so it has to be root.
		"securityContext": map[string]any{"runAsUser": 0, "runAsGroup": 0},
		"command":         []string{"python3", "/opt/medrouter/train_lora.py"},
		"args":            s.finetuneArgs(name, dataset),
		"env":             env,
		"resources": map[string]any{
			// The GPU node has ~30Gi allocatable; a 14GB bf16 model loads with
			// low_cpu_mem_usage inside 20Gi. A 48Gi request is unschedulable.
			"requests": map[string]any{"cpu": "8", "memory": "20Gi", "metax-tech.com/gpu": "1"},
			"limits":   map[string]any{"cpu": "8", "memory": "28Gi", "metax-tech.com/gpu": "1"},
		},
		"volumeMounts": []any{
			map[string]any{"name": "base", "mountPath": "/models/base", "readOnly": true},
			map[string]any{"name": "dataset", "mountPath": "/data/" + dataset, "readOnly": true},
			map[string]any{"name": "output", "mountPath": "/out"},
		},
	}
	spec := map[string]any{
		"backoffLimit":            0,
		"activeDeadlineSeconds":   int64(deadline),
		"ttlSecondsAfterFinished": 604800,
		"template": map[string]any{
			"metadata": map[string]any{"labels": map[string]string{"medrouter.io/workspace": name}},
			"spec": map[string]any{
				"restartPolicy": "Never",
				"containers":    []any{container},
				"volumes": []any{
					map[string]any{"name": "base", "hostPath": map[string]any{"path": basePath, "type": "Directory"}},
					map[string]any{"name": "dataset", "persistentVolumeClaim": map[string]any{"claimName": datasetPVC}},
					map[string]any{"name": "output", "hostPath": map[string]any{"path": s.cfg.StudioFinetuneOutput, "type": "DirectoryOrCreate"}},
				},
			},
		},
	}
	return matched, spec
}

// finetuneArgs builds the training command. The row cap is passed explicitly so
// the deadline stays reachable on a single accelerator.
func (s *WorkspaceService) finetuneArgs(name, dataset string) []string {
	args := []string{
		"--base-model", "/models/base",
		"--dataset-dir", "/data/" + dataset,
		// One directory per workspace: sharing /out would make two runs
		// overwrite each other's adapter.
		"--output-dir", "/out/" + name,
	}
	if s.cfg.StudioFinetuneMaxSamples > 0 {
		args = append(args, "--max-samples", strconv.Itoa(s.cfg.StudioFinetuneMaxSamples))
	}
	return args
}

// finetuneName is unique per user, like notebook names, because workspaces
// share one namespace.
func finetuneName(modelName, datasetName string, ownerID int64) string {
	slug := dnsNamePattern.ReplaceAllString(strings.ToLower(modelName+"-"+datasetName), "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "finetune"
	}
	suffix := fmt.Sprintf("-u%d-lora", ownerID)
	if len(slug)+len(suffix) > 63 {
		slug = strings.TrimRight(slug[:63-len(suffix)], "-")
	}
	return slug + suffix
}

// deriveJobPhase maps a batch Job onto the studio's phases. A Job is terminal:
// a finished run is "succeeded", not "stopped".
func deriveJobPhase(status kube.JobStatus) (phase, reason string) {
	for _, condition := range status.Conditions {
		if condition.Status != "True" {
			continue
		}
		switch condition.Type {
		case "Complete":
			return model.WorkspaceSucceeded, ""
		case "Failed":
			if condition.Reason != "" {
				return model.WorkspaceFailed, condition.Reason
			}
			return model.WorkspaceFailed, status.Reason
		}
	}
	// A batch Job has no status.phase; only conditions and counters. Reading
	// Phase here was why a running fine-tune stayed "creating".
	if status.Succeeded > 0 {
		return model.WorkspaceSucceeded, ""
	}
	if status.Failed > 0 {
		return model.WorkspaceFailed, status.Reason
	}
	if status.Active > 0 {
		return model.WorkspaceRunning, ""
	}
	return model.WorkspaceCreating, status.Reason
}
