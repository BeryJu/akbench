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

// Package api implements the benchmark web UI's backend: a thin REST/JSON
// layer over the management cluster's bench.goauthentik.io CRDs, plus a
// read-only PromQL proxy and Grafana embed-URL builder.
//
// It deliberately talks to nothing but the management cluster - all
// multi-cluster logic lives in the operator, so the UI never needs a
// workload cluster's credentials.
package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Config is everything the backend needs to serve the UI.
type Config struct {
	// Namespace is the single namespace bench CRs are read from and written
	// to. The UI is not namespace-aware by design.
	Namespace string

	// Token is the shared bearer token every request must present. Empty
	// disables authentication and is only ever valid in tests - New's caller
	// is responsible for refusing to start without one.
	Token string

	// PrometheusURL is the base URL of the central Prometheus the PromQL
	// proxy forwards to (e.g. http://prometheus.monitoring:9090).
	PrometheusURL string

	// GrafanaURL is the browser-reachable Grafana base URL used to build
	// embed URLs. Empty disables the embed endpoint.
	GrafanaURL string

	// CAPIAvailable reports whether cluster.x-k8s.io Clusters can be listed.
	// False (M3's single-cluster case) makes /clusters report just the
	// management cluster itself.
	CAPIAvailable bool

	// HTTPClient is used for Prometheus calls. Defaults to a 30s client.
	HTTPClient *http.Client
}

// Server holds the wiring shared by all handlers.
type Server struct {
	client client.Client
	cfg    Config
	http   *http.Client
}

// New builds a Server. The client must be backed by the management cluster
// and its scheme must know the bench.goauthentik.io types.
func New(c client.Client, cfg Config) *Server {
	if cfg.Namespace == "" {
		cfg.Namespace = "default"
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Server{client: c, cfg: cfg, http: httpClient}
}

// Router builds the full HTTP surface. Everything under /api/v1 requires the
// bearer token; /healthz and /readyz do not, so probes stay credential-free.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, r, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, r, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(s.requireToken)

		r.Get("/clusters", s.listClusters)
		r.Post("/clusters", s.registerCluster)
		r.Get("/clusters/{name}", s.getCluster)
		r.Delete("/clusters/{name}", s.deleteCluster)

		for path, kind := range resourceKinds {
			r.Get("/"+path, s.listResource(kind))
			r.Post("/"+path, s.createResource(kind))
			r.Get("/"+path+"/{name}", s.getResource(kind))
			r.Put("/"+path+"/{name}", s.replaceResource(kind))
			r.Delete("/"+path+"/{name}", s.deleteResource(kind))
			r.Get("/"+path+"/{name}/status", s.getResourceStatus(kind))
		}

		r.Get("/topologies/{name}/extra-values", s.getExtraValues)
		r.Put("/topologies/{name}/extra-values", s.putExtraValues)

		r.Post("/testcases/{name}/runs", s.createRun)

		r.Get("/metrics/query", s.proxyQuery)
		r.Get("/metrics/compare", s.compare)
		r.Get("/metrics/range", s.metricsRange)
		r.Get("/grafana/embed-url", s.embedURL)
	})

	return r
}
