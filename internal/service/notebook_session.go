package service

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/suanova/medrouter/internal/common"
	"github.com/suanova/medrouter/internal/kube"
)

// NotebookSession is what the browser needs to open a running notebook without
// the user typing a token.
type NotebookSession struct {
	// URL is where the browser navigates: the public endpoint's lab page.
	URL string
	// Upstream is the operator's own address for the same page. The server
	// calls it to mint the identity cookie, because that is the address the
	// cookie is named after.
	Upstream string
	// Token is the Jupyter server token. It travels in a header only and is
	// never put in a URL, so nothing logs or stores it.
	Token string
}

// NotebookSession resolves the token and the addresses of one workspace's
// jupyter endpoint. The Secret is derived from the DevEnvironment's own status,
// never named by the caller, so this cannot read arbitrary Secrets.
func (s *WorkspaceService) NotebookSession(ctx context.Context, ownerID, id int64) (NotebookSession, *common.APIError) {
	if s.cluster == nil {
		return NotebookSession{}, common.NewAPIErrorStatus(http.StatusServiceUnavailable, "STUDIO_UNAVAILABLE",
			"The cluster is not reachable from this deployment")
	}
	row, apiErr := s.owned(ownerID, id)
	if apiErr != nil {
		return NotebookSession{}, apiErr
	}
	if row.Kind != WorkspaceNotebook {
		return NotebookSession{}, common.NewAPIErrorStatus(http.StatusConflict, "NOT_A_NOTEBOOK",
			"This workspace is not a notebook")
	}

	env, found, err := s.cluster.GetDevEnvironment(ctx, row.Namespace, row.Name)
	if err != nil {
		return NotebookSession{}, workspaceAPIError(err)
	}
	if !found {
		return NotebookSession{}, common.NewAPIErrorStatus(http.StatusConflict, "NOTEBOOK_NOT_FOUND",
			"The notebook is not created yet")
	}
	state := kube.NotebookStateOf(env)
	if state.Endpoint == "" {
		return NotebookSession{}, common.NewAPIErrorStatus(http.StatusConflict, "NOTEBOOK_NOT_RUNNING",
			"The notebook is not running yet")
	}
	ref := env.Status.JupyterTokenSecret
	if ref == nil || ref.Name == "" {
		return NotebookSession{}, common.NewAPIErrorStatus(http.StatusConflict, "NOTEBOOK_TOKEN_MISSING",
			"The notebook has no login token yet")
	}
	namespace := ref.Namespace
	if namespace == "" {
		namespace = row.Namespace
	}
	token, ok, err := s.cluster.GetSecretValue(ctx, namespace, ref.Name, "token")
	if err != nil || !ok {
		return NotebookSession{}, common.NewAPIErrorStatus(http.StatusBadGateway, "NOTEBOOK_TOKEN_UNREADABLE",
			"The notebook's login token could not be read")
	}

	upstream, err := labURL(state.Endpoint)
	if err != nil {
		return NotebookSession{}, common.NewAPIErrorStatus(http.StatusBadGateway, "NOTEBOOK_UNREACHABLE",
			"The notebook endpoint is not a URL")
	}
	published := publicEndpoint(state.Endpoint, s.cfg.StudioNotebookBaseURL)
	public, err := labURL(published)
	if err != nil {
		return NotebookSession{}, common.NewAPIErrorStatus(http.StatusBadGateway, "NOTEBOOK_UNREACHABLE",
			"The notebook endpoint is not a URL")
	}
	return NotebookSession{URL: public, Upstream: upstream, Token: token}, nil
}

// labURL is the JupyterLab page under an environment endpoint. The operator
// publishes the base path (…/<ns>/<name>/), and lab hangs off it.
func labURL(endpoint string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("not an absolute URL: %q", endpoint)
	}
	if !strings.HasSuffix(parsed.Path, "/") {
		parsed.Path += "/"
	}
	parsed.Path += "lab"
	return parsed.String(), nil
}
