package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/suanova/medrouter/internal/service"
)

func TestRelayJupyterCookies(t *testing.T) {
	got := relayJupyterCookies([]string{
		"username-medrouter-workspace-medmcqa-u3-lab-0=abc; Path=/; expires=x",
		"_xsrf=xyz; Path=/",
		"other=1",
	}, "/dev/medrouter-workspace/medmcqa-u3-lab/", true)

	want := "username-medrouter-workspace-medmcqa-u3-lab-0=abc; Path=/dev/medrouter-workspace/medmcqa-u3-lab/; HttpOnly; SameSite=Lax; Secure"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("cookies = %v", got)
	}
}

func TestRelayJupyterCookiesWithoutSecure(t *testing.T) {
	got := relayJupyterCookies([]string{"username-a=b"}, "/dev/ns/env/", false)
	want := "username-a=b; Path=/dev/ns/env/; HttpOnly; SameSite=Lax"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("cookies = %v", got)
	}
}

func TestLoginJupyterMintsSessionCookie(t *testing.T) {
	var auth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		http.SetCookie(w, &http.Cookie{Name: "username-lab", Value: "session-value", Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "_xsrf", Value: "should-not-relay", Path: "/"})
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	cookies, err := loginJupyter(context.Background(), service.NotebookSession{
		URL:      "https://www.medrouter.com:8443/dev/medrouter-workspace/lab/lab",
		Upstream: upstream.URL + "/dev/medrouter-workspace/lab/lab",
		Token:    "secret",
	})
	if err != nil {
		t.Fatalf("loginJupyter: %v", err)
	}
	if auth != "token secret" {
		t.Fatalf("authorization = %q", auth)
	}
	want := "username-lab=session-value; Path=/dev/medrouter-workspace/lab/; HttpOnly; SameSite=Lax; Secure"
	if len(cookies) != 1 || cookies[0] != want {
		t.Fatalf("cookies = %v", cookies)
	}
}

func TestLoginJupyterRejectsRefusedLogin(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Jupyter answers a bad token with its login page, not a cookie.
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	if _, err := loginJupyter(context.Background(), service.NotebookSession{
		URL:      "http://example/dev/ns/env/lab",
		Upstream: upstream.URL + "/dev/ns/env/lab",
		Token:    "wrong",
	}); err == nil {
		t.Fatal("a login without an identity cookie must fail")
	}

	refused := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer refused.Close()
	if _, err := loginJupyter(context.Background(), service.NotebookSession{
		URL:      "http://example/dev/ns/env/lab",
		Upstream: refused.URL + "/dev/ns/env/lab",
		Token:    "wrong",
	}); err == nil {
		t.Fatal("a non-200 login must fail")
	}
}
