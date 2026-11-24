package controller

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/suanova/medrouter/internal/common"
	"github.com/suanova/medrouter/internal/middleware"
	"github.com/suanova/medrouter/internal/service"
)

// StudioController owns the studio's server-side state: the favorite list that
// drives one-click launch, and the workspaces it produces.
type StudioController struct {
	favorites  *service.FavoriteService
	workspaces *service.WorkspaceService
	finetune   *service.FineTuneService
}

func NewStudioController(
	favorites *service.FavoriteService,
	workspaces *service.WorkspaceService,
	finetune *service.FineTuneService,
) *StudioController {
	return &StudioController{favorites: favorites, workspaces: workspaces, finetune: finetune}
}

func (h *StudioController) requireUser(c *gin.Context) (*int64, bool) {
	user := middleware.CurrentUser(c)
	if user == nil {
		common.Fail(c, common.NewAPIErrorStatus(http.StatusUnauthorized, "UNAUTHENTICATED", "Please sign in"))
		return nil, false
	}
	return &user.ID, true
}

// ListFavorites returns the caller's starred models and datasets.
func (h *StudioController) ListFavorites(c *gin.Context) {
	userID, ok := h.requireUser(c)
	if !ok {
		return
	}
	favorites, apiErr := h.favorites.List(*userID)
	if apiErr != nil {
		common.Fail(c, apiErr)
		return
	}
	common.Success(c, gin.H{"favorites": favorites})
}

type favoriteRequest struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// AddFavorite stars one catalog entry. It is idempotent.
func (h *StudioController) AddFavorite(c *gin.Context) {
	userID, ok := h.requireUser(c)
	if !ok {
		return
	}
	var req favoriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.NewAPIErrorStatus(http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body"))
		return
	}
	favorite, apiErr := h.favorites.Add(*userID, req.Kind, req.Name)
	if apiErr != nil {
		common.Fail(c, apiErr)
		return
	}
	common.Success(c, favorite)
}

// RemoveFavorite unstars one catalog entry.
func (h *StudioController) RemoveFavorite(c *gin.Context) {
	userID, ok := h.requireUser(c)
	if !ok {
		return
	}
	if apiErr := h.favorites.Remove(*userID, c.Param("kind"), c.Param("name")); apiErr != nil {
		common.Fail(c, apiErr)
		return
	}
	common.Success(c, gin.H{"kind": c.Param("kind"), "name": c.Param("name")})
}

type workspaceRequest struct {
	SourceKind string `json:"source_kind"`
	SourceName string `json:"source_name"`
	// DatasetName is required when starting a fine-tune from a model favorite.
	DatasetName string `json:"dataset_name"`
}

// ListWorkspaces returns the caller's launched workspaces.
func (h *StudioController) ListWorkspaces(c *gin.Context) {
	userID, ok := h.requireUser(c)
	if !ok {
		return
	}
	workspaces, apiErr := h.workspaces.List(*userID)
	if apiErr != nil {
		common.Fail(c, apiErr)
		return
	}
	common.Success(c, gin.H{"workspaces": workspaces})
}

// CreateWorkspace launches the environment for one starred entry. Everything
// beyond which favorite to start is decided by the service.
func (h *StudioController) CreateWorkspace(c *gin.Context) {
	userID, ok := h.requireUser(c)
	if !ok {
		return
	}
	var req workspaceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.NewAPIErrorStatus(http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body"))
		return
	}
	workspace, apiErr := h.workspaces.Create(c.Request.Context(), *userID, req.SourceKind, req.SourceName, req.DatasetName)
	if apiErr != nil {
		common.Fail(c, apiErr)
		return
	}
	common.Success(c, workspace)
}

// SetWorkspaceRunning starts or stops one workspace.
func (h *StudioController) SetWorkspaceRunning(c *gin.Context) {
	userID, ok := h.requireUser(c)
	if !ok {
		return
	}
	id, apiErr := workspaceID(c)
	if apiErr != nil {
		common.Fail(c, apiErr)
		return
	}
	workspace, apiErr := h.workspaces.SetRunning(c.Request.Context(), *userID, id, c.Param("action") == "start")
	if apiErr != nil {
		common.Fail(c, apiErr)
		return
	}
	common.Success(c, workspace)
}

// DeleteWorkspace removes the environment and its record.
func (h *StudioController) DeleteWorkspace(c *gin.Context) {
	userID, ok := h.requireUser(c)
	if !ok {
		return
	}
	id, apiErr := workspaceID(c)
	if apiErr != nil {
		common.Fail(c, apiErr)
		return
	}
	if apiErr := h.workspaces.Delete(c.Request.Context(), *userID, id); apiErr != nil {
		common.Fail(c, apiErr)
		return
	}
	common.Success(c, gin.H{"id": id})
}

func workspaceID(c *gin.Context) (int64, *common.APIError) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, common.NewAPIErrorStatus(http.StatusBadRequest, "INVALID_ID", "Invalid workspace id")
	}
	return id, nil
}

// FineTuneTargets reports which datasets can be used to fine-tune a model. The
// picker filters on this, and the launch path re-checks it server-side.
func (h *StudioController) FineTuneTargets(c *gin.Context) {
	if _, ok := h.requireUser(c); !ok {
		return
	}
	model := c.Query("model")
	if model == "" {
		common.Fail(c, common.NewAPIErrorStatus(http.StatusBadRequest, "INVALID_REQUEST", "model is required"))
		return
	}
	common.Success(c, h.finetune.Targets(c.Request.Context(), model))
}
