package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func newFakeRegistry(t *testing.T, existing string) *httptest.Server {
	t.Helper()
	var registry *httptest.Server
	registry = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			if r.URL.Query().Get("scope") != "repository:goauthentik/dev-server:pull" {
				t.Errorf("unexpected token scope %q", r.URL.Query().Get("scope"))
			}
			_, _ = w.Write([]byte(`{"token":"anon"}`))
			return
		}
		// ghcr.io challenges every unauthenticated request, even for public images.
		if r.Header.Get("Authorization") != "Bearer anon" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+registry.URL+`/token",service="ghcr.io"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/v2/goauthentik/dev-server/manifests/"+existing {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(registry.Close)
	return registry
}

func TestCheckImage(t *testing.T) {
	registry := newFakeRegistry(t, "gh-main")
	s, _ := newServer(t, Config{})
	s.imageRegistry = registry.URL

	for tag, want := range map[string]bool{"gh-main": true, "gh-0123abc": false} {
		rec := do(t, s, http.MethodGet, "/api/v1/images/check?repository=goauthentik/dev-server&tag="+tag, nil)
		requireStatus(t, rec, http.StatusOK)
		if got := decodeBody[map[string]bool](t, rec)["exists"]; got != want {
			t.Errorf("tag %s: exists = %v, want %v", tag, got, want)
		}
	}
}

func TestCheckImageRejectsOtherRepositories(t *testing.T) {
	s, _ := newServer(t, Config{})
	for _, query := range []string{
		"repository=library/nginx&tag=latest",
		"repository=goauthentik/../other&tag=latest",
		"repository=goauthentik/dev-server&tag=../latest",
	} {
		rec := do(t, s, http.MethodGet, "/api/v1/images/check?"+query, nil)
		requireStatus(t, rec, http.StatusBadRequest)
	}
}
