package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
	"github.com/goauthentik/authentik-bench/internal/render"
)

// grafanaProxy reverse-proxies /grafana-proxy/{cluster}/* straight to that
// WorkloadCluster's own Grafana NodePort (status.grafanaURL, populated by
// WorkloadClusterReconciler's ensureGrafana - see render.GrafanaObjects'
// own doc comment for the full story of why this exists at all: the k8s
// API server's own service-proxy subresource content-rewrites absolute
// HTML paths in a way that breaks Grafana's own, so the browser has to
// reach this route directly instead of through that mechanism).
//
// Registered outside the /api/v1 route group (see Router): an <iframe
// src=> can't attach the Authorization header s.requireToken demands, so
// this route accepts a session cookie minted by an authenticated request
// instead (grafana_session.go). Grafana itself also requires real
// credentials now (render.GrafanaObjects), which this handler injects below
// - so an ak-bench session/token is required to reach Grafana through here,
// and reaching Grafana's NodePort by any other path (the only way to bypass
// this handler, given cross-cluster reachability needs a NodePort at all)
// hits Grafana's own login screen instead of an open admin session.
//
// The Director deliberately does NOT strip the "/grafana-proxy/<cluster>"
// prefix from the forwarded request path - the one deliberate difference
// from a typical strip-prefix reverse proxy. Grafana's own
// GF_SERVER_SERVE_FROM_SUB_PATH=true (render.GrafanaObjects) means Grafana
// expects exactly that: a reverse proxy in front of it that preserves the
// sub-path rather than stripping it.
func (s *Server) grafanaProxy(w http.ResponseWriter, r *http.Request) {
	if !s.grafanaAuthorized(r) {
		writeError(w, r, http.StatusUnauthorized, "sign in to the API server to open Grafana")
		return
	}
	clusterName := chi.URLParam(r, "cluster")

	var wc benchv1alpha1.WorkloadCluster
	key := types.NamespacedName{Name: clusterName, Namespace: s.cfg.Namespace}
	if err := s.client.Get(r.Context(), key, &wc); err != nil {
		writeAPIError(w, r, err)
		return
	}
	if wc.Status.GrafanaURL == "" {
		writeError(w, r, http.StatusNotFound, "cluster has no Grafana URL recorded yet")
		return
	}

	adminPassword, err := s.grafanaAdminPassword(r.Context(), &wc)
	if err != nil {
		writeError(w, r, http.StatusBadGateway, err.Error())
		return
	}

	target, err := url.Parse(wc.Status.GrafanaURL)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "invalid recorded Grafana URL: "+err.Error())
		return
	}

	proxy := &httputil.ReverseProxy{
		// Rewrite, not the deprecated Director: ProxyRequest.SetURL also
		// joins target's own path onto the outbound request's, which isn't
		// what's wanted here (see doc comment above) - Out's fields are set
		// directly instead, the same as the old Director did to req
		// directly.
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = target.Scheme
			pr.Out.URL.Host = target.Host
			pr.Out.Host = target.Host
			stripProxyCredentials(pr.Out)
			// Grafana itself requires real credentials now (see
			// render.GrafanaObjects' own doc comment) - the browser never
			// sees them, only this hop does, which is what keeps the iframe
			// login-free.
			pr.Out.SetBasicAuth(render.GrafanaAdminUser, adminPassword)
			// pr.Out.URL.Path is left as-is (not stripped of its
			// "/grafana-proxy/<cluster>" prefix) - see doc comment above.
		},
	}
	proxy.ServeHTTP(w, r)
}

// grafanaAdminPassword resolves wc's generated Grafana admin password
// (render.GrafanaAdminSecretName, written by WorkloadClusterReconciler's
// ensureGrafana), caching it for the life of this process - the Secret is
// create-only and never rotated by the operator, so a cached value can never
// go stale short of a manual rotation, which a process restart picks up.
func (s *Server) grafanaAdminPassword(ctx context.Context, wc *benchv1alpha1.WorkloadCluster) (string, error) {
	s.grafanaAdminMu.Lock()
	cached, ok := s.grafanaAdminCache[wc.Name]
	s.grafanaAdminMu.Unlock()
	if ok {
		return cached, nil
	}

	var secret corev1.Secret
	key := types.NamespacedName{Name: render.GrafanaAdminSecretName(wc), Namespace: s.cfg.Namespace}
	if err := s.client.Get(ctx, key, &secret); err != nil {
		return "", fmt.Errorf("read Grafana admin credentials: %w", err)
	}
	password := strings.TrimSpace(string(secret.Data["password"]))
	if password == "" {
		return "", fmt.Errorf("secret %s/%s has no password", key.Namespace, key.Name)
	}

	s.grafanaAdminMu.Lock()
	s.grafanaAdminCache[wc.Name] = password
	s.grafanaAdminMu.Unlock()
	return password, nil
}
