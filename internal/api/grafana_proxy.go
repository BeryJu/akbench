/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package api

import (
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/go-chi/chi/v5"
	"k8s.io/apimachinery/pkg/types"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// grafanaProxy reverse-proxies /grafana-proxy/{cluster}/* straight to that
// WorkloadCluster's own Grafana NodePort (status.grafanaURL, populated by
// WorkloadClusterReconciler's ensureGrafana - see render.GrafanaObjects'
// own doc comment for the full story of why this exists at all: the k8s
// API server's own service-proxy subresource content-rewrites absolute
// HTML paths in a way that breaks Grafana's own, so the browser has to
// reach this route directly instead of through that mechanism).
//
// Deliberately registered outside the /api/v1 route group (see Router,
// alongside /healthz and /readyz): an <iframe src=> can't attach the
// Authorization header s.requireToken demands, so this route carries
// no auth at all. That's consistent with, not a regression from, this
// project's existing dev-scale security posture - Grafana itself already
// runs with anonymous Admin auth, and every other per-workload-cluster
// addon (Prometheus/Pyroscope/Tempo NodePorts) is already reachable
// without credentials by anyone who can reach the node network.
//
// The Director deliberately does NOT strip the "/grafana-proxy/<cluster>"
// prefix from the forwarded request path - the one deliberate difference
// from a typical strip-prefix reverse proxy. Grafana's own
// GF_SERVER_SERVE_FROM_SUB_PATH=true (render.GrafanaObjects) means Grafana
// expects exactly that: a reverse proxy in front of it that preserves the
// sub-path rather than stripping it.
func (s *Server) grafanaProxy(w http.ResponseWriter, r *http.Request) {
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

	target, err := url.Parse(wc.Status.GrafanaURL)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "invalid recorded Grafana URL: "+err.Error())
		return
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
			// req.URL.Path is left as-is (not stripped of its
			// "/grafana-proxy/<cluster>" prefix) - see doc comment above.
		},
	}
	proxy.ServeHTTP(w, r)
}
