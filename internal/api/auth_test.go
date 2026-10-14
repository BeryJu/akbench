package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-logr/logr/funcr"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// fakeVerifier accepts exactly one token.
type fakeVerifier struct{ valid string }

func (f fakeVerifier) Verify(_ context.Context, raw string) (string, error) {
	if raw != f.valid {
		return "", errors.New("bad token")
	}
	return "alice", nil
}

func requestWithBearer(t *testing.T, s *Server, target, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	return rec
}

func TestRequireTokenAcceptsStaticOrOIDC(t *testing.T) {
	s, _ := newServer(t, Config{
		OIDC:       fakeVerifier{valid: "a.valid.jwt"},
		OIDCConfig: OIDCConfig{Issuer: "https://idp.example/", ClientID: "bench"},
	})
	for token, want := range map[string]int{
		testToken:     http.StatusOK,
		"a.valid.jwt": http.StatusOK,
		"forged.jwt":  http.StatusUnauthorized,
		"":            http.StatusUnauthorized,
	} {
		requireStatus(t, requestWithBearer(t, s, "/api/v1/deployments", token), want)
	}
}

func TestRequireTokenOIDCOnly(t *testing.T) {
	s, _ := newServer(t, Config{OIDC: fakeVerifier{valid: "a.valid.jwt"}})
	// newServer fills in testToken; clear it to model an OIDC-only backend.
	s.cfg.Token = ""
	requireStatus(t, requestWithBearer(t, s, "/api/v1/deployments", "a.valid.jwt"), http.StatusOK)
	requireStatus(t, requestWithBearer(t, s, "/api/v1/deployments", testToken), http.StatusUnauthorized)
	requireStatus(t, requestWithBearer(t, s, "/api/v1/deployments", ""), http.StatusUnauthorized)
}

func TestAuthConfigIsPublic(t *testing.T) {
	s, _ := newServer(t, Config{
		OIDC:       fakeVerifier{},
		OIDCConfig: OIDCConfig{Issuer: "https://idp.example/", ClientID: "bench"},
	})
	rec := requestWithBearer(t, s, "/api/v1/auth/config", "")
	requireStatus(t, rec, http.StatusOK)
	got := decodeBody[authConfigResponse](t, rec)
	if got.OIDC == nil || got.OIDC.Issuer != "https://idp.example/" || got.OIDC.ClientID != "bench" ||
		len(got.OIDC.Scopes) != 3 {
		t.Fatalf("auth config = %+v", got.OIDC)
	}

	plain, _ := newServer(t, Config{})
	if got := decodeBody[authConfigResponse](t, requestWithBearer(t, plain, "/api/v1/auth/config", "")); got.OIDC != nil {
		t.Fatalf("auth config without OIDC = %+v, want none", got.OIDC)
	}
}

// auditedRequest serves req through s with a logger capturing every line.
func auditedRequest(t *testing.T, s *Server, req *http.Request) (*httptest.ResponseRecorder, []string) {
	t.Helper()
	var lines []string
	logger := funcr.New(func(prefix, args string) { lines = append(lines, prefix+" "+args) }, funcr.Options{})
	req = req.WithContext(logf.IntoContext(req.Context(), logger))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	return rec, lines
}

func TestAuditLogsWritesWithUser(t *testing.T) {
	s, _ := newServer(t, Config{
		OIDC:       fakeVerifier{valid: "a.valid.jwt"},
		OIDCConfig: OIDCConfig{Issuer: "https://idp.example/", ClientID: "bench"},
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/deployments/missing", nil)
	req.Header.Set("Authorization", "Bearer a.valid.jwt")
	rec, lines := auditedRequest(t, s, req)
	requireStatus(t, rec, http.StatusNotFound)
	if len(lines) != 1 || !strings.Contains(lines[0], `"user"="alice"`) || !strings.Contains(lines[0], `"status"=404`) ||
		!strings.Contains(lines[0], `"path"="/api/v1/deployments/missing"`) {
		t.Errorf("audit lines = %q, want one naming alice, 404 and the path", lines)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/deployments", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	if _, lines := auditedRequest(t, s, req); len(lines) != 0 {
		t.Errorf("reads logged: %q", lines)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/grafana/session", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	if _, lines := auditedRequest(t, s, req); len(lines) != 0 {
		t.Errorf("credential mint logged: %q", lines)
	}
}

func TestAuditLogsRejectedRequests(t *testing.T) {
	s, _ := newServer(t, Config{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/deployments", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	rec, lines := auditedRequest(t, s, req)
	requireStatus(t, rec, http.StatusUnauthorized)
	if len(lines) != 1 || !strings.Contains(lines[0], "Rejected unauthenticated request") ||
		!strings.Contains(lines[0], `"forwardedFor"="203.0.113.7"`) {
		t.Errorf("audit lines = %q, want one rejection with forwardedFor", lines)
	}
}
