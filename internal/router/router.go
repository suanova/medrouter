package router

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/suanova/medrouter/internal/common"
	"github.com/suanova/medrouter/internal/config"
	"github.com/suanova/medrouter/internal/controller"
	"github.com/suanova/medrouter/internal/middleware"
	"github.com/suanova/medrouter/internal/service"
	"github.com/suanova/medrouter/internal/webui"
)

type Deps struct {
	Config       *config.Config
	Users        *controller.UserController
	Verification *controller.VerificationController
	OAuth        *controller.OAuthController
	Status       *controller.StatusController
	Models       *controller.ModelController
	Datasets     *controller.DatasetController
	Sessions     *service.SessionService
	Static       fs.FS
}

func New(d Deps) *gin.Engine {
	if d.Config.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()
	engine.Use(middleware.Recovery(), middleware.SecurityHeaders())
	// An empty list disables proxy header trust, so ClientIP is RemoteAddr.
	_ = engine.SetTrustedProxies(d.Config.TrustedProxies)

	basePath := d.Config.BasePath
	apiPrefix := d.Config.APIPath()
	api := engine.Group(apiPrefix)
	api.Use(middleware.LimitBody(), middleware.RequireSameOrigin(d.Config.PublicOrigin))

	api.GET("/status", d.Status.Get)
	api.GET("/models", d.Models.List)
	api.GET("/models/:name", d.Models.Get)
	// Browsing is public; downloading a file requires a signed-in user.
	api.GET("/models/:name/files", d.Models.Files)
	api.GET("/datasets", d.Datasets.List)
	api.GET("/datasets/:name", d.Datasets.Get)
	// Browsing is public; downloading a file requires a signed-in user.
	api.GET("/datasets/:name/files", d.Datasets.Files)
	api.POST("/user/register", d.Users.Register)
	api.POST("/user/login", d.Users.Login)
	api.POST("/user/auth/logout", d.Users.Logout)
	api.POST("/verification/send", d.Verification.Send)
	api.GET("/oauth/:provider/start", d.OAuth.Start)
	api.GET("/oauth/:provider/callback", d.OAuth.Callback)

	protected := api.Group("")
	protected.Use(middleware.RequireAuth(d.Sessions))
	protected.GET("/user/self", d.Users.Self)
	protected.GET("/models/:name/files/download", d.Models.Download)
	protected.GET("/datasets/:name/files/download", d.Datasets.Download)
	// Preview transfers file content, so it is gated like a download; the
	// listing above stays public.
	protected.GET("/models/:name/files/preview", d.Models.Preview)
	protected.GET("/models/:name/files/content", d.Models.Content)
	protected.GET("/datasets/:name/files/preview", d.Datasets.Preview)
	protected.GET("/datasets/:name/files/content", d.Datasets.Content)

	engine.GET(basePath+"/healthz", d.Status.Health)

	static := webui.New(d.Static, basePath, apiPrefix)
	// Where the API used to live before API_PREFIX moved it. Keep it a JSON
	// 404 instead of the SPA shell so API clients cannot mistake HTML for a
	// response. When API_PREFIX is still the default the first check covers it.
	legacyAPIPrefix := basePath + "/api"
	engine.NoRoute(func(c *gin.Context) {
		requestPath := c.Request.URL.Path
		if requestPath == apiPrefix || strings.HasPrefix(requestPath, apiPrefix+"/") {
			common.Fail(c, common.NewAPIErrorStatus(http.StatusNotFound, "NOT_FOUND", "API endpoint not found"))
			return
		}
		if apiPrefix != legacyAPIPrefix &&
			(requestPath == legacyAPIPrefix || strings.HasPrefix(requestPath, legacyAPIPrefix+"/")) {
			common.Fail(c, common.NewAPIErrorStatus(http.StatusNotFound, "NOT_FOUND", "API endpoint not found; the API moved to "+apiPrefix))
			return
		}
		static.ServeHTTP(c.Writer, c.Request)
	})

	return engine
}
