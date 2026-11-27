package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/suanova/medrouter/internal/common"
)

const maxBodyBytes = 1 << 20 // 1 MiB

// LimitBody caps request bodies and, when a body is present, requires JSON for
// API calls.
func LimitBody() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > maxBodyBytes {
			common.AbortFail(c, common.NewAPIErrorStatus(http.StatusRequestEntityTooLarge,
				"INVALID_REQUEST", "Request body is too large"))
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes)

		if c.Request.ContentLength > 0 && strings.EqualFold(c.Request.Method, http.MethodPost) {
			contentType := c.ContentType()
			if contentType != "" && contentType != "application/json" {
				common.AbortFail(c, common.NewAPIErrorStatus(http.StatusUnsupportedMediaType,
					"INVALID_REQUEST", "Content-Type must be application/json"))
				return
			}
		}
		c.Next()
	}
}

// LimitUploadBody caps a multipart upload route. LimitBody's JSON requirement
// does not apply here: this middleware is registered on a dedicated group so
// file uploads can exceed the 1 MiB API body limit without widening it for
// every other endpoint.
func LimitUploadBody(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > maxBytes {
			common.AbortFail(c, common.NewAPIErrorStatus(http.StatusRequestEntityTooLarge,
				"INVALID_REQUEST", "Request body is too large"))
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		if strings.EqualFold(c.Request.Method, http.MethodPost) {
			if contentType := c.ContentType(); contentType != "multipart/form-data" {
				common.AbortFail(c, common.NewAPIErrorStatus(http.StatusUnsupportedMediaType,
					"INVALID_REQUEST", "Content-Type must be multipart/form-data"))
				return
			}
		}
		c.Next()
	}
}

// SecurityHeaders sets conservative defaults for both API and static replies.
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.Writer.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	}
}

// Recovery converts panics into the standard error envelope.
func Recovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		common.AbortFail(c, common.NewAPIErrorStatus(http.StatusInternalServerError,
			"INTERNAL_ERROR", "Internal server error"))
	})
}
