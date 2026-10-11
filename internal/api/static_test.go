package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func uiServer(t *testing.T) *Server {
	t.Helper()
	s, _ := newServer(t, Config{UI: fstest.MapFS{
		"index.html":          {Data: []byte("<html>index</html>")},
		"favicon.svg":         {Data: []byte("<svg/>")},
		"assets/index-abc.js": {Data: []byte("console.log(1)")},
	}})
	return s
}

func get(t *testing.T, s *Server, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestUIServesClientRoutesAsIndex(t *testing.T) {
	s := uiServer(t)
	for _, target := range []string{"/", "/deployments", "/deployments/foo/edit", "/shell?cluster=wc1"} {
		rec := get(t, s, target)
		requireStatus(t, rec, http.StatusOK)
		if !strings.Contains(rec.Body.String(), "index") {
			t.Errorf("%s served %q, want index.html", target, rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("%s Cache-Control = %q, want no-cache", target, rec.Header().Get("Cache-Control"))
		}
	}
}

func TestUIServesHashedAssetsImmutable(t *testing.T) {
	s := uiServer(t)
	rec := get(t, s, "/assets/index-abc.js")
	requireStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("Cache-Control = %q, want immutable", rec.Header().Get("Cache-Control"))
	}
	requireStatus(t, get(t, s, "/assets/gone-123.js"), http.StatusNotFound)
	requireStatus(t, get(t, s, "/favicon.svg"), http.StatusOK)
}

func TestUIDoesNotShadowAPIOrAuth(t *testing.T) {
	s := uiServer(t)
	rec := get(t, s, "/api/v1/no-such-route")
	requireStatus(t, rec, http.StatusNotFound)
	if !strings.Contains(rec.Body.String(), `"error"`) {
		t.Errorf("unknown API route served %q, want a JSON error", rec.Body.String())
	}
	// Static assets are public; the API still is not.
	requireStatus(t, get(t, s, "/api/v1/deployments"), http.StatusUnauthorized)
	requireStatus(t, get(t, s, "/healthz"), http.StatusOK)
}
