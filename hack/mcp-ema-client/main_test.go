package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	benchv1alpha1 "github.com/authentik-labs/akbench/api/v1alpha1"
	"github.com/authentik-labs/akbench/internal/api"
)

// fakeAuthentik plays both SEP-990 roles the way authentik lays them out:
// an enterprise IdP provider at /idp/ that answers token exchange with an
// ID-JAG, and akbench's authorization server provider at /as/ that answers
// the JWT bearer grant with an access token. Every token endpoint request
// is checked for the parameters SEP-990 requires.
type fakeAuthentik struct {
	t   *testing.T
	srv *httptest.Server
	key *rsa.PrivateKey
	// idJAGType overrides the ID-JAG's typ header, to break conformance.
	idJAGType string
}

func newFakeAuthentik(t *testing.T) *fakeAuthentik {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeAuthentik{t: t, key: key, idJAGType: idJAGType}
	mux := http.NewServeMux()
	for _, p := range []string{"idp", "as"} {
		mux.HandleFunc("/"+p+"/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
			issuer := f.srv.URL + "/" + p + "/"
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer": issuer, "jwks_uri": f.srv.URL + "/jwks", "token_endpoint": issuer + "token",
				"authorization_endpoint": issuer + "authorize", "response_types_supported": []string{"code"},
				"code_challenge_methods_supported":      []string{"S256"},
				"grant_types_supported":                 []string{"authorization_code", grantTypeTokenExchange, grantTypeJWTBearer},
				"id_token_signing_alg_values_supported": []string{"RS256"},
			})
		})
	}
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &key.PublicKey, KeyID: "k", Algorithm: string(jose.RS256), Use: "sig"},
		}})
	})
	mux.HandleFunc("/idp/token", f.tokenExchange)
	mux.HandleFunc("/as/token", f.jwtBearer)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAuthentik) idpIssuer() string { return f.srv.URL + "/idp/" }
func (f *fakeAuthentik) asIssuer() string  { return f.srv.URL + "/as/" }

func (f *fakeAuthentik) sign(typ string, claims map[string]any) string {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: f.key},
		(&jose.SignerOptions{}).WithType(jose.ContentType(typ)).WithHeader("kid", "k"))
	if err != nil {
		f.t.Fatal(err)
	}
	raw, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		f.t.Fatal(err)
	}
	return raw
}

func (f *fakeAuthentik) require(r *http.Request, want map[string]string) bool {
	for k, v := range want {
		if got := r.PostFormValue(k); got != v {
			f.t.Errorf("%s: %s = %q, want %q", r.URL.Path, k, got, v)
			return false
		}
	}
	return true
}

func (f *fakeAuthentik) tokenExchange(w http.ResponseWriter, r *http.Request) {
	if !f.require(r, map[string]string{
		"grant_type": grantTypeTokenExchange, "requested_token_type": tokenTypeIDJAG,
		"subject_token_type": tokenTypeIDToken, "audience": f.asIssuer(), "client_id": "idp-client",
	}) {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}
	now := time.Now()
	idJAG := f.sign(f.idJAGType, map[string]any{
		"iss": f.idpIssuer(), "sub": "alice", "aud": f.asIssuer(), "client_id": "mcp-client",
		"resource": r.PostFormValue("resource"), "jti": "j1", "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
	})
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": idJAG, "issued_token_type": tokenTypeIDJAG, "token_type": "N_A", "expires_in": 300,
	})
}

func (f *fakeAuthentik) jwtBearer(w http.ResponseWriter, r *http.Request) {
	if !f.require(r, map[string]string{"grant_type": grantTypeJWTBearer, "client_id": "mcp-client"}) {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}
	assertion, err := jwt.ParseSigned(r.PostFormValue("assertion"), []jose.SignatureAlgorithm{jose.RS256})
	var claims map[string]any
	if err != nil || assertion.Claims(&f.key.PublicKey, &claims) != nil {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	}
	now := time.Now()
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": f.sign("at+jwt", map[string]any{
			"iss": f.asIssuer(), "sub": claims["sub"], "aud": claims["resource"], "client_id": "mcp-client",
			"scope": "akbench:mcp", "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
		}),
		"token_type": "Bearer", "expires_in": 3600,
	})
}

// newAkbench serves akbench's API with MCP behind EMA, trusting fa's
// authorization server.
func newAkbench(t *testing.T, fa *fakeAuthentik) string {
	scheme := runtime.NewScheme()
	if err := benchv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(nil)
	srv.Start()
	t.Cleanup(srv.Close)
	resource := srv.URL + "/api/v1/mcp"
	s := api.New(fake.NewClientBuilder().WithScheme(scheme).Build(), api.Config{
		Namespace: "default", Token: "static",
		MCPAuth: &api.MCPAuthConfig{Issuer: fa.asIssuer(), Resource: resource, Scopes: []string{"akbench:mcp"}},
	})
	srv.Config.Handler = s.Router()
	return resource
}

func TestEndToEnd(t *testing.T) {
	fa := newFakeAuthentik(t)
	resource := newAkbench(t, fa)
	failedChecks = 0

	err := run(context.Background(), &options{
		mcpURL: resource, mcpClientID: "mcp-client",
		idpIssuer: fa.idpIssuer(), idpClientID: "idp-client",
		idToken: fa.sign("JWT", map[string]any{"iss": fa.idpIssuer(), "sub": "alice", "aud": "idp-client"}),
		call:    "list_metrics", args: "{}",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
}

// A non-conforming ID-JAG still gets a token here, but the run fails.
func TestEndToEndReportsNonConformingIDJAG(t *testing.T) {
	fa := newFakeAuthentik(t)
	fa.idJAGType = "JWT"
	resource := newAkbench(t, fa)
	failedChecks = 0

	err := run(context.Background(), &options{
		mcpURL: resource, mcpClientID: "mcp-client",
		idpIssuer: fa.idpIssuer(), idpClientID: "idp-client",
		idToken: fa.sign("JWT", map[string]any{"iss": fa.idpIssuer(), "sub": "alice", "aud": "idp-client"}),
	})
	if err == nil || err.Error() != "1 checks failed" {
		t.Fatalf("run returned %v, want 1 failed check", err)
	}
}
