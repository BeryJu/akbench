package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	benchv1alpha1 "github.com/authentik-labs/akbench/api/v1alpha1"
	"github.com/authentik-labs/akbench/internal/api"
)

// fakeAuthentik is an enterprise IdP the way authentik lays one out: a
// provider at /idp/ that answers token exchange with an ID-JAG for akbench,
// checking the request for the parameters SEP-990 requires.
type fakeAuthentik struct {
	t   *testing.T
	srv *httptest.Server
	key *rsa.PrivateKey
	// idJAGType overrides the ID-JAG's typ header, to break conformance.
	idJAGType string
	// akbench is akbench's issuer, the ID-JAG audience.
	akbench string
}

func newFakeAuthentik(t *testing.T) *fakeAuthentik {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeAuthentik{t: t, key: key, idJAGType: idJAGType}
	mux := http.NewServeMux()
	mux.HandleFunc("/idp/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		issuer := f.idpIssuer()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": issuer, "jwks_uri": f.srv.URL + "/jwks", "token_endpoint": issuer + "token",
			"authorization_endpoint": issuer + "authorize", "response_types_supported": []string{"code"},
			"code_challenge_methods_supported":      []string{"S256"},
			"grant_types_supported":                 []string{"authorization_code", grantTypeTokenExchange},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &key.PublicKey, KeyID: "k", Algorithm: string(jose.RS256), Use: "sig"},
		}})
	})
	mux.HandleFunc("/idp/token", f.tokenExchange)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAuthentik) idpIssuer() string { return f.srv.URL + "/idp/" }

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
		"subject_token_type": tokenTypeIDToken, "audience": f.akbench, "client_id": "idp-client",
	}) {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}
	now := time.Now()
	idJAG := f.sign(f.idJAGType, map[string]any{
		"iss": f.idpIssuer(), "sub": "alice", "aud": f.akbench, "client_id": "mcp-client",
		"resource": r.PostFormValue("resource"), "jti": rand.Text(), "act": map[string]any{"sub": "agent"},
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
	})
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": idJAG, "issued_token_type": tokenTypeIDJAG, "token_type": "N_A", "expires_in": 300,
	})
}

// newAkbench serves akbench's API with MCP behind EMA as its own
// authorization server, trusting fa's ID-JAGs.
func newAkbench(t *testing.T, fa *fakeAuthentik) string {
	scheme := runtime.NewScheme()
	if err := benchv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(nil)
	srv.Start()
	t.Cleanup(srv.Close)
	resource := srv.URL + "/api/v1/mcp"
	fa.akbench = srv.URL
	s := api.New(fake.NewClientBuilder().WithScheme(scheme).Build(), api.Config{
		Namespace: "default", Token: "static",
		MCPAuth: &api.MCPAuthConfig{
			Resource: resource, IdPIssuer: fa.idpIssuer(), ClientID: "mcp-client", ClientSecret: "s3cret",
		},
	})
	srv.Config.Handler = s.Router()
	return resource
}

func TestEndToEnd(t *testing.T) {
	fa := newFakeAuthentik(t)
	resource := newAkbench(t, fa)
	failedChecks = 0

	err := run(context.Background(), &options{
		mcpURL: resource, mcpClientID: "mcp-client", mcpClientSecret: "s3cret",
		idpIssuer: fa.idpIssuer(), idpClientID: "idp-client",
		idToken: fa.sign("JWT", map[string]any{"iss": fa.idpIssuer(), "sub": "alice", "aud": "idp-client"}),
		call:    "list_metrics", args: "{}",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
}

// akbench refuses a non-conforming ID-JAG, which the client flags too.
func TestEndToEndReportsNonConformingIDJAG(t *testing.T) {
	fa := newFakeAuthentik(t)
	fa.idJAGType = "JWT"
	resource := newAkbench(t, fa)
	failedChecks = 0

	err := run(context.Background(), &options{
		mcpURL: resource, mcpClientID: "mcp-client", mcpClientSecret: "s3cret",
		idpIssuer: fa.idpIssuer(), idpClientID: "idp-client",
		idToken: fa.sign("JWT", map[string]any{"iss": fa.idpIssuer(), "sub": "alice", "aud": "idp-client"}),
	})
	if err == nil || !strings.Contains(err.Error(), "invalid_grant") || failedChecks != 1 {
		t.Fatalf("run returned %v with %d failed checks, want invalid_grant and 1", err, failedChecks)
	}
}
