package controller

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/suanova/medrouter/internal/common"
	"github.com/suanova/medrouter/internal/middleware"
	"github.com/suanova/medrouter/internal/service"
)

// fileNotFound carries the version-not-found code of the calling library, so
// one set of handlers can serve both models and datasets.
type fileNotFound struct {
	code    string
	message string
}

// actorID is the quota and audit key for the signed-in caller. Download and
// preview routes sit behind RequireAuth, so this is normally non-empty.
func actorID(c *gin.Context) string {
	if user := middleware.CurrentUser(c); user != nil {
		return strconv.FormatInt(user.ID, 10)
	}
	return ""
}

// downloadFile validates one file and either signs a presigned URL or reports
// that the caller must stream /files/content instead.
func downloadFile(c *gin.Context, files *service.FilesService, missing fileNotFound) {
	if files == nil {
		common.Fail(c, common.NewAPIErrorStatus(http.StatusServiceUnavailable, service.FilesDisabled, "File browsing is disabled"))
		return
	}
	download, found := files.Download(c.Request.Context(), actorID(c),
		c.Param("name"), c.Query("ref"), c.Query("path"))
	if !found {
		common.Fail(c, common.NewAPIErrorStatus(http.StatusNotFound, missing.code, missing.message))
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

// previewFile describes one file. A readable file the browser cannot render is
// still a success with previewable=false: the object exists, only the preview
// is unavailable.
func previewFile(c *gin.Context, files *service.FilesService, missing fileNotFound) {
	if files == nil {
		common.Fail(c, common.NewAPIErrorStatus(http.StatusServiceUnavailable, service.FilesDisabled, "File browsing is disabled"))
		return
	}
	preview, found := files.Preview(c.Request.Context(),
		c.Param("name"), c.Query("ref"), c.Query("path"))
	if !found {
		common.Fail(c, common.NewAPIErrorStatus(http.StatusNotFound, missing.code, missing.message))
		return
	}
	if preview.Previewable && (preview.Kind == "image" || preview.Kind == "pdf") {
		preview.ContentURL = contentURL(c)
	}
	common.Success(c, preview)
}

// contentFile streams an image or PDF from the object store through the API.
// Streaming is what keeps the object-store endpoint and credentials out of the
// browser; it also lets Range requests pass through so a PDF viewer can seek.
func contentFile(c *gin.Context, files *service.FilesService, missing fileNotFound) {
	if files == nil {
		common.Fail(c, common.NewAPIErrorStatus(http.StatusServiceUnavailable, service.FilesDisabled, "File browsing is disabled"))
		return
	}
	release, err := files.AcquireSlot(c.Request.Context())
	if err != nil {
		common.Fail(c, common.NewAPIErrorStatus(http.StatusTooManyRequests, "PREVIEW_BUSY", "Too many concurrent previews, try again shortly"))
		return
	}
	defer release()

	offset, length, ranged := parseRange(c.GetHeader("Range"))
	// download=1 is proxy-mode download: any file type, capped by the download
	// limit instead of the preview limit, and served as an attachment.
	asDownload := c.Query("download") == "1"
	content, reason, found := files.Content(c.Request.Context(), actorID(c),
		c.Param("name"), c.Query("ref"), c.Query("path"), offset, length, asDownload)
	if !found {
		common.Fail(c, common.NewAPIErrorStatus(http.StatusNotFound, missing.code, missing.message))
		return
	}
	if reason != "" {
		status, code := contentErrorStatus(reason)
		common.Fail(c, common.NewAPIErrorStatus(status, code, serviceMessage(reason)))
		return
	}
	defer func() { _ = content.Body.Close() }()

	// A large PDF can take longer than the server's global write timeout.
	if timeout := files.StreamTimeout(); timeout > 0 {
		_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(timeout))
	}

	disposition := "inline"
	if content.Attachment {
		disposition = "attachment"
	}

	header := c.Writer.Header()
	header.Set("Content-Type", content.ContentType)
	header.Set("Content-Disposition", disposition)
	header.Set("Accept-Ranges", "bytes")
	header.Set("Cache-Control", "private, max-age=300")
	// The global security headers deny framing; a same-origin preview may
	// legitimately want to embed the PDF.
	header.Set("X-Frame-Options", "SAMEORIGIN")
	if content.ETag != "" {
		header.Set("ETag", `"`+content.ETag+`"`)
	}
	if ranged {
		header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d",
			content.Offset, content.Offset+content.Length-1, content.Size))
		c.Status(http.StatusPartialContent)
	} else {
		header.Set("Content-Length", strconv.FormatInt(content.Length, 10))
		c.Status(http.StatusOK)
	}
	if _, err := io.Copy(c.Writer, content.Body); err != nil {
		// The status line is already sent; all we can do is stop the stream.
		return
	}
}

// contentURL is the same-origin stream URL for the object being previewed. It
// is derived from the current request so the configured API prefix and any
// proxy rewrite are preserved.
func contentURL(c *gin.Context) string {
	path := strings.TrimSuffix(c.Request.URL.EscapedPath(), "/preview") + "/content"
	return path + "?" + c.Request.URL.Query().Encode()
}

// parseRange understands a single "bytes=start-end" or "bytes=start-" range.
// Anything else (multi-range, other units, garbage) falls back to the full
// object, which is always a valid S3 response.
func parseRange(raw string) (offset, length int64, ok bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "bytes=") {
		return 0, 0, false
	}
	spec := strings.TrimPrefix(raw, "bytes=")
	if strings.Contains(spec, ",") {
		return 0, 0, false
	}
	startRaw, endRaw, found := strings.Cut(spec, "-")
	if !found {
		return 0, 0, false
	}
	start, err := strconv.ParseInt(strings.TrimSpace(startRaw), 10, 64)
	if err != nil || start < 0 {
		return 0, 0, false
	}
	if strings.TrimSpace(endRaw) == "" {
		return start, 0, true
	}
	end, err := strconv.ParseInt(strings.TrimSpace(endRaw), 10, 64)
	if err != nil || end < start {
		return 0, 0, false
	}
	return start, end - start + 1, true
}

// contentErrorStatus maps a streaming failure onto a client or upstream status.
func contentErrorStatus(reason string) (int, string) {
	switch reason {
	case service.FilesInvalidPath:
		return http.StatusBadRequest, service.FilesInvalidPath
	case service.FilesPreviewUnsupported:
		return http.StatusUnsupportedMediaType, reason
	case service.FilesPreviewTooLarge:
		return http.StatusRequestEntityTooLarge, reason
	case service.FilesPreviewDisabled:
		return http.StatusServiceUnavailable, reason
	case service.FilesNotFound:
		return http.StatusNotFound, "FILE_NOT_FOUND"
	case service.FilesAuth:
		return http.StatusBadGateway, reason
	case service.FilesTimeout:
		return http.StatusGatewayTimeout, reason
	default:
		return http.StatusServiceUnavailable, reason
	}
}

// serviceMessage is the operator-facing fallback text when a reason has no
// dedicated message.
func serviceMessage(reason string) string {
	switch reason {
	case service.FilesPreviewUnsupported:
		return "This file format cannot be previewed in the browser."
	case service.FilesPreviewTooLarge:
		return "This file is too large to preview."
	default:
		return "The file cannot be streamed."
	}
}
