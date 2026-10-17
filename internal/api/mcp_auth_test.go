package api

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeIdP is an enterprise IdP reduced to what akbench's MCP authorization
// server needs from one: OIDC discovery and a JWKS to verify ID-JAGs against.
type fakeIdP struct {
	*httptest.Server
	key *rsa.PrivateKey
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeIdP{key: key}
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

func (f *fakeIdP) sign(t *testing.T, typ string, claims map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: f.key},
		(&jose.SignerOptions{}).WithType(jose.ContentType(typ)).WithHeader("kid", "k"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

const (
	testMCPResource = "https://bench.example.com/api/v1/mcp"
	testMCPIssuer   = "https://bench.example.com"
)

// idJAG is an ID-JAG as authentik mints it, with overrides applied (a nil
// value removes the claim).
func (f *fakeIdP) idJAG(t *testing.T, overrides map[string]any) string {
	claims := map[string]any{
		"iss": f.URL, "sub": "u1", "aud": testMCPIssuer, "client_id": "claude", "resource": testMCPResource,
		"scope": "akbench:mcp", "jti": rand.Text(), "act": map[string]any{"sub": "agent-1"},
		"exp": time.Now().Add(5 * time.Minute).Unix(), "iat": time.Now().Unix(),
	}
	maps.Copy(claims, overrides)
	maps.DeleteFunc(claims, func(_ string, v any) bool { return v == nil })
	return f.sign(t, idJAGType, claims)
}

func newMCPAuthServer(t *testing.T, idp *fakeIdP) *httptest.Server {
	t.Helper()
	s, _ := newServer(t, Config{MCPAuth: &MCPAuthConfig{
		Resource: testMCPResource, IdPIssuer: idp.URL, ClientID: "claude", ClientSecret: "s3cret",
		Scopes: []string{"akbench:mcp"},
	}})
	srv := httptest.NewServer(s.Router())
	t.Cleanup(srv.Close)
	return srv
}

// grant runs the JWT bearer grant and returns the status and response body.
func grant(t *testing.T, srvURL string, form url.Values) (int, map[string]any) {
	t.Helper()
	resp, err := http.PostForm(srvURL+mcpTokenPath, form)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, body
}

func grantForm(assertion string) url.Values {
	return url.Values{
		"grant_type": {grantTypeJWTBearer}, "assertion": {assertion},
		"client_id": {"claude"}, "client_secret": {"s3cret"},
	}
}

func accessToken(t *testing.T, srvURL string, idJAG string) string {
	t.Helper()
	status, body := grant(t, srvURL, grantForm(idJAG))
	token, _ := body["access_token"].(string)
	if status != http.StatusOK || token == "" {
		t.Fatalf("grant: %d %v", status, body)
	}
	return token
}

func postMCP(t *testing.T, srvURL, token string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srvURL+"/api/v1/mcp",
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

func getJSON(t *testing.T, metadataURL string, into any) {
	t.Helper()
	resp, err := http.Get(metadataURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		t.Fatal(err)
	}
}

func TestMCPAuthServesMetadata(t *testing.T) {
	srv := newMCPAuthServer(t, newFakeIdP(t))

	var prm struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
	}
	getJSON(t, srv.URL+"/.well-known/oauth-protected-resource/api/v1/mcp", &prm)
	if prm.Resource != testMCPResource || len(prm.AuthorizationServers) != 1 || prm.AuthorizationServers[0] != testMCPIssuer {
		t.Fatalf("unexpected resource metadata: %+v", prm)
	}

	var asm struct {
		Issuer              string   `json:"issuer"`
		TokenEndpoint       string   `json:"token_endpoint"`
		GrantTypesSupported []string `json:"grant_types_supported"`
	}
	getJSON(t, srv.URL+"/.well-known/oauth-authorization-server", &asm)
	if asm.Issuer != testMCPIssuer || asm.TokenEndpoint != testMCPIssuer+mcpTokenPath ||
		len(asm.GrantTypesSupported) != 1 || asm.GrantTypesSupported[0] != grantTypeJWTBearer {
		t.Fatalf("unexpected authorization server metadata: %+v", asm)
	}
}

func TestMCPAuthChallengesWithResourceMetadata(t *testing.T) {
	srv := newMCPAuthServer(t, newFakeIdP(t))
	resp := postMCP(t, srv.URL, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", resp.StatusCode)
	}
	want := `resource_metadata="https://bench.example.com/.well-known/oauth-protected-resource/api/v1/mcp"`
	if got := resp.Header.Get("WWW-Authenticate"); !strings.Contains(got, want) {
		t.Fatalf("WWW-Authenticate %q lacks %s", got, want)
	}
}

func TestMCPAuthGrantsForValidIDJAG(t *testing.T) {
	idp := newFakeIdP(t)
	srv := newMCPAuthServer(t, idp)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             srv.URL + "/api/v1/mcp",
		HTTPClient:           &http.Client{Transport: bearerTransport{accessToken(t, srv.URL, idp.idJAG(t, nil))}},
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

func TestMCPAuthRejectsBadGrants(t *testing.T) {
	idp := newFakeIdP(t)
	other := newFakeIdP(t)
	srv := newMCPAuthServer(t, idp)
	replayed := idp.idJAG(t, nil)
	accessToken(t, srv.URL, replayed)

	withForm := func(assertion string, set map[string]string) url.Values {
		form := grantForm(assertion)
		for k, v := range set {
			form.Set(k, v)
		}
		return form
	}
	for name, tc := range map[string]struct {
		form url.Values
		want string
	}{
		"wrong secret":     {withForm(idp.idJAG(t, nil), map[string]string{"client_secret": "nope"}), "invalid_client"},
		"other client":     {withForm(idp.idJAG(t, nil), map[string]string{"client_id": "other"}), "invalid_client"},
		"wrong grant type": {withForm(idp.idJAG(t, nil), map[string]string{"grant_type": "client_credentials"}), "unsupported_grant_type"},
		"plain JWT":        {grantForm(idp.sign(t, "JWT", map[string]any{"iss": idp.URL, "aud": testMCPIssuer})), "invalid_grant"},
		"other issuer":     {grantForm(other.idJAG(t, map[string]any{"iss": idp.URL})), "invalid_grant"},
		"wrong audience":   {grantForm(idp.idJAG(t, map[string]any{"aud": testMCPIssuer + "/"})), "invalid_grant"},
		"wrong client_id":  {grantForm(idp.idJAG(t, map[string]any{"client_id": "other"})), "invalid_grant"},
		"wrong resource":   {grantForm(idp.idJAG(t, map[string]any{"resource": "https://other/mcp"})), "invalid_grant"},
		"expired":          {grantForm(idp.idJAG(t, map[string]any{"exp": time.Now().Add(-time.Minute).Unix()})), "invalid_grant"},
		"no iat":           {grantForm(idp.idJAG(t, map[string]any{"iat": nil})), "invalid_grant"},
		"no jti":           {grantForm(idp.idJAG(t, map[string]any{"jti": nil})), "invalid_grant"},
		"replayed":         {grantForm(replayed), "invalid_grant"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, body := grant(t, srv.URL, tc.form); body["error"] != tc.want {
				t.Fatalf("got %v, want error %s", body, tc.want)
			}
		})
	}
}

// The granted scopes never exceed the ID-JAG's.
func TestMCPAuthLimitsScopesToIDJAG(t *testing.T) {
	idp := newFakeIdP(t)
	srv := newMCPAuthServer(t, idp)

	form := grantForm(idp.idJAG(t, map[string]any{"scope": "openid"}))
	form.Set("scope", "openid akbench:mcp")
	status, body := grant(t, srv.URL, form)
	if status != http.StatusOK || body["scope"] != "openid" {
		t.Fatalf("grant: %d %v, want scope openid only", status, body)
	}
	if resp := postMCP(t, srv.URL, body["access_token"].(string)); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("MCP status %d, want 403 for the missing scope", resp.StatusCode)
	}
}

func TestMCPAuthRejectsOtherTokens(t *testing.T) {
	idp := newFakeIdP(t)
	srv := newMCPAuthServer(t, idp)
	for name, token := range map[string]string{
		// EMA only: the API's own static token no longer opens MCP.
		"static API token": testToken,
		// The ID-JAG is a grant, not an access token.
		"ID-JAG": idp.idJAG(t, nil),
		// Neither is a token from another akbench (or this one before a restart).
		"other akbench": accessToken(t, newMCPAuthServer(t, idp).URL, idp.idJAG(t, nil)),
	} {
		t.Run(name, func(t *testing.T) {
			if resp := postMCP(t, srv.URL, token); resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status %d, want 401", resp.StatusCode)
			}
		})
	}
}

// An MCP access token must not work on the rest of the API.
func TestMCPAuthTokenDoesNotOpenTheAPI(t *testing.T) {
	idp := newFakeIdP(t)
	srv := newMCPAuthServer(t, idp)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/testcases", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken(t, srv.URL, idp.idJAG(t, nil)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", resp.StatusCode)
	}
}

func TestMCPAuthConfigValidate(t *testing.T) {
	valid := MCPAuthConfig{Resource: testMCPResource, IdPIssuer: "https://idp", ClientID: "c", ClientSecret: "s"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	for name, mutate := range map[string]func(*MCPAuthConfig){
		"no IdP":            func(c *MCPAuthConfig) { c.IdPIssuer = "" },
		"no client":         func(c *MCPAuthConfig) { c.ClientID = "" },
		"no secret":         func(c *MCPAuthConfig) { c.ClientSecret = "" },
		"relative resource": func(c *MCPAuthConfig) { c.Resource = "/api/v1/mcp" },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := valid
			mutate(&cfg)
			if cfg.Validate() == nil {
				t.Fatal("Validate() = nil, want an error")
			}
		})
	}
}
