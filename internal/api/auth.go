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
	"crypto/subtle"
	"net/http"
	"strings"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// requireToken enforces the caller's credentials: either the static bearer
// token the UI can be configured with, or an OIDC-issued JWT (oidc.go) when
// an issuer is configured. This is the only thing between the public
// internet and the CRDs (and, via shell.go, a cluster-admin shell), so the
// static comparison runs in constant time and everything fails closed.
func (s *Server) requireToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Token == "" && s.cfg.OIDC == nil {
			// Only reachable in tests - cmd/apiserver refuses to start
			// without either.
			next.ServeHTTP(w, r)
			return
		}
		if presented, ok := bearerToken(r); ok && s.authenticate(r, presented) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="authentik-bench"`)
		writeError(w, r, http.StatusUnauthorized, "missing or invalid bearer token")
	})
}

func (s *Server) authenticate(r *http.Request, presented string) bool {
	if s.cfg.Token != "" && subtle.ConstantTimeCompare([]byte(presented), []byte(s.cfg.Token)) == 1 {
		return true
	}
	if s.cfg.OIDC == nil {
		return false
	}
	if err := s.cfg.OIDC.Verify(r.Context(), presented); err != nil {
		logf.FromContext(r.Context()).V(1).Info("Rejected bearer token", "reason", err.Error())
		return false
	}
	return true
}

// tokenHeader is a fallback to Authorization: a request proxied through
// Headlamp's own ApiProxy (see web/headlamp-plugin) arrives with
// Authorization already carrying Headlamp's own Kubernetes bearer token -
// verified live, that header is overwritten by the proxy layer, not passed
// through - so the token has nowhere else to go but a header of our own.
const tokenHeader = "X-Bench-Api-Token"

func bearerToken(r *http.Request) (string, bool) {
	if v := r.Header.Get(tokenHeader); v != "" {
		return v, true
	}
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	return header[len(prefix):], true
}
