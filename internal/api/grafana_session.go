package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The Grafana proxy authenticates with a cookie rather than the bearer
// token every /api/v1 route uses: the UI embeds Grafana in an <iframe>, and
// neither the iframe's own request nor any request Grafana's frontend makes
// from inside it can carry an Authorization header - but a same-origin
// cookie rides along with all of them, WebSockets (Grafana Live) included.
//
// The cookie is minted by an authenticated POST /api/v1/grafana/session, so
// holding one proves the browser recently held a valid API credential. It's
// a signed expiry timestamp, nothing more - no server-side session state,
// same as the rest of this server - signed with a key generated per process
// (newGrafanaSessionKey), so a restart simply invalidates every cookie and
// the UI mints a fresh one on its next Grafana page load.
const (
	grafanaSessionCookie = "akbench_grafana"
	// Short, and the UI re-mints well before expiry while Grafana is open -
	// an OIDC access token behind the original mint may itself expire, and a
	// stale cookie shouldn't outlive it by much.
	grafanaSessionTTL = time.Hour
	// grafanaProxyPathPrefix scopes the cookie to the proxy alone, so it's
	// never sent to (or mistaken for a credential by) the /api/v1 routes.
	grafanaProxyPathPrefix = "/grafana-proxy/"
)

func newGrafanaSessionKey() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		// crypto/rand never fails on supported platforms; refusing to run
		// beats signing with a predictable key.
		panic("generate Grafana session key: " + err.Error())
	}
	return key
}

func (s *Server) signGrafanaSession(expires time.Time) string {
	payload := strconv.FormatInt(expires.Unix(), 10)
	mac := hmac.New(sha256.New, s.grafanaKey)
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// validGrafanaSession reports whether value is an unexpired cookie this
// process signed.
func (s *Server) validGrafanaSession(value string, now time.Time) bool {
	payload, signature, ok := strings.Cut(value, ".")
	if !ok {
		return false
	}
	presented, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, s.grafanaKey)
	mac.Write([]byte(payload))
	if !hmac.Equal(presented, mac.Sum(nil)) {
		return false
	}
	expires, err := strconv.ParseInt(payload, 10, 64)
	return err == nil && now.Unix() < expires
}

// createGrafanaSession sets the proxy's session cookie. Mounted behind
// requireToken, which is the whole point.
func (s *Server) createGrafanaSession(w http.ResponseWriter, r *http.Request) {
	expires := time.Now().Add(grafanaSessionTTL)
	http.SetCookie(w, &http.Cookie{
		Name:     grafanaSessionCookie,
		Value:    s.signGrafanaSession(expires),
		Path:     grafanaProxyPathPrefix,
		Expires:  expires,
		HttpOnly: true,
		Secure:   r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
		SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, r, http.StatusOK, map[string]string{expiresAtKey: expires.UTC().Format(time.RFC3339)})
}

// grafanaAuthorized accepts either the session cookie (the iframe) or a
// bearer token (scripts and tools that can send one).
func (s *Server) grafanaAuthorized(r *http.Request) bool {
	if s.cfg.Token == "" && s.cfg.OIDC == nil {
		// Same test-only escape hatch as requireToken.
		return true
	}
	if cookie, err := r.Cookie(grafanaSessionCookie); err == nil && s.validGrafanaSession(cookie.Value, time.Now()) {
		return true
	}
	presented, ok := bearerToken(r)
	return ok && s.authenticate(r, presented)
}

// stripProxyCredentials removes this server's own credentials from a request
// before it's forwarded to Grafana - they mean nothing to Grafana, and an
// API token has no business reaching a workload cluster.
func stripProxyCredentials(req *http.Request) {
	req.Header.Del("Authorization")
	cookies := req.Cookies()
	req.Header.Del("Cookie")
	for _, c := range cookies {
		if c.Name != grafanaSessionCookie {
			req.AddCookie(c)
		}
	}
}
