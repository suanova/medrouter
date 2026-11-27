package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"

	"github.com/suanova/medrouter/internal/model"
)

// AgentModel is one deployed inference service the DSH agent can run on. ID is
// the InferenceService route name (also the AI gateway's x-ai-eg-model value);
// Name is the human-readable model/version.
type AgentModel struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	OwnerID int64  `json:"owner_id,omitempty"`
}

// ListRunningInferenceModels returns every running inference deployment, or
// only one user's when ownerID > 0. The dsh runtime receives the full list (it
// serves every account); the Chat page receives the caller's.
func ListRunningInferenceModels(ownerID int64) ([]AgentModel, error) {
	query := model.DB.Where("kind = ? AND phase = ?", WorkspaceInference, model.WorkspaceRunning)
	if ownerID > 0 {
		query = query.Where("owner_id = ?", ownerID)
	}
	var rows []model.Workspace
	if err := query.Order("name asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	models := make([]AgentModel, 0, len(rows))
	for _, row := range rows {
		if row.Name == "" {
			continue
		}
		models = append(models, AgentModel{
			ID:      row.Name,
			Name:    agentModelDisplay(row),
			OwnerID: row.OwnerID,
		})
	}
	return models, nil
}

// UserCanUseAgentModel reports whether name is one of the user's running
// inference services. Selecting another account's service is rejected here,
// before the model id reaches dsh or the AI gateway.
func UserCanUseAgentModel(ownerID int64, name string) (bool, error) {
	if ownerID <= 0 || name == "" {
		return false, nil
	}
	var count int64
	err := model.DB.Model(&model.Workspace{}).
		Where("owner_id = ? AND kind = ? AND phase = ? AND name = ?",
			ownerID, WorkspaceInference, model.WorkspaceRunning, name).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// agentModelDisplay prefers the deployed ModelVersion (which carries a
// fine-tuned adapter) over the base model name.
func agentModelDisplay(row model.Workspace) string {
	if row.Spec != "" {
		var spec struct {
			ModelRef string `json:"modelRef"`
		}
		if err := json.Unmarshal([]byte(row.Spec), &spec); err == nil && spec.ModelRef != "" {
			return spec.ModelRef
		}
	}
	if row.SourceName != "" {
		return row.SourceName
	}
	return row.Name
}

// AgentModelCatalogHash identifies one set of model routes. The dsh Deployment
// carries it as a pod-template annotation; a change there rolls the runtime so
// its startup catalog includes the new set.
func AgentModelCatalogHash(models []AgentModel) string {
	hasher := sha256.New()
	for _, agentModel := range models {
		_, _ = hasher.Write([]byte(agentModel.ID))
		_, _ = hasher.Write([]byte{'\n'})
	}
	return hex.EncodeToString(hasher.Sum(nil)[:8])
}

// deploymentAnnotator is the slice of the cluster client the catalog sync
// needs. Asserting it keeps the WorkspaceCluster interface (and its test
// fakes) unchanged for deployments that do not run the DSH runtime.
type deploymentAnnotator interface {
	SetDeploymentPodAnnotation(ctx context.Context, namespace, name, key, value string) error
}

const dshCatalogAnnotation = "medrouter.io/dsh-model-catalog"

// WithDshCatalog wires the runtime Deployment MedRouter rolls when the served
// model set changes.
func (s *WorkspaceService) WithDshCatalog(name, namespace string) *WorkspaceService {
	s.dshDeployment = name
	s.dshNamespace = namespace
	return s
}

// syncAgentCatalog bumps the dsh Deployment's pod-template annotation when the
// running inference set differs from the last one applied. The runtime
// regenerates its pi-ai routes from that set at startup.
func (s *WorkspaceService) syncAgentCatalog(ctx context.Context) {
	if s.cluster == nil || s.dshDeployment == "" || s.dshNamespace == "" {
		return
	}
	annotator, ok := s.cluster.(deploymentAnnotator)
	if !ok {
		return
	}
	models, err := ListRunningInferenceModels(0)
	if err != nil {
		return
	}
	hash := AgentModelCatalogHash(models)

	s.catalogMu.Lock()
	unchanged := hash == s.catalogHash
	s.catalogMu.Unlock()
	if unchanged {
		return
	}

	if err := annotator.SetDeploymentPodAnnotation(ctx, s.dshNamespace, s.dshDeployment, dshCatalogAnnotation, hash); err != nil {
		s.catalogMu.Lock()
		warned := s.catalogWarned
		s.catalogWarned = true
		s.catalogMu.Unlock()
		if !warned {
			log.Printf("dsh catalog: could not roll %s/%s: %v", s.dshNamespace, s.dshDeployment, err)
		}
		return
	}
	s.catalogMu.Lock()
	s.catalogHash = hash
	s.catalogWarned = false
	s.catalogMu.Unlock()
}
