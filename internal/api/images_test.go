package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func newFakeRegistry(t *testing.T, existing string) *httptest.Server {
	t.Helper()
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if r.URL.Query().Get("scope") != "repository:goauthentik/dev-server:pull" {
				t.Errorf("unexpected token scope %q", r.URL.Query().Get("scope"))
			}
			_, _ = w.Write([]byte(`{"token":"anon"}`))
		case "/v2/goauthentik/dev-server/manifests/" + existing:
			if r.Header.Get("Authorization") != "Bearer anon" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
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
