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
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
)

// OIDCConfig configures login through an OpenID Connect provider - this
// project benchmarks an IdP, so it dogfoods one (typically an authentik
// OAuth2/OpenID provider).
//
// The SPA is a public client doing authorization code + PKCE in the browser
// and sends the resulting access token as a plain Bearer token; the backend
// only ever verifies JWTs, it never handles a code exchange, a client
// secret or a session cookie.
type OIDCConfig struct {
	// Issuer is the provider's issuer URL, e.g.
	// https://authentik.example.com/application/o/authentik-bench/.
	Issuer string
	// ClientID is the SPA's client ID, which is also the audience every
	// accepted token must carry.
	ClientID string
	// Scopes the SPA requests. Defaults to openid, profile, email.
	Scopes []string
	// RequiredGroup, when set, must appear in the token's groups claim.
	// Logging in grants a cluster-admin shell (shell.go), so being able to
	// authenticate against the provider at all should not be enough unless
	// the provider itself restricts who can use the application.
	RequiredGroup string
}

// TokenVerifier checks a presented bearer token.
type TokenVerifier interface {
	Verify(ctx context.Context, rawToken string) error
}

// authConfigResponse is what GET /api/v1/auth/config returns, so the SPA can
// decide how to log in without any build-time configuration.
type authConfigResponse struct {
	OIDC *authConfigOIDC `json:"oidc,omitempty"`
}

type authConfigOIDC struct {
	Issuer   string   `json:"issuer"`
	ClientID string   `json:"clientId"`
	Scopes   []string `json:"scopes"`
}

func (s *Server) authConfig(w http.ResponseWriter, r *http.Request) {
	resp := authConfigResponse{}
	if s.cfg.OIDC != nil && s.cfg.OIDCConfig.Issuer != "" {
		resp.OIDC = &authConfigOIDC{
			Issuer:   s.cfg.OIDCConfig.Issuer,
			ClientID: s.cfg.OIDCConfig.ClientID,
			Scopes:   s.cfg.OIDCConfig.scopes(),
		}
	}
	writeJSON(w, r, http.StatusOK, resp)
}

func (c OIDCConfig) scopes() []string {
	if len(c.Scopes) == 0 {
		return []string{oidc.ScopeOpenID, "profile", "email"}
	}
	return c.Scopes
}

// oidcVerifier discovers the provider lazily, on first use, and keeps
// retrying until discovery succeeds: the IdP is often the very authentik
// install being benchmarked, and the backend refusing to start (or staying
// broken until restarted) because it happened to be down at boot would be
// the wrong failure mode. The static token keeps working meanwhile.
type oidcVerifier struct {
	cfg OIDCConfig

	mu       sync.Mutex
	verifier *oidc.IDTokenVerifier
}

// NewOIDCVerifier builds a TokenVerifier for cfg. It does no network I/O.
func NewOIDCVerifier(cfg OIDCConfig) (TokenVerifier, error) {
	if cfg.Issuer == "" || cfg.ClientID == "" {
		return nil, errors.New("OIDC needs both an issuer and a client ID")
	}
	return &oidcVerifier{cfg: cfg}, nil
}

func (v *oidcVerifier) get(ctx context.Context) (*oidc.IDTokenVerifier, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.verifier != nil {
		return v.verifier, nil
	}
	// The provider (and its JWKS key set) outlives this request, so it must
	// not be bound to the request's context.
	provider, err := oidc.NewProvider(context.WithoutCancel(ctx), v.cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC provider: %w", err)
	}
	v.verifier = provider.Verifier(&oidc.Config{ClientID: v.cfg.ClientID})
	return v.verifier, nil
}

// Verify checks signature, issuer, audience and expiry. go-oidc's
// IDTokenVerifier applies to any JWT with those claims, which includes the
// access tokens authentik issues (aud is the client ID there too).
func (v *oidcVerifier) Verify(ctx context.Context, rawToken string) error {
	verifier, err := v.get(ctx)
	if err != nil {
		return err
	}
	token, err := verifier.Verify(ctx, rawToken)
	if err != nil {
		return err
	}
	if v.cfg.RequiredGroup == "" {
		return nil
	}
	var claims struct {
		Groups []string `json:"groups"`
	}
	if err := token.Claims(&claims); err != nil {
		return fmt.Errorf("decode claims: %w", err)
	}
	if !slices.Contains(claims.Groups, v.cfg.RequiredGroup) {
		return fmt.Errorf("token lacks required group %q", v.cfg.RequiredGroup)
	}
	return nil
}
