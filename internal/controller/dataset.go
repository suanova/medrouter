package controller

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/suanova/medrouter/internal/common"
	"github.com/suanova/medrouter/internal/service"
)

type DatasetController struct {
	catalog *service.DatasetCatalog
	files   *service.FilesService
}

func NewDatasetController(catalog *service.DatasetCatalog, files *service.FilesService) *DatasetController {
	return &DatasetController{catalog: catalog, files: files}
}

func (h *DatasetController) List(c *gin.Context) {
	common.Success(c, h.catalog.List(c.Request.Context()))
}

func (h *DatasetController) Get(c *gin.Context) {
	detail, found := h.catalog.Detail(c.Request.Context(), c.Param("name"))
	if detail != nil && !detail.Available {
		// A cluster-side failure is not a missing dataset: keep the 200 envelope
		// so the page can show a retryable unavailable state.
		common.Success(c, detail)
		return
	}
	if !found {
		common.Fail(c, common.NewAPIErrorStatus(http.StatusNotFound, "DATASET_NOT_FOUND", "Dataset not found"))
		return
	}
	common.Success(c, detail)
}

// Files lists one directory level of a dataset version. Browsing is public;
// download (Download) is the authenticated action.
func (h *DatasetController) Files(c *gin.Context) {
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
		common.Fail(c, common.NewAPIErrorStatus(http.StatusNotFound, "DATASET_VERSION_NOT_FOUND", "Dataset version not found"))
		return
	}
	common.Success(c, files)
}

// Download signs a short-lived GET URL for one file. The route sits behind
// RequireAuth, so the caller is a signed-in user.
func (h *DatasetController) Download(c *gin.Context) {
	downloadFile(c, h.files, fileNotFound{
		code:    "DATASET_VERSION_NOT_FOUND",
		message: "Dataset version not found",
	})
}

// Preview describes one dataset file and, for text, returns its first window.
func (h *DatasetController) Preview(c *gin.Context) {
	previewFile(c, h.files, fileNotFound{
		code:    "DATASET_VERSION_NOT_FOUND",
		message: "Dataset version not found",
	})
}

// Content streams an image or PDF so the object store stays private.
func (h *DatasetController) Content(c *gin.Context) {
	contentFile(c, h.files, fileNotFound{
		code:    "DATASET_VERSION_NOT_FOUND",
		message: "Dataset version not found",
	})
}

// downloadErrorStatus maps a stable file reason onto an appropriate HTTP
// status. Object-store failures are upstream (5xx), not client errors.
func downloadErrorStatus(reason string) (int, string) {
	switch reason {
	case service.FilesInvalidPath:
		return http.StatusBadRequest, service.FilesInvalidPath
	case service.FilesNotFound:
		return http.StatusNotFound, "FILE_NOT_FOUND"
	case service.FilesTooLarge:
		return http.StatusRequestEntityTooLarge, service.FilesTooLarge
	case service.FilesQuotaExceeded:
		return http.StatusTooManyRequests, service.FilesQuotaExceeded
	case service.FilesDisabled, service.FilesUnsupportedStorage, service.FilesURIMissing:
		return http.StatusServiceUnavailable, reason
	case service.FilesAuth:
		return http.StatusBadGateway, reason
	case service.FilesUnavailable:
		return http.StatusServiceUnavailable, reason
	case service.FilesTimeout:
		return http.StatusGatewayTimeout, reason
	default:
		// Catalog reasons (DISABLED, NO_CLUSTER, CRD_MISSING, ...) mean the
		// version could not be resolved at all.
		return http.StatusServiceUnavailable, reason
	}
}
