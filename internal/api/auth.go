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

func bearerToken(r *http.Request) (string, bool) {
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	return header[len(prefix):], true
}
