package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestSecurityHeadersOnAPIRoutes(t *testing.T) {
	s, _ := newServer(t, Config{})
	rec := requestWithBearer(t, s, "/api/v1/deployments", testToken)
	requireStatus(t, rec, http.StatusOK)
	for k, want := range securityHeaderValues {
		if got := rec.Header().Values(k); len(got) != 1 || got[0] != want {
			t.Errorf("%s = %q, want [%q]", k, got, want)
		}
	}
}

func TestSPAGetsItsOwnCSP(t *testing.T) {
	s := uiServer(t)
	s.cfg.OIDCConfig.Issuer = "https://idp.example:9443/application/o/bench/"
	for _, target := range []string{"/", "/deployments", "/assets/index-abc.js"} {
		rec := get(t, s, target)
		csp := rec.Header().Values("Content-Security-Policy")
		if len(csp) != 1 {
			t.Fatalf("%s: Content-Security-Policy = %q, want exactly one", target, csp)
		}
		for _, want := range []string{
			"script-src 'self' 'wasm-unsafe-eval'",
			"connect-src 'self' data: https://api.github.com https://idp.example:9443;",
			"frame-src 'self' https://idp.example:9443;",
			"frame-ancestors 'self'",
		} {
			if !strings.Contains(csp[0], want) {
				t.Errorf("%s: CSP %q lacks %q", target, csp[0], want)
			}
		}
	}
}

func TestSPACSPWithoutOIDC(t *testing.T) {
	csp := spaCSP("")
	if !strings.Contains(csp, "connect-src 'self' data: https://api.github.com;") || !strings.Contains(csp, "frame-src 'self';") {
		t.Errorf("spaCSP(\"\") = %q", csp)
	}
}

func TestDropUpstreamSecurityHeaders(t *testing.T) {
	res := &http.Response{Header: http.Header{}}
	res.Header.Set("X-Content-Type-Options", "nosniff")
	res.Header.Set("Content-Security-Policy", "default-src 'self'")
	_ = dropUpstreamSecurityHeaders(res)
	if res.Header.Get("X-Content-Type-Options") != "" {
		t.Error("upstream X-Content-Type-Options kept")
	}
	if res.Header.Get("Content-Security-Policy") == "" {
		t.Error("upstream Content-Security-Policy dropped")
	}
}
