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
// read-only PromQL proxy, a per-workload-cluster Grafana reverse proxy
// (grafana_proxy.go), an in-browser kubectl shell (shell.go) and the web UI's
// own static assets (static.go).
//
// The REST API deliberately talks to nothing but the management cluster -
// all multi-cluster logic lives in the operator. The shell is the one
// exception: it hands the user the operator's merged kubeconfig, which is
// the point of it.
package api

import (
	"io/fs"
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

	// Token is a shared static bearer token requests may present. With both
	// Token and OIDC unset authentication is disabled, which is only ever
	// valid in tests - New's caller is responsible for refusing to start
	// that way.
	Token string

	// OIDC verifies OIDC-issued bearer tokens (oidc.go); nil disables OIDC
	// login. OIDCConfig is what the SPA is told to log in with.
	OIDC       TokenVerifier
	OIDCConfig OIDCConfig

	// Shell enables the kubectl shell (shell.go); nil disables it.
	Shell *ShellConfig

	// UI is the built web UI (web/ui's dist/) served at every path the API
	// doesn't claim; nil serves no UI.
	UI fs.FS

	// PrometheusURL is the base URL of the central Prometheus the PromQL
	// proxy forwards to (e.g. http://prometheus.monitoring:9090).
	PrometheusURL string

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
	shell  *shellState
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
	return &Server{client: c, cfg: cfg, http: httpClient, shell: newShellState()}
}

// probeStatusField is the JSON key /healthz and /readyz answer under -
// unrelated to (and coincidentally the same string as) every k8s object's
// own status field elsewhere in this package, kept named just for goconst's
// sake.
const probeStatusField = "status"

// Router builds the full HTTP surface. Everything under /api/v1 requires a
// bearer token except the auth discovery endpoint the login screen needs and
// the shell's WebSocket, which authenticates with a single-use ticket minted
// by an authenticated request (see shell.go). /healthz, /readyz and the UI's
// static assets are public.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer)

	probeOK := func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, r, http.StatusOK, map[string]string{probeStatusField: "ok"})
	}
	r.Get("/healthz", probeOK)
	r.Get("/readyz", probeOK)

	// Unauthenticated by design - an <iframe src=> can't carry a bearer
	// token. See grafana_proxy.go's own doc comment for the full rationale.
	r.HandleFunc("/grafana-proxy/{cluster}/*", s.grafanaProxy)

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/auth/config", s.authConfig)
		r.Get("/shell/connect", s.connectShell)

		r.Group(func(r chi.Router) {
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

			r.Get("/deployments/{name}/extra-values", s.getExtraValues)
			r.Put("/deployments/{name}/extra-values", s.putExtraValues)

			r.Post("/testcases/{name}/runs", s.createRun)

			r.Get("/metrics/query", s.proxyQuery)
			r.Get("/metrics/compare", s.compare)
			r.Get("/metrics/range", s.metricsRange)

			r.Get("/shell/contexts", s.listShellContexts)
			r.Post("/shell/sessions", s.createShellSession)
		})

		// Without this, an unknown /api/v1 path would fall through to the
		// SPA's index.html and answer 200 with HTML.
		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			writeError(w, r, http.StatusNotFound, "no such API route")
		})
	})

	if s.cfg.UI != nil {
		spa := spaHandler(s.cfg.UI)
		r.Get("/*", spa.ServeHTTP)
		r.Head("/*", spa.ServeHTTP)
	}

	return r
}
