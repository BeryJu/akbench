package api

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeIssuer is an OAuth authorization server reduced to what akbench's
// MCP auth needs from one: OIDC discovery and a JWKS to verify against.
type fakeIssuer struct {
	*httptest.Server
	key *rsa.PrivateKey
}

func newFakeIssuer(t *testing.T) *fakeIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeIssuer{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": f.URL, "jwks_uri": f.URL + "/jwks", "token_endpoint": f.URL + "/token",
			"authorization_endpoint": f.URL + "/authorize", "id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &key.PublicKey, KeyID: "k", Algorithm: string(jose.RS256), Use: "sig"},
		}})
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func (f *fakeIssuer) sign(t *testing.T, claims map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: f.key},
		(&jose.SignerOptions{}).WithType("at+jwt").WithHeader("kid", "k"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

const testMCPResource = "https://bench.example.com/api/v1/mcp"

func (f *fakeIssuer) accessToken(t *testing.T, overrides map[string]any) string {
	claims := map[string]any{
		"iss": f.URL, "sub": "u1", "aud": testMCPResource, "preferred_username": "alice",
		"client_id": "claude", "scope": "akbench:mcp",
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
	}
	maps.Copy(claims, overrides)
	return f.sign(t, claims)
}

func newMCPAuthServer(t *testing.T, issuer *fakeIssuer) *httptest.Server {
	t.Helper()
	s, _ := newServer(t, Config{MCPAuth: &MCPAuthConfig{
		Issuer: issuer.URL, Resource: testMCPResource, Scopes: []string{"akbench:mcp"},
	}})
	srv := httptest.NewServer(s.Router())
	t.Cleanup(srv.Close)
	return srv
}

func postMCP(t *testing.T, url, token string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url+"/api/v1/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp
}

func TestMCPAuthServesResourceMetadata(t *testing.T) {
	issuer := newFakeIssuer(t)
	srv := newMCPAuthServer(t, issuer)

	resp, err := http.Get(srv.URL + "/.well-known/oauth-protected-resource/api/v1/mcp")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var prm struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
		ScopesSupported      []string `json:"scopes_supported"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&prm); err != nil {
		t.Fatal(err)
	}
	if prm.Resource != testMCPResource || len(prm.AuthorizationServers) != 1 || prm.AuthorizationServers[0] != issuer.URL {
		t.Fatalf("unexpected resource metadata: %+v", prm)
	}
}

func TestMCPAuthChallengesWithResourceMetadata(t *testing.T) {
	srv := newMCPAuthServer(t, newFakeIssuer(t))
	resp := postMCP(t, srv.URL, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", resp.StatusCode)
	}
	want := `resource_metadata="https://bench.example.com/.well-known/oauth-protected-resource/api/v1/mcp"`
	if got := resp.Header.Get("WWW-Authenticate"); !strings.Contains(got, want) {
		t.Fatalf("WWW-Authenticate %q lacks %s", got, want)
	}
}

func TestMCPAuthRejectsOtherTokens(t *testing.T) {
	issuer := newFakeIssuer(t)
	srv := newMCPAuthServer(t, issuer)
	other := newFakeIssuer(t)

	for name, tc := range map[string]struct {
		token string
		want  int
	}{
		// EMA only: the API's own static token no longer opens MCP.
		"static API token": {testToken, http.StatusUnauthorized},
		"wrong audience":   {issuer.accessToken(t, map[string]any{"aud": "web-ui"}), http.StatusUnauthorized},
		"expired":          {issuer.accessToken(t, map[string]any{"exp": time.Now().Add(-time.Hour).Unix()}), http.StatusUnauthorized},
		"other issuer":     {other.accessToken(t, map[string]any{"iss": issuer.URL}), http.StatusUnauthorized},
		"missing scope":    {issuer.accessToken(t, map[string]any{"scope": "openid"}), http.StatusForbidden},
	} {
		t.Run(name, func(t *testing.T) {
			if resp := postMCP(t, srv.URL, tc.token); resp.StatusCode != tc.want {
				t.Fatalf("status %d, want %d", resp.StatusCode, tc.want)
			}
		})
	}
}

// An MCP access token must not work on the rest of the API: its audience
// is the MCP resource, never the web UI's client ID.
func TestMCPAuthTokenDoesNotOpenTheAPI(t *testing.T) {
	issuer := newFakeIssuer(t)
	srv := newMCPAuthServer(t, issuer)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/testcases", nil)
	req.Header.Set("Authorization", "Bearer "+issuer.accessToken(t, nil))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", resp.StatusCode)
	}
}

func TestMCPAuthAcceptsValidToken(t *testing.T) {
	issuer := newFakeIssuer(t)
	srv := newMCPAuthServer(t, issuer)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             srv.URL + "/api/v1/mcp",
		HTTPClient:           &http.Client{Transport: bearerTransport{issuer.accessToken(t, nil)}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()
	if _, err := session.ListTools(context.Background(), nil); err != nil {
		t.Fatalf("list tools: %v", err)
	}
}

func TestMCPAuthConfigValidate(t *testing.T) {
	ui := OIDCConfig{Issuer: "https://idp/application/o/bench/", ClientID: "web-ui"}
	for name, tc := range map[string]struct {
		cfg MCPAuthConfig
		ok  bool
	}{
		"no resource":       {MCPAuthConfig{Issuer: "https://idp"}, false},
		"no issuer":         {MCPAuthConfig{Resource: testMCPResource}, false},
		"relative resource": {MCPAuthConfig{Issuer: "https://idp", Resource: "/api/v1/mcp"}, false},
		// The UI's own issuer is fine as long as MCP tokens carry the
		// resource as their audience...
		"shared issuer": {MCPAuthConfig{Issuer: ui.Issuer, Resource: testMCPResource}, true},
		// ...but not the UI's client ID: the tokens would be interchangeable.
		"shared issuer and audience": {
			MCPAuthConfig{Issuer: "https://idp/application/o/bench", Resource: testMCPResource, Audience: "web-ui"}, false,
		},
		"separate provider, same client ID": {
			MCPAuthConfig{Issuer: "https://idp/application/o/bench-mcp/", Resource: testMCPResource, Audience: "web-ui"}, true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := tc.cfg.Validate(ui); (err == nil) != tc.ok {
				t.Fatalf("Validate() = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}
