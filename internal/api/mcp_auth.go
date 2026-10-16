package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// MCPAuthConfig puts /api/v1/mcp behind Enterprise-Managed Authorization
// (MCP SEP-990) instead of the API's own bearer token.
//
// In that flow akbench is a plain OAuth 2.0 resource server. The MCP client
// signs the user in at the enterprise IdP, exchanges the ID token there for
// an ID-JAG (Identity Assertion JWT Authorization Grant, RFC 8693 token
// exchange), and presents the ID-JAG to the authorization server named
// here (an RFC 7523 JWT bearer grant). That server decides whether to trust
// the IdP's assertion and mints the access token akbench checks: signed by
// Issuer, with Audience in its aud claim. Trust in the IdP and the ID-JAG
// itself are never seen here.
//
// Once this is set, the MCP endpoint accepts these tokens only: neither the
// static API token nor a web UI OIDC token works on it, and an MCP token
// (whose audience is not the UI's client ID) doesn't work anywhere else.
type MCPAuthConfig struct {
	// Issuer is the authorization server's issuer URL, discovered through
	// its OpenID configuration for the JWKS. For authentik, the issuer of
	// the provider that accepts ID-JAG grants for akbench.
	Issuer string
	// Resource is the MCP endpoint's canonical public URL (e.g.
	// https://bench.example.com/api/v1/mcp), its RFC 9728 resource
	// identifier. Clients pass it as the resource parameter of the token
	// exchange.
	Resource string
	// Audience is the aud value every accepted access token must carry.
	// Defaults to Resource (RFC 8707); set it to the authorization server's
	// client ID if that is what it puts in aud instead.
	Audience string
	// Scopes every access token must carry, also advertised in the resource
	// metadata. Optional.
	Scopes []string
}

// mcpAuthSigningAlgs are the asymmetric JWS algorithms an MCP access token
// may be signed with. go-oidc defaults to RS256 alone when the issuer's
// discovery document doesn't list any.
var mcpAuthSigningAlgs = []string{
	oidc.RS256, oidc.RS384, oidc.RS512, oidc.ES256, oidc.ES384, oidc.ES512, oidc.PS256, oidc.PS384, oidc.PS512, oidc.EdDSA,
}

// wellKnownProtectedResource is RFC 9728's metadata path prefix; the
// resource's own path is appended to it.
const wellKnownProtectedResource = "/.well-known/oauth-protected-resource"

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

func (c *MCPAuthConfig) audience() string {
	if c.Audience != "" {
		return c.Audience
	}
	return c.Resource
}

// Validate reports a configuration the MCP endpoint can't run with. ui is
// the web UI's OIDC login (zero when disabled): an MCP access token from the
// same issuer with the same audience would also pass the UI's verifier and
// open the whole API, so that combination is refused.
func (c *MCPAuthConfig) Validate(ui OIDCConfig) error {
	if c.Issuer == "" || c.Resource == "" {
		return fmt.Errorf("MCP authorization needs both an issuer and a resource URL")
	}
	if _, _, err := c.metadataURL(); err != nil {
		return err
	}
	if ui.ClientID != "" && c.audience() == ui.ClientID &&
		strings.TrimSuffix(c.Issuer, "/") == strings.TrimSuffix(ui.Issuer, "/") {
		return fmt.Errorf("the MCP audience %q is the web UI's OIDC client ID at the same issuer, "+
			"so MCP and UI tokens would be interchangeable; have the authorization server issue MCP tokens "+
			"for the resource URL, or use a separate provider", ui.ClientID)
	}
	return nil
}

// mcpTokenVerifier checks access tokens for MCPAuthConfig. Like
// oidcVerifier it discovers the issuer lazily and retries until that
// succeeds, since the authorization server is often an authentik that's
// still starting.
type mcpTokenVerifier struct {
	cfg *MCPAuthConfig

	mu       sync.Mutex
	verifier *oidc.IDTokenVerifier
}

func (v *mcpTokenVerifier) get(ctx context.Context) (*oidc.IDTokenVerifier, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.verifier != nil {
		return v.verifier, nil
	}
	provider, err := oidc.NewProvider(context.WithoutCancel(ctx), v.cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discover MCP authorization server: %w", err)
	}
	v.verifier = provider.Verifier(&oidc.Config{ClientID: v.cfg.audience(), SupportedSigningAlgs: mcpAuthSigningAlgs})
	return v.verifier, nil
}

// verify is an auth.TokenVerifier: signature, issuer, audience and expiry
// via go-oidc, then the token's scopes for auth.RequireBearerToken to check.
func (v *mcpTokenVerifier) verify(ctx context.Context, rawToken string, _ *http.Request) (*auth.TokenInfo, error) {
	verifier, err := v.get(ctx)
	if err != nil {
		return nil, err
	}
	token, err := verifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", auth.ErrInvalidToken, err)
	}
	var claims struct {
		Scope             string `json:"scope"`
		PreferredUsername string `json:"preferred_username"`
		Email             string `json:"email"`
		ClientID          string `json:"client_id"`
		AuthorizedParty   string `json:"azp"`
	}
	if err := token.Claims(&claims); err != nil {
		return nil, fmt.Errorf("%w: decode claims: %v", auth.ErrInvalidToken, err)
	}
	who := token.Subject
	for _, name := range []string{claims.PreferredUsername, claims.Email} {
		if name != "" {
			who = name
			break
		}
	}
	client := claims.ClientID
	if client == "" {
		client = claims.AuthorizedParty
	}
	return &auth.TokenInfo{
		Scopes:     strings.Fields(claims.Scope),
		Expiration: token.Expiry,
		UserID:     who,
		Extra:      map[string]any{"client": client},
	}, nil
}

// mcpAuthMiddleware authenticates /api/v1/mcp per MCPAuthConfig, answering
// an unauthenticated request with the WWW-Authenticate challenge that points
// the client at the resource metadata.
func (s *Server) mcpAuthMiddleware() func(http.Handler) http.Handler {
	cfg := s.cfg.MCPAuth
	// Validated by New's caller (cmd/apiserver); a bad URL here only loses
	// the challenge's resource_metadata parameter.
	metadataURL, _, _ := cfg.metadataURL()
	requireToken := auth.RequireBearerToken((&mcpTokenVerifier{cfg: cfg}).verify,
		&auth.RequireBearerTokenOptions{ResourceMetadataURL: metadataURL, Scopes: cfg.Scopes})
	return func(next http.Handler) http.Handler {
		return requireToken(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			who := "mcp"
			if info := auth.TokenInfoFromContext(r.Context()); info != nil {
				who = info.UserID
				if client, _ := info.Extra["client"].(string); client != "" {
					who += " via " + client
				}
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, who)))
		}))
	}
}

// mcpResourceMetadata serves RFC 9728 protected resource metadata, which is
// how an MCP client learns which authorization server to request an access
// token from (the audience of its ID-JAG token exchange).
func (s *Server) mcpResourceMetadata() http.Handler {
	cfg := s.cfg.MCPAuth
	return auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
		Resource:               cfg.Resource,
		AuthorizationServers:   []string{cfg.Issuer},
		ScopesSupported:        cfg.Scopes,
		BearerMethodsSupported: []string{"header"},
		ResourceName:           "authentik-bench",
	})
}
