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
		Adapter:      artifact.Adapter,
	}
	overrides := map[string]any{}
	if artifact.Adapter != "" {
		// ${MODEL_PATH} is expanded by the entrypoint's shell at runtime, so
		// the adapter path does not depend on where the operator mounts the
		// model.
		overrides["lora_paths"] = artifact.Name + "=${MODEL_PATH}/" + artifact.Adapter
	}
	spec := map[string]any{
		"modelRef":   artifact.Name,
		"profileRef": profile.Metadata.Name,
		"overrides":  overrides,
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

// advanceFineTune registers the adapter as a ModelVersion once training
// succeeds. There is no merge step: the serving profile loads the adapter
// through --lora-paths, and the adapter already lives inside the base tree.
func (s *WorkspaceService) advanceFineTune(ctx context.Context, row model.Workspace) (phase, reason, modelVersion string) {
	if row.ModelVersion != "" {
		return model.WorkspaceReady, "", row.ModelVersion
	}
	if s.cfg.StudioDryRun || s.cluster == nil || s.modelInfo == nil {
		return model.WorkspaceSucceeded, "", ""
	}
	// Training itself is done, so a registration problem is reported with the
	// reason on a successful run rather than as a failed one: the work the user
	// asked for completed, and the registration is retried on the next pass.
	artifact, ok := s.modelInfo(ctx, row.SourceName)
	if !ok {
		return model.WorkspaceSucceeded, "BASE_MODEL_MISSING", ""
	}
	if apiErr := s.registerModelVersion(ctx, row, artifact); apiErr != nil {
		return model.WorkspaceSucceeded, apiErr.Code, ""
	}
	return model.WorkspaceReady, "", modelVersionName(artifact.Model, adapterVersion(row))
}

// adapterVersion is the version half of a fine-tuned ModelVersion's name. The
// platform requires metadata.name to be <model>-<version> (optionally with a
// suffix), so the version cannot simply repeat the workspace name: it is the
// dataset and owner that distinguish this adapter from the base version.
func adapterVersion(row model.Workspace) string {
	var matched MatchedResources
	_ = json.Unmarshal([]byte(row.Matched), &matched)
	dataset := matched.Dataset
	if dataset == "" {
		dataset = "finetune"
	}
	slug := dnsNamePattern.ReplaceAllString(strings.ToLower(dataset), "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "finetune"
	}
	return fmt.Sprintf("%s-u%d-lora", slug, row.OwnerID)
}

func modelVersionName(baseModel, version string) string { return baseModel + "-" + version }

// registerModelVersion makes the adapter discoverable and deployable. The base
// family is preserved (same spec.model) so it appears as a version of the
// original model; the storage stays the base directory and the annotation tells
// the deployment where the adapter sits inside it.
func (s *WorkspaceService) registerModelVersion(ctx context.Context, row model.Workspace, artifact ModelArtifact) *common.APIError {
	name := modelVersionName(artifact.Model, adapterVersion(row))
	exists, err := s.cluster.ModelVersionExists(ctx, name)
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
		"medrouter.io/lora-adapter":      adapterSubdir(row.Name),
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
		"version":      adapterVersion(row),
		"architecture": artifact.Architecture,
		"quantization": artifact.Quantization,
		// Same weights as the base: the adapter is a directory inside this
		// mount, which is what the serving pod needs.
		"storage": map[string]any{
			"strategy": "HostPath",
			"hostPath": map[string]any{"path": artifact.Storage.HostPath.Path},
		},
	}
	if err := s.cluster.CreateModelVersion(ctx, name, labels, annotations, spec); err != nil {
		var status *kube.StatusError
		if errors.As(err, &status) && status.Reason == "ALREADY_EXISTS" {
			return nil
		}
		return workspaceAPIError(err)
	}
	return nil
}
