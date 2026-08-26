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
)

// requireToken enforces the static bearer token the UI is configured with.
// Real SSO (OIDC via authentik, dogfooding the product under test) is a
// later milestone; until then this is the only thing between the public
// internet and the CRDs, so it compares in constant time and fails closed.
func (s *Server) requireToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Token == "" {
			// Only reachable in tests - cmd/apiserver refuses to start
			// without a token.
			next.ServeHTTP(w, r)
			return
		}
		presented, ok := bearerToken(r)
		if !ok || subtle.ConstantTimeCompare([]byte(presented), []byte(s.cfg.Token)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="authentik-bench"`)
			writeError(w, r, http.StatusUnauthorized, "missing or invalid bearer token")
			return
		}
		next.ServeHTTP(w, r)
	})
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
