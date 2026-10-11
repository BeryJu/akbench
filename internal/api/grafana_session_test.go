package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// grafanaTestServer is a Server whose one WorkloadCluster's Grafana is a
// fake; *forwarded is the last request that reached it.
func grafanaTestServer(t *testing.T) (*Server, **http.Request) {
	t.Helper()
	forwarded := new(*http.Request)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*forwarded = r.Clone(r.Context())
	}))
	t.Cleanup(upstream.Close)
	wc := &benchv1alpha1.WorkloadCluster{Name: "bench", Namespace: testNamespace}
	wc.Status.GrafanaURL = upstream.URL
	s, _ := newServer(t, Config{}, wc)
	return s, forwarded
}

func proxyRequest(s *Server, cookie *http.Cookie, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/grafana-proxy/bench/api/health", nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	req.AddCookie(&http.Cookie{Name: "grafana_session", Value: "keep-me"})
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	return rec
}

func TestGrafanaProxyRequiresAuth(t *testing.T) {
	s, _ := grafanaTestServer(t)
	requireStatus(t, proxyRequest(s, nil, ""), http.StatusUnauthorized)
	requireStatus(t, proxyRequest(s, nil, "wrong"), http.StatusUnauthorized)
	requireStatus(t, proxyRequest(s, nil, testToken), http.StatusOK)
}

func TestGrafanaSessionCookieOpensProxy(t *testing.T) {
	s, _ := grafanaTestServer(t)

	// Minting needs the API credential.
	unauthenticated := httptest.NewRecorder()
	s.Router().ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodPost, "/api/v1/grafana/session", nil))
	requireStatus(t, unauthenticated, http.StatusUnauthorized)

	rec := do(t, s, http.MethodPost, "/api/v1/grafana/session", nil)
	requireStatus(t, rec, http.StatusOK)
	var session *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == grafanaSessionCookie {
			session = c
		}
	}
	if session == nil {
		t.Fatal("no session cookie set")
	}
	if !session.HttpOnly || session.Path != grafanaProxyPathPrefix || session.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie attributes: %+v", session)
	}
	requireStatus(t, proxyRequest(s, session, ""), http.StatusOK)
}

func TestGrafanaProxyStripsOwnCredentials(t *testing.T) {
	s, last := grafanaTestServer(t)
	session := &http.Cookie{Name: grafanaSessionCookie, Value: s.signGrafanaSession(time.Now().Add(time.Minute))}
	requireStatus(t, proxyRequest(s, session, testToken), http.StatusOK)
	forwarded := *last
	if forwarded == nil {
		t.Fatal("request never reached Grafana")
	}
	if got := forwarded.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization forwarded to Grafana: %q", got)
	}
	if _, err := forwarded.Cookie(grafanaSessionCookie); err == nil {
		t.Error("session cookie forwarded to Grafana")
	}
	if c, err := forwarded.Cookie("grafana_session"); err != nil || c.Value != "keep-me" {
		t.Errorf("Grafana's own cookie not forwarded: %v %v", c, err)
	}
}

func TestGrafanaSessionRejectsExpiredOrForged(t *testing.T) {
	s, _ := newServer(t, Config{})
	now := time.Now()
	valid := s.signGrafanaSession(now.Add(time.Minute))
	if !s.validGrafanaSession(valid, now) {
		t.Fatal("fresh session rejected")
	}
	if s.validGrafanaSession(valid, now.Add(2*time.Minute)) {
		t.Error("expired session accepted")
	}
	other, _ := newServer(t, Config{})
	if s.validGrafanaSession(other.signGrafanaSession(now.Add(time.Minute)), now) {
		t.Error("session signed by another process accepted")
	}
	for _, forged := range []string{"", "123", "9999999999.AAAA", "9999999999." + valid[len("0000000000."):]} {
		if s.validGrafanaSession(forged, now) {
			t.Errorf("forged session %q accepted", forged)
		}
	}
}
