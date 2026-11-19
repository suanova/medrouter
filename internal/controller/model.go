package controller

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/suanova/medrouter/internal/common"
	"github.com/suanova/medrouter/internal/service"
)

type ModelController struct {
	catalog *service.ModelCatalog
	files   *service.FilesService
}

func NewModelController(catalog *service.ModelCatalog, files *service.FilesService) *ModelController {
	return &ModelController{catalog: catalog, files: files}
}

func (h *ModelController) List(c *gin.Context) {
	common.Success(c, h.catalog.List(c.Request.Context()))
}

func (h *ModelController) Get(c *gin.Context) {
	detail, found := h.catalog.Detail(c.Request.Context(), c.Param("name"))
	if detail != nil && !detail.Available {
		// A cluster-side failure is not a missing model: keep the 200 envelope
		// so the page can show a retryable unavailable state.
		common.Success(c, detail)
		return
	}
	if !found {
		common.Fail(c, common.NewAPIErrorStatus(http.StatusNotFound, "MODEL_NOT_FOUND", "Model not found"))
		return
	}
	common.Success(c, detail)
}

// Files lists one directory level of a model version. Browsing is public;
// download (Download) is the authenticated action.
func (h *ModelController) Files(c *gin.Context) {
	if h.files == nil {
		common.Success(c, &service.FileListing{
			FilesStatus: service.FilesStatus{
				Available:   false,
				Reason:      service.FilesDisabled,
				Message:     "File browsing is disabled for this deployment.",
				Retryable:   false,
				GeneratedAt: time.Now().UTC(),
			},
			Entries: []service.FileEntry{},
		})
		return
	}

	limit := 0
	if raw := c.Query("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	files, found := h.files.List(c.Request.Context(), c.Param("name"), c.Query("ref"), c.Query("prefix"), c.Query("cursor"), limit)
	if !found {
		common.Fail(c, common.NewAPIErrorStatus(http.StatusNotFound, "MODEL_VERSION_NOT_FOUND", "Model version not found"))
		return
	}
	common.Success(c, files)
}

// Download signs a short-lived GET URL for one model file. The route sits
// behind RequireAuth, so the caller is a signed-in user.
func (h *ModelController) Download(c *gin.Context) {
	if h.files == nil {
		common.Fail(c, common.NewAPIErrorStatus(http.StatusServiceUnavailable, service.FilesDisabled, "File browsing is disabled"))
		return
	}
	download, found := h.files.Download(c.Request.Context(), c.Param("name"), c.Query("ref"), c.Query("path"))
	if !found {
		common.Fail(c, common.NewAPIErrorStatus(http.StatusNotFound, "MODEL_VERSION_NOT_FOUND", "Model version not found"))
		return
	}
	if !download.Available {
		status, code := downloadErrorStatus(download.Reason)
		message := download.Message
		if message == "" {
			message = "The file could not be downloaded."
		}
		common.Fail(c, common.NewAPIErrorStatus(status, code, message))
		return
	}
	common.Success(c, download)
}
