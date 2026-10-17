package api

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// MCPAuthConfig puts /api/v1/mcp behind Enterprise-Managed Authorization
// (MCP SEP-990) instead of the API's own bearer token, with akbench as its
// own OAuth authorization server.
//
// The MCP client signs the user in at the enterprise IdP and exchanges the
// ID token there for an ID-JAG (Identity Assertion JWT Authorization Grant,
// RFC 8693 token exchange) whose audience is akbench's issuer. It presents
// the ID-JAG at akbench's token endpoint (RFC 7523 JWT bearer grant), which
// verifies it against IdPIssuer and answers with a short-lived access token
// for the MCP endpoint.
//
// Once this is set, the MCP endpoint accepts these tokens only: neither the
// static API token nor a web UI OIDC token works on it, and an MCP token
// (signed with a key only this process has) doesn't work anywhere else.
type MCPAuthConfig struct {
	// Resource is the MCP endpoint's canonical public URL (e.g.
	// https://bench.example.com/api/v1/mcp), its RFC 9728 resource
	// identifier. Its origin is akbench's issuer, the ID-JAG audience.
	Resource string
	// IdPIssuer is the enterprise IdP whose ID-JAGs are trusted, discovered
	// through its OpenID configuration for the JWKS. For authentik, the
	// issuer of the provider that mints them, e.g.
	// https://authentik.company/application/o/akbench-ema-idp/.
	IdPIssuer string
	// ClientID and ClientSecret are the one MCP client's credentials at
	// akbench's token endpoint; ID-JAGs must name ClientID.
	ClientID     string
	ClientSecret string
	// Scopes every access token must carry, also advertised in the metadata.
	// Optional.
	Scopes []string
}

const (
	grantTypeJWTBearer = "urn:ietf:params:oauth:grant-type:jwt-bearer"
	// idJAGType is the JWT typ header an ID-JAG must carry.
	idJAGType = "oauth-id-jag+jwt"
	// mcpTokenPath is akbench's token endpoint, relative to its issuer.
	mcpTokenPath = "/oauth/token"
	// mcpAccessTokenTTL is how long a granted access token is valid.
	mcpAccessTokenTTL = time.Hour
	// wellKnownProtectedResource is RFC 9728's metadata path prefix; the
	// resource's own path is appended to it.
	wellKnownProtectedResource = "/.well-known/oauth-protected-resource"
	// wellKnownAuthServer is RFC 8414's metadata path. The issuer has no
	// path, so nothing is inserted.
	wellKnownAuthServer = "/.well-known/oauth-authorization-server"
)

// mcpAuthSigningAlgs are the asymmetric JWS algorithms an ID-JAG may be
// signed with. go-oidc defaults to RS256 alone when the IdP's discovery
// document doesn't list any.
var mcpAuthSigningAlgs = []string{
	oidc.RS256, oidc.RS384, oidc.RS512, oidc.ES256, oidc.ES384, oidc.ES512, oidc.PS256, oidc.PS384, oidc.PS512, oidc.EdDSA,
}

// issuer is akbench's authorization server issuer: the resource's origin.
func (c *MCPAuthConfig) issuer() string {
	u, _ := url.Parse(c.Resource)
	return u.Scheme + "://" + u.Host
}

// metadataURL is where Resource's RFC 9728 metadata is served, and
// metadataPath that URL's path on this server.
func (c *MCPAuthConfig) metadataURL() (metadataURL, metadataPath string, err error) {
	u, err := url.Parse(c.Resource)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", "", fmt.Errorf("MCP resource %q is not an absolute URL", c.Resource)
	}
	metadataPath = wellKnownProtectedResource + strings.TrimSuffix(u.Path, "/")
	return u.Scheme + "://" + u.Host + metadataPath, metadataPath, nil
}

// Validate reports a configuration the MCP endpoint can't run with.
func (c *MCPAuthConfig) Validate() error {
	if c.IdPIssuer == "" || c.ClientID == "" || c.ClientSecret == "" {
		return errors.New("MCP authorization needs a trusted IdP issuer and a client ID and secret")
	}
	_, _, err := c.metadataURL()
	return err
}

// mcpAuth is akbench's authorization server for MCPAuthConfig: it grants
// access tokens for ID-JAGs and checks them on the MCP endpoint.
type mcpAuth struct {
	cfg *MCPAuthConfig
	// key signs access tokens. Only this process verifies them, so there's
	// no JWKS to publish.
	// ponytail: per-process key and jti cache, so a restart logs MCP clients
	// out and replicas don't share either; persist both in a Secret if the
	// apiserver ever runs more than one replica.
	key []byte

	// mu guards idp, discovered lazily and retried until that succeeds since
	// the IdP is often an authentik that's still starting, and seen, the
	// IdP-issued jti values not yet expired, against ID-JAG replay.
	mu   sync.Mutex
	idp  *oidc.IDTokenVerifier
	seen map[string]time.Time
}

func newMCPAuth(cfg *MCPAuthConfig) *mcpAuth {
	return &mcpAuth{cfg: cfg, key: newSessionKey(), seen: map[string]time.Time{}}
}

func (a *mcpAuth) idpVerifier(ctx context.Context) (*oidc.IDTokenVerifier, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.idp != nil {
		return a.idp, nil
	}
	provider, err := oidc.NewProvider(context.WithoutCancel(ctx), a.cfg.IdPIssuer)
	if err != nil {
		return nil, fmt.Errorf("discover the enterprise IdP: %w", err)
	}
	a.idp = provider.Verifier(&oidc.Config{ClientID: a.cfg.issuer(), SupportedSigningAlgs: mcpAuthSigningAlgs})
	return a.idp, nil
}

// firstUse records jti until exp and reports whether it was new.
func (a *mcpAuth) firstUse(jti string, exp time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for k, until := range a.seen {
		if until.Before(now) {
			delete(a.seen, k)
		}
	}
	if _, ok := a.seen[jti]; ok {
		return false
	}
	a.seen[jti] = exp
	return true
}

// mcpActor is a JWT act claim (RFC 8693): the agent acting for sub.
type mcpActor struct {
	Subject string `json:"sub"`
}

// mcpGrant is what a verified ID-JAG grants.
type mcpGrant struct {
	subject string
	actor   *mcpActor
	scopes  []string
}

// verifyIDJAG checks an ID-JAG per draft-ietf-oauth-identity-assertion-authz-grant
// and SEP-990: signed by the trusted IdP, for this issuer, the authenticated
// client and the MCP resource, unexpired and not seen before.
func (a *mcpAuth) verifyIDJAG(ctx context.Context, raw string) (*mcpGrant, error) {
	if typ := jwtType(raw); typ != idJAGType {
		return nil, fmt.Errorf("typ header is %q, not %s", typ, idJAGType)
	}
	verifier, err := a.idpVerifier(ctx)
	if err != nil {
		return nil, err
	}
	// Signature, iss, aud and exp.
	token, err := verifier.Verify(ctx, raw)
	if err != nil {
		return nil, err
	}
	var claims struct {
		ClientID string       `json:"client_id"`
		Resource jwt.Audience `json:"resource"`
		Scope    *string      `json:"scope"`
		JTI      string       `json:"jti"`
		Actor    *mcpActor    `json:"act"`
	}
	if err := token.Claims(&claims); err != nil {
		return nil, err
	}
	switch {
	case claims.ClientID != a.cfg.ClientID:
		return nil, fmt.Errorf("client_id %q is not this client", claims.ClientID)
	case !claims.Resource.Contains(a.cfg.Resource):
		return nil, fmt.Errorf("resource %v is not %s", []string(claims.Resource), a.cfg.Resource)
	case token.Subject == "":
		return nil, errors.New("no sub")
	case token.IssuedAt.IsZero() || token.IssuedAt.After(time.Now().Add(time.Minute)):
		return nil, errors.New("iat missing or in the future")
	case claims.JTI == "":
		return nil, errors.New("no jti")
	case !a.firstUse(claims.JTI, token.Expiry):
		return nil, errors.New("already used")
	}
	grant := &mcpGrant{subject: token.Subject, actor: claims.Actor, scopes: a.cfg.Scopes}
	if claims.Scope != nil {
		grant.scopes = strings.Fields(*claims.Scope)
	}
	return grant, nil
}

// jwtType returns a compact JWS's typ header, lowercased and without the
// optional application/ prefix (RFC 7515 4.1.9), or "" if there's none.
func jwtType(raw string) string {
	head, _, _ := strings.Cut(raw, ".")
	b, err := base64.RawURLEncoding.DecodeString(head)
	var header struct {
		Type string `json:"typ"`
	}
	if err != nil || json.Unmarshal(b, &header) != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(header.Type), "application/")
}

// mcpTokenClaims are an access token's own claims beyond the registered ones.
type mcpTokenClaims struct {
	ClientID string    `json:"client_id"`
	Scope    string    `json:"scope,omitempty"`
	Actor    *mcpActor `json:"act,omitempty"`
}

// token is the RFC 7523 JWT bearer grant (only), with client_secret_post or
// client_secret_basic client authentication.
func (a *mcpAuth) token(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, r, http.StatusBadRequest, "invalid_request", "malformed form body")
		return
	}
	clientID, secret, ok := r.BasicAuth()
	if !ok {
		clientID, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	if clientID != a.cfg.ClientID || subtle.ConstantTimeCompare([]byte(secret), []byte(a.cfg.ClientSecret)) != 1 {
		w.Header().Set("WWW-Authenticate", `Basic realm="akbench"`)
		writeOAuthError(w, r, http.StatusUnauthorized, "invalid_client", "unknown client or wrong secret")
		return
	}
	if gt := r.PostForm.Get("grant_type"); gt != grantTypeJWTBearer {
		writeOAuthError(w, r, http.StatusBadRequest, "unsupported_grant_type", "only "+grantTypeJWTBearer+" is supported")
		return
	}
	grant, err := a.verifyIDJAG(r.Context(), r.PostForm.Get("assertion"))
	if err != nil {
		writeOAuthError(w, r, http.StatusBadRequest, "invalid_grant", "ID-JAG rejected: "+err.Error())
		return
	}
	// Never more than the ID-JAG grants.
	scopes := grant.scopes
	if requested := strings.Fields(r.PostForm.Get("scope")); len(requested) > 0 {
		scopes = slices.DeleteFunc(requested, func(s string) bool { return !slices.Contains(grant.scopes, s) })
	}

	now := time.Now()
	expiry := now.Add(mcpAccessTokenTTL)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: a.key},
		(&jose.SignerOptions{}).WithType("at+jwt"))
	if err != nil {
		writeOAuthError(w, r, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	accessToken, err := jwt.Signed(signer).Claims(jwt.Claims{
		Issuer: a.cfg.issuer(), Subject: grant.subject, Audience: jwt.Audience{a.cfg.Resource},
		IssuedAt: jwt.NewNumericDate(now), Expiry: jwt.NewNumericDate(expiry),
	}).Claims(mcpTokenClaims{ClientID: clientID, Scope: strings.Join(scopes, " "), Actor: grant.actor}).Serialize()
	if err != nil {
		writeOAuthError(w, r, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	logf.FromContext(r.Context()).Info("Granted MCP access token", "subject", grant.subject, "client", clientID)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, r, http.StatusOK, map[string]any{
		"access_token": accessToken, "token_type": "Bearer",
		"expires_in": int(time.Until(expiry).Seconds()), "scope": strings.Join(scopes, " "),
	})
}

func writeOAuthError(w http.ResponseWriter, r *http.Request, status int, code, description string) {
	writeJSON(w, r, status, map[string]string{"error": code, "error_description": description})
}

// verify is an auth.TokenVerifier for the access tokens token grants.
func (a *mcpAuth) verify(_ context.Context, rawToken string, _ *http.Request) (*auth.TokenInfo, error) {
	token, err := jwt.ParseSigned(rawToken, []jose.SignatureAlgorithm{jose.HS256})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", auth.ErrInvalidToken, err)
	}
	var std jwt.Claims
	var claims mcpTokenClaims
	if err := token.Claims(a.key, &std, &claims); err != nil {
		return nil, fmt.Errorf("%w: %v", auth.ErrInvalidToken, err)
	}
	if err := std.Validate(jwt.Expected{
		Issuer: a.cfg.issuer(), AnyAudience: jwt.Audience{a.cfg.Resource}, Time: time.Now(),
	}); err != nil {
		return nil, fmt.Errorf("%w: %v", auth.ErrInvalidToken, err)
	}
	via := claims.ClientID
	if claims.Actor != nil && claims.Actor.Subject != "" {
		via = claims.Actor.Subject
	}
	return &auth.TokenInfo{
		Scopes:     strings.Fields(claims.Scope),
		Expiration: std.Expiry.Time(),
		UserID:     std.Subject,
		Extra:      map[string]any{"via": via},
	}, nil
}

// middleware authenticates /api/v1/mcp, answering an unauthenticated request
// with the WWW-Authenticate challenge that points the client at the
// resource metadata.
func (a *mcpAuth) middleware() func(http.Handler) http.Handler {
	// Validated by New's caller (cmd/apiserver); a bad URL here only loses
	// the challenge's resource_metadata parameter.
	metadataURL, _, _ := a.cfg.metadataURL()
	requireToken := auth.RequireBearerToken(a.verify,
		&auth.RequireBearerTokenOptions{ResourceMetadataURL: metadataURL, Scopes: a.cfg.Scopes})
	return func(next http.Handler) http.Handler {
		return requireToken(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			who := "mcp"
			if info := auth.TokenInfoFromContext(r.Context()); info != nil {
				who = info.UserID
				if via, _ := info.Extra["via"].(string); via != "" {
					who += " via " + via
				}
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, who)))
		}))
	}
}

// resourceMetadata serves RFC 9728 protected resource metadata, which is
// how an MCP client learns the authorization server to request an access
// token from (the audience of its ID-JAG token exchange): akbench itself.
func (a *mcpAuth) resourceMetadata() http.Handler {
	return auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
		Resource:               a.cfg.Resource,
		AuthorizationServers:   []string{a.cfg.issuer()},
		ScopesSupported:        a.cfg.Scopes,
		BearerMethodsSupported: []string{"header"},
		ResourceName:           "authentik-bench",
	})
}

// serverMetadata serves RFC 8414 authorization server metadata.
func (a *mcpAuth) serverMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r, http.StatusOK, &oauthex.AuthServerMeta{
		Issuer:                            a.cfg.issuer(),
		TokenEndpoint:                     a.cfg.issuer() + mcpTokenPath,
		GrantTypesSupported:               []string{grantTypeJWTBearer},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post", "client_secret_basic"},
		ScopesSupported:                   a.cfg.Scopes,
		ResponseTypesSupported:            []string{},
		// There's no authorization endpoint, but the Go MCP SDK refuses
		// metadata without a PKCE method.
		CodeChallengeMethodsSupported: []string{"S256"},
	})
}
