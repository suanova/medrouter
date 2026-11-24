package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/suanova/medrouter/internal/common"
	"github.com/suanova/medrouter/internal/kube"
	"github.com/suanova/medrouter/internal/model"
)

// ModelInfoFunc resolves a model or ModelVersion reference to its identity.
type ModelInfoFunc func(ctx context.Context, ref string) (ModelArtifact, bool)

// ProfilesFunc returns the runtime profiles available in the deployment.
type ProfilesFunc func(ctx context.Context) []kube.RuntimeProfile

// WithDeployment wires the parts one-click deployment and post-training
// registration need.
func (s *WorkspaceService) WithDeployment(modelInfo ModelInfoFunc, profiles ProfilesFunc) *WorkspaceService {
	s.modelInfo = modelInfo
	s.profiles = profiles
	return s
}

// matchProfile picks the runtime for an architecture and quantization. SGLang
// wins ties because it is the engine the platform has validated.
func matchProfile(profiles []kube.RuntimeProfile, architecture, quantization string) (kube.RuntimeProfile, bool) {
	var candidates []kube.RuntimeProfile
	for _, profile := range profiles {
		if !containsString(profile.Spec.ModelRequirements.Architectures, architecture) {
			continue
		}
		if !containsString(profile.Spec.ModelRequirements.Quantization, quantization) {
			continue
		}
		candidates = append(candidates, profile)
	}
	if len(candidates) == 0 {
		return kube.RuntimeProfile{}, false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return engineRank(candidates[i].Spec.Engine.Name) < engineRank(candidates[j].Spec.Engine.Name)
	})
	return candidates[0], true
}

func engineRank(engine string) int {
	switch strings.ToLower(engine) {
	case "sglang":
		return 0
	case "vllm":
		return 1
	default:
		return 2
	}
}

func inferenceName(ref string, ownerID int64) string {
	slug := dnsNamePattern.ReplaceAllString(strings.ToLower(ref), "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "inference"
	}
	suffix := fmt.Sprintf("-u%d-svc", ownerID)
	if len(slug)+len(suffix) > 63 {
		slug = strings.TrimRight(slug[:63-len(suffix)], "-")
	}
	return slug + suffix
}

// createInference deploys one model version behind the AI gateway. There is no
// fine-tune dataset here: the entry point is "deploy this model".
func (s *WorkspaceService) createInference(ctx context.Context, ownerID int64, ref string) (*WorkspaceDTO, *common.APIError) {
	if s.modelInfo == nil || s.profiles == nil {
		return nil, common.NewAPIErrorStatus(http.StatusServiceUnavailable, "STUDIO_UNAVAILABLE",
			"Deployment is not configured for this deployment")
	}
	artifact, ok := s.modelInfo(ctx, ref)
	if !ok {
		return nil, common.NewAPIErrorStatus(http.StatusNotFound, "MODEL_NOT_FOUND", "Model not found")
	}
	profile, ok := matchProfile(s.profiles(ctx), artifact.Architecture, artifact.Quantization)
	if !ok {
		return nil, common.NewAPIErrorStatus(http.StatusConflict, "NO_PROFILE",
			"No runtime profile matches this model's architecture and quantization")
	}
	if apiErr := s.checkQuota(ownerID); apiErr != nil {
		return nil, apiErr
	}

	name := inferenceName(artifact.Model, ownerID)
	matched := MatchedResources{
		Image:        profile.Spec.Engine.Name + " " + profile.Spec.Engine.Version,
		Profile:      profile.Metadata.Name,
		ModelVersion: artifact.Name,
		Router:       s.cfg.StudioGatewayRouter,
	}
	spec := map[string]any{
		"modelRef":   artifact.Name,
		"profileRef": profile.Metadata.Name,
		"overrides":  map[string]any{},
		"route": map[string]any{
			"aigatewayRouter": s.cfg.StudioGatewayRouter,
			"modelName":       name,
		},
	}
	if s.cfg.StudioDryRun {
		return &WorkspaceDTO{
			Kind:       WorkspaceInference,
			SourceKind: FavoriteModel,
			SourceName: artifact.Model,
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
		"medrouter.io/kind":            WorkspaceInference,
		"medrouter.io/source-name":     artifact.Model,
	}
	if _, err := s.cluster.CreateInferenceService(ctx, s.cfg.StudioNamespace, name, labels, spec); err != nil {
		return nil, workspaceAPIError(err)
	}

	matchedJSON, _ := json.Marshal(matched)
	specJSON, _ := json.Marshal(spec)
	row := model.Workspace{
		OwnerID:    ownerID,
		Kind:       WorkspaceInference,
		SourceKind: FavoriteModel,
		SourceName: artifact.Model,
		Name:       name,
		Namespace:  s.cfg.StudioNamespace,
		Phase:      model.WorkspaceCreating,
		Matched:    string(matchedJSON),
		Spec:       string(specJSON),
	}
	if err := model.DB.Create(&row).Error; err != nil {
		_ = s.cluster.DeleteInferenceService(context.WithoutCancel(ctx), s.cfg.StudioNamespace, name)
		return nil, common.NewAPIErrorStatus(http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to record the workspace")
	}
	dto := toWorkspaceDTO(row)
	return &dto, nil
}

// inferenceState projects an InferenceService onto the studio phases.
func (s *WorkspaceService) inferenceState(ctx context.Context, row model.Workspace) (phase, reason, endpoint string, ok bool) {
	service, found, err := s.cluster.GetInferenceService(ctx, row.Namespace, row.Name)
	if err != nil || !found {
		return "", "", "", false
	}
	endpoint = kube.InferenceServiceEndpoint(service)
	if kube.InferenceServiceReady(service) {
		return model.WorkspaceRunning, "", endpoint, true
	}
	if reason := kube.InferenceServiceReason(service); reason != "" {
		return model.WorkspaceFailed, reason, endpoint, true
	}
	return model.WorkspaceCreating, "", endpoint, true
}

// advanceFineTune drives the post-training steps: merge the adapter into a full
// model, register it as a ModelVersion, then mark the workspace ready. It is
// idempotent, so the reconciler can call it on every tick.
func (s *WorkspaceService) advanceFineTune(ctx context.Context, row model.Workspace) (phase, reason, modelVersion string) {
	if row.ModelVersion != "" {
		return model.WorkspaceReady, "", row.ModelVersion
	}
	if s.cfg.StudioDryRun || s.cluster == nil || s.modelInfo == nil {
		return model.WorkspaceSucceeded, "", ""
	}
	artifact, ok := s.modelInfo(ctx, row.SourceName)
	if !ok {
		return model.WorkspaceFailed, "BASE_MODEL_MISSING", ""
	}

	mergeName := row.Name + "-merge"
	mergeJob, found, err := s.cluster.GetJob(ctx, row.Namespace, mergeName)
	if err != nil {
		return model.WorkspaceMerging, "", ""
	}
	if !found {
		spec, apiErr := s.mergeJobSpec(row, artifact)
		if apiErr != nil {
			return model.WorkspaceFailed, apiErr.Code, ""
		}
		labels := map[string]string{
			"app.kubernetes.io/managed-by": "medrouter",
			"medrouter.io/kind":            "merge",
			"medrouter.io/workspace":       row.Name,
		}
		if _, err := s.cluster.CreateJob(ctx, row.Namespace, mergeName, labels, spec); err != nil {
			return model.WorkspaceMerging, "MERGE_CREATE_FAILED", ""
		}
		return model.WorkspaceMerging, "", ""
	}

	mergePhase, mergeReason := deriveJobPhase(mergeJob.Status)
	switch mergePhase {
	case model.WorkspaceFailed:
		return model.WorkspaceFailed, mergeReason, ""
	case model.WorkspaceSucceeded:
	default:
		return model.WorkspaceMerging, "", ""
	}

	if apiErr := s.registerModelVersion(ctx, row, artifact); apiErr != nil {
		return model.WorkspaceFailed, apiErr.Code, ""
	}
	return model.WorkspaceReady, "", row.Name
}

// mergeJobSpec folds the adapter into the base weights so an existing serving
// profile can load the result. The merge runs on CPU: it needs no accelerator.
func (s *WorkspaceService) mergeJobSpec(row model.Workspace, artifact ModelArtifact) (map[string]any, *common.APIError) {
	if artifact.Storage.Strategy != "HostPath" || artifact.Storage.HostPath == nil || artifact.Storage.HostPath.Path == "" {
		return nil, common.NewAPIErrorStatus(http.StatusConflict, "MODEL_STORAGE_UNSUPPORTED",
			"The base weights are not on a host path")
	}
	container := map[string]any{
		"name":            "merge",
		"image":           s.cfg.StudioFinetuneImage,
		"securityContext": map[string]any{"runAsUser": 0, "runAsGroup": 0},
		"command":         []string{"python3", "/scripts/merge_lora.py"},
		"args": []string{
			"--base-model", "/models/base",
			"--adapter", "/lora/" + row.Name,
			"--output-dir", "/merged/" + row.Name,
		},
		"resources": map[string]any{
			"requests": map[string]any{"cpu": "6", "memory": "16Gi"},
			"limits":   map[string]any{"cpu": "6", "memory": "26Gi"},
		},
		"volumeMounts": []any{
			map[string]any{"name": "scripts", "mountPath": "/scripts", "readOnly": true},
			map[string]any{"name": "base", "mountPath": "/models/base", "readOnly": true},
			map[string]any{"name": "lora", "mountPath": "/lora", "readOnly": true},
			map[string]any{"name": "merged", "mountPath": "/merged"},
		},
	}
	spec := map[string]any{
		"backoffLimit":            1,
		"activeDeadlineSeconds":   int64(3600),
		"ttlSecondsAfterFinished": 604800,
		"template": map[string]any{
			"metadata": map[string]any{"labels": map[string]string{"medrouter.io/workspace": row.Name}},
			"spec": map[string]any{
				"restartPolicy": "Never",
				"containers":    []any{container},
				"volumes": []any{
					map[string]any{"name": "scripts", "configMap": map[string]any{"name": s.cfg.StudioMergeScript}},
					map[string]any{"name": "base", "hostPath": map[string]any{"path": artifact.Storage.HostPath.Path, "type": "Directory"}},
					map[string]any{"name": "lora", "hostPath": map[string]any{"path": s.cfg.StudioFinetuneOutput, "type": "Directory"}},
					map[string]any{"name": "merged", "hostPath": map[string]any{"path": s.cfg.StudioModelOutput, "type": "DirectoryOrCreate"}},
				},
			},
		},
	}
	return spec, nil
}

// registerModelVersion makes the merged model discoverable and deployable. The
// base family is preserved (same spec.model) so it appears as a version of the
// original model; lineage lives in annotations.
func (s *WorkspaceService) registerModelVersion(ctx context.Context, row model.Workspace, artifact ModelArtifact) *common.APIError {
	exists, err := s.cluster.ModelVersionExists(ctx, row.Name)
	if err != nil {
		return workspaceAPIError(err)
	}
	if exists {
		return nil
	}
	var matched MatchedResources
	_ = json.Unmarshal([]byte(row.Matched), &matched)

	annotations := map[string]string{
		"medrouter.io/base-modelversion": artifact.Name,
		"medrouter.io/finetune-method":   "lora",
		"medrouter.io/finetune-job":      row.Name,
	}
	if matched.Dataset != "" {
		annotations["medrouter.io/finetune-dataset"] = matched.Dataset
	}
	labels := map[string]string{
		"app.kubernetes.io/managed-by": "medrouter",
		"medrouter.io/finetuned":       "true",
	}
	spec := map[string]any{
		"model":        artifact.Model,
		"version":      row.Name,
		"architecture": artifact.Architecture,
		"quantization": artifact.Quantization,
		"storage": map[string]any{
			"strategy": "HostPath",
			"hostPath": map[string]any{"path": s.cfg.StudioModelOutput + "/" + row.Name},
		},
	}
	if err := s.cluster.CreateModelVersion(ctx, row.Name, labels, annotations, spec); err != nil {
		var status *kube.StatusError
		if errors.As(err, &status) && status.Reason == "ALREADY_EXISTS" {
			return nil
		}
		return workspaceAPIError(err)
	}
	return nil
}
