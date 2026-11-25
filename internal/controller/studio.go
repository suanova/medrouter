package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

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

// OpenNotebook signs the browser into a running notebook and hands it the
// address to open. The token never leaves the server: the login call is made
// here, and only Jupyter's identity cookie is relayed to the browser.
func (h *StudioController) OpenNotebook(c *gin.Context) {
	userID, ok := h.requireUser(c)
	if !ok {
		return
	}
	id, apiErr := workspaceID(c)
	if apiErr != nil {
		common.Fail(c, apiErr)
		return
	}
	session, apiErr := h.workspaces.NotebookSession(c.Request.Context(), *userID, id)
	if apiErr != nil {
		common.Fail(c, apiErr)
		return
	}
	cookies, err := loginJupyter(c.Request.Context(), session)
	if err != nil {
		common.Fail(c, common.NewAPIErrorStatus(http.StatusBadGateway, "JUPYTER_LOGIN_FAILED",
			"The notebook did not accept the login"))
		return
	}
	for _, cookie := range cookies {
		c.Writer.Header().Add("Set-Cookie", cookie)
	}
	// A session response is never cached anywhere.
	c.Header("Cache-Control", "no-store")
	common.Success(c, gin.H{"url": session.URL})
}

// jupyterClient does not follow redirects: a login that answers with one is a
// login that failed, and following it would hide where the request went.
var jupyterClient = &http.Client{
	Timeout: 10 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// loginJupyter authenticates against the environment's own endpoint with the
// server token and returns the identity cookie, re-scoped to the environment's
// path on the browser's origin.
func loginJupyter(ctx context.Context, session service.NotebookSession) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, session.Upstream, nil)
	if err != nil {
		return nil, err
	}
	// The token travels in a header, never in a URL.
	req.Header.Set("Authorization", "token "+session.Token)
	resp, err := jupyterClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8<<10))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("login answered %d", resp.StatusCode)
	}

	published, err := url.Parse(session.URL)
	if err != nil {
		return nil, err
	}
	path := published.Path
	if !strings.HasSuffix(path, "/") {
		path = path[:strings.LastIndex(path, "/")+1]
	}
	cookies := relayJupyterCookies(resp.Header.Values("Set-Cookie"), path, published.Scheme == "https")
	if len(cookies) == 0 {
		return nil, errors.New("the notebook set no identity cookie")
	}
	return cookies, nil
}

// relayJupyterCookies keeps Jupyter's identity cookie and drops everything
// else. The XSRF cookie is installed by the page load that follows, with the
// attributes the server itself wants, so relaying it would only be a second
// guess. Only name and value are kept; the scope is the environment's own path
// and the browser session's lifetime.
func relayJupyterCookies(setCookies []string, path string, secure bool) []string {
	out := make([]string, 0, len(setCookies))
	for _, raw := range setCookies {
		pair := raw
		if cut := strings.Index(pair, ";"); cut >= 0 {
			pair = pair[:cut]
		}
		name, _, ok := strings.Cut(pair, "=")
		if !ok || !strings.HasPrefix(name, "username-") {
			continue
		}
		cookie := pair + "; Path=" + path + "; HttpOnly; SameSite=Lax"
		if secure {
			cookie += "; Secure"
		}
		out = append(out, cookie)
	}
	return out
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
