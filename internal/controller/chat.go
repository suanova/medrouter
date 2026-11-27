package controller

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/suanova/medrouter/internal/common"
	"github.com/suanova/medrouter/internal/config"
	"github.com/suanova/medrouter/internal/middleware"
	"github.com/suanova/medrouter/internal/service"
)

// chatSessionIDPattern matches the ids dsh-api mints (sess-<base36>-<random>).
// The same value also reaches an upstream URL path, so anything outside this
// alphabet is rejected rather than escaped.
var chatSessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,200}$`)

// ChatController exposes the DeepSeek Harness agent behind the 对话 page.
// Every call carries the authenticated user's id to dsh-api; the runtime owns
// workspace separation and refuses tasks/sessions created by another account.
type ChatController struct {
	cfg *config.Config
	dsh *service.DshClient
}

func NewChatController(cfg *config.Config, dsh *service.DshClient) *ChatController {
	return &ChatController{cfg: cfg, dsh: dsh}
}

// Status reports whether the page can be used and what it supports.
func (cc *ChatController) Status(c *gin.Context) {
	enabled := cc.cfg.DshEnabled && cc.dsh != nil && cc.dsh.Enabled()
	defaultModel := ""
	if len(cc.cfg.DshModels) > 0 {
		defaultModel = cc.cfg.DshModels[0].ID
	}
	payload := gin.H{
		"enabled":          enabled,
		"default_preset":   cc.cfg.DshDefaultPreset,
		"max_prompt_chars": cc.cfg.DshMaxPromptChars,
		"models":           cc.cfg.DshModels,
		"default_model":    defaultModel,
		"uploads": gin.H{
			"enabled":   enabled && cc.cfg.DshUploadsEnabled(),
			"max_bytes": cc.cfg.DshUploadMaxBytes,
			"max_files": cc.cfg.DshUploadMaxFiles,
			"accept":    service.DshUploadAccept(),
		},
	}
	common.Success(c, payload)
}

type chatSubmitRequest struct {
	Prompt      string                  `json:"prompt"`
	SessionID   string                  `json:"sessionId"`
	Preset      string                  `json:"preset"`
	Model       string                  `json:"model"`
	Attachments []service.DshAttachment `json:"attachments"`
}

// Submit starts one turn. It returns as soon as dsh-api has queued the task;
// the client polls GET /chat/tasks/:id for the reply and artifacts.
func (cc *ChatController) Submit(c *gin.Context) {
	if !cc.cfg.DshEnabled || cc.dsh == nil || !cc.dsh.Enabled() {
		common.Fail(c, common.NewAPIError("AGENT_UNAVAILABLE", "The agent is not available"))
		return
	}
	user := middleware.CurrentUser(c)
	if user == nil {
		common.AbortFail(c, common.NewAPIErrorStatus(http.StatusUnauthorized, "UNAUTHENTICATED", "Please sign in"))
		return
	}
	var req chatSubmitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, common.NewAPIError("INVALID_REQUEST", "Invalid request body"))
		return
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		common.Fail(c, common.NewAPIError("INVALID_REQUEST", "Write a message before sending"))
		return
	}
	if utf8.RuneCountInString(prompt) > cc.cfg.DshMaxPromptChars {
		common.Fail(c, common.NewAPIError("INVALID_REQUEST",
			fmt.Sprintf("The message can be at most %d characters", cc.cfg.DshMaxPromptChars)))
		return
	}
	if req.SessionID != "" && !chatSessionIDPattern.MatchString(req.SessionID) {
		common.Fail(c, common.NewAPIError("INVALID_REQUEST", "Invalid session id"))
		return
	}

	attachments := req.Attachments
	if len(attachments) > 0 {
		upload, err := service.LoadDshUploadConfig(cc.cfg)
		if err != nil {
			common.Fail(c, common.NewAPIError("UPLOADS_DISABLED", "File uploads are not configured"))
			return
		}
		if err := service.ValidateDshAttachments(upload, user.ID, attachments); err != nil {
			common.Fail(c, common.NewAPIError("INVALID_ATTACHMENT", err.Error()))
			return
		}
	}

	preset := strings.TrimSpace(req.Preset)
	if preset == "" {
		preset = cc.cfg.DshDefaultPreset
	}
	model := strings.TrimSpace(req.Model)
	if model != "" && !cc.cfg.DshModelAllowed(model) {
		common.Fail(c, common.NewAPIError("INVALID_REQUEST", "Unknown model"))
		return
	}
	task, err := cc.dsh.Submit(c.Request.Context(), user.ID, service.DshSubmitInput{
		Prompt:      prompt,
		SessionID:   req.SessionID,
		Preset:      preset,
		Model:       model,
		Attachments: attachments,
	})
	if err != nil {
		// A submit is the only place a 404 can mean the session is unknown: the
		// task was created on a dsh pod that no longer holds it (scaled in,
		// restarted, or more than one replica without matching routing). Tell
		// the page so it can start a fresh session instead of retrying the
		// same dead id forever.
		var dshErr *service.DshError
		if req.SessionID != "" && errors.As(err, &dshErr) && dshErr.Status == http.StatusNotFound {
			common.Fail(c, common.NewAPIErrorStatus(http.StatusNotFound, "SESSION_EXPIRED",
				"The previous session is no longer available; send a new message to start a fresh one"))
			return
		}
		writeChatError(c, err)
		return
	}
	common.Success(c, gin.H{"task": task})
}

// Task polls one turn. dsh-api fails closed on another user's task id, and the
// user id forwarded here is the one the submit used, so ownership carries over.
func (cc *ChatController) Task(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		common.AbortFail(c, common.NewAPIErrorStatus(http.StatusUnauthorized, "UNAUTHENTICATED", "Please sign in"))
		return
	}
	if cc.dsh == nil || !cc.dsh.Enabled() {
		common.Fail(c, common.NewAPIError("AGENT_UNAVAILABLE", "The agent is not available"))
		return
	}
	task, err := cc.dsh.Task(c.Request.Context(), user.ID, c.Param("id"))
	if err != nil {
		writeChatError(c, err)
		return
	}
	common.Success(c, gin.H{"task": task})
}

// Cancel stops a running turn. Nothing is resubmitted; the task settles as
// canceled and the UI unlocks the composer.
func (cc *ChatController) Cancel(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		common.AbortFail(c, common.NewAPIErrorStatus(http.StatusUnauthorized, "UNAUTHENTICATED", "Please sign in"))
		return
	}
	if cc.dsh == nil || !cc.dsh.Enabled() {
		common.Fail(c, common.NewAPIError("AGENT_UNAVAILABLE", "The agent is not available"))
		return
	}
	if err := cc.dsh.Cancel(c.Request.Context(), user.ID, c.Param("id")); err != nil {
		writeChatError(c, err)
		return
	}
	common.Success(c, gin.H{"status": "cancel-requested"})
}

// Upload stores one attachment in the volume shared with the dsh pod. One file
// per request lets the UI retry a single failed upload.
func (cc *ChatController) Upload(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		common.AbortFail(c, common.NewAPIErrorStatus(http.StatusUnauthorized, "UNAUTHENTICATED", "Please sign in"))
		return
	}
	if !cc.cfg.DshEnabled {
		common.Fail(c, common.NewAPIError("UPLOADS_DISABLED", "The agent is not available"))
		return
	}
	upload, err := service.LoadDshUploadConfig(cc.cfg)
	if err != nil {
		common.Fail(c, common.NewAPIError("UPLOADS_DISABLED", "File uploads are not configured"))
		return
	}
	// Uploads are multipart and much larger than the JSON API body limit, and
	// the server's global 30s read/write deadlines are tuned for JSON. Give
	// this one request room to finish on a slow link.
	responseController := http.NewResponseController(c.Writer)
	_ = responseController.SetReadDeadline(time.Now().Add(5 * time.Minute))
	// Leave 1 MiB for the multipart envelope; SaveDshAttachment enforces the
	// real file size again after writing.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, upload.MaxBytes+(1<<20))
	// 1 MiB stays in memory; the rest spills to a temp file that is copied into
	// the shared volume and then removed.
	if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
		common.Fail(c, common.NewAPIErrorStatus(http.StatusRequestEntityTooLarge, "INVALID_REQUEST",
			fmt.Sprintf("Upload a single file of at most %d MB", upload.MaxBytes>>20)))
		return
	}
	defer func() {
		if c.Request.MultipartForm != nil {
			_ = c.Request.MultipartForm.RemoveAll()
		}
	}()
	file, err := c.FormFile("file")
	if err != nil {
		common.Fail(c, common.NewAPIError("INVALID_REQUEST", "Upload a single file in the file field"))
		return
	}
	attachment, err := service.SaveDshAttachment(upload, user.ID, file)
	if err != nil {
		common.Fail(c, common.NewAPIError("INVALID_ATTACHMENT", err.Error()))
		return
	}
	common.Success(c, gin.H{"attachment": attachment})
}

// Artifact proxies one produced file. Bytes stream through MedRouter so the
// dsh pod stays cluster-internal and the browser only needs the session cookie.
func (cc *ChatController) Artifact(c *gin.Context) {
	user := middleware.CurrentUser(c)
	if user == nil {
		common.AbortFail(c, common.NewAPIErrorStatus(http.StatusUnauthorized, "UNAUTHENTICATED", "Please sign in"))
		return
	}
	if cc.dsh == nil || !cc.dsh.Enabled() {
		common.Fail(c, common.NewAPIError("AGENT_UNAVAILABLE", "The agent is not available"))
		return
	}
	resp, err := cc.dsh.DownloadArtifact(c.Request.Context(), user.ID, c.Param("id"), c.Param("key"))
	if err != nil {
		writeChatError(c, err)
		return
	}
	defer resp.Body.Close()

	if contentType := strings.TrimSpace(resp.Header.Get("Content-Type")); contentType != "" {
		c.Header("Content-Type", contentType)
	} else {
		c.Header("Content-Type", "application/octet-stream")
	}
	if disposition := strings.TrimSpace(resp.Header.Get("Content-Disposition")); disposition != "" {
		c.Header("Content-Disposition", disposition)
	}
	if length := strings.TrimSpace(resp.Header.Get("Content-Length")); length != "" {
		c.Header("Content-Length", length)
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Status(http.StatusOK)
	// A produced file can be tens of megabytes; extend the server's 60s write
	// deadline for this response only.
	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(5 * time.Minute))
	if _, err := io.Copy(c.Writer, resp.Body); err != nil {
		// The client went away; there is no response left to correct.
		return
	}
}

// writeChatError keeps upstream error text out of 5xx responses while still
// giving the page a stable code it can translate.
func writeChatError(c *gin.Context, err error) {
	var dshErr *service.DshError
	if errors.As(err, &dshErr) {
		switch {
		case dshErr.Status == http.StatusNotFound:
			common.Fail(c, common.NewAPIErrorStatus(http.StatusNotFound, "TASK_NOT_FOUND", "Task not found"))
		case dshErr.Status >= 400 && dshErr.Status < 500:
			message := dshErr.Message
			if message == "" {
				message = "The agent rejected the request"
			}
			common.Fail(c, common.NewAPIError("AGENT_REQUEST_INVALID", message))
		default:
			log.Printf("chat: dsh returned %d: %v", dshErr.Status, dshErr)
			common.Fail(c, common.NewAPIErrorStatus(http.StatusBadGateway, "AGENT_ERROR", "The agent failed to process the request"))
		}
		return
	}
	var unavailable *service.DshUnavailableError
	if errors.As(err, &unavailable) {
		log.Printf("chat: dsh unavailable: %v", err)
		common.Fail(c, common.NewAPIErrorStatus(http.StatusServiceUnavailable, "AGENT_UNAVAILABLE", "The agent is not reachable"))
		return
	}
	log.Printf("chat: %v", err)
	common.Fail(c, common.NewAPIErrorStatus(http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error"))
}
