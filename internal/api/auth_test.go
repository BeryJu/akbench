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
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeVerifier accepts exactly one token.
type fakeVerifier struct{ valid string }

func (f fakeVerifier) Verify(_ context.Context, raw string) error {
	if raw != f.valid {
		return errors.New("bad token")
	}
	return nil
}

func requestWithBearer(t *testing.T, s *Server, target, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	return rec
}

func TestRequireTokenAcceptsStaticOrOIDC(t *testing.T) {
	s, _ := newServer(t, Config{
		OIDC:       fakeVerifier{valid: "a.valid.jwt"},
		OIDCConfig: OIDCConfig{Issuer: "https://idp.example/", ClientID: "bench"},
	})
	for token, want := range map[string]int{
		testToken:     http.StatusOK,
		"a.valid.jwt": http.StatusOK,
		"forged.jwt":  http.StatusUnauthorized,
		"":            http.StatusUnauthorized,
	} {
		requireStatus(t, requestWithBearer(t, s, "/api/v1/deployments", token), want)
	}
}

func TestRequireTokenOIDCOnly(t *testing.T) {
	s, _ := newServer(t, Config{OIDC: fakeVerifier{valid: "a.valid.jwt"}})
	// newServer fills in testToken; clear it to model an OIDC-only backend.
	s.cfg.Token = ""
	requireStatus(t, requestWithBearer(t, s, "/api/v1/deployments", "a.valid.jwt"), http.StatusOK)
	requireStatus(t, requestWithBearer(t, s, "/api/v1/deployments", testToken), http.StatusUnauthorized)
	requireStatus(t, requestWithBearer(t, s, "/api/v1/deployments", ""), http.StatusUnauthorized)
}

func TestAuthConfigIsPublic(t *testing.T) {
	s, _ := newServer(t, Config{
		OIDC:       fakeVerifier{},
		OIDCConfig: OIDCConfig{Issuer: "https://idp.example/", ClientID: "bench"},
	})
	rec := requestWithBearer(t, s, "/api/v1/auth/config", "")
	requireStatus(t, rec, http.StatusOK)
	got := decodeBody[authConfigResponse](t, rec)
	if got.OIDC == nil || got.OIDC.Issuer != "https://idp.example/" || got.OIDC.ClientID != "bench" ||
		len(got.OIDC.Scopes) != 3 {
		t.Fatalf("auth config = %+v", got.OIDC)
	}

	plain, _ := newServer(t, Config{})
	if got := decodeBody[authConfigResponse](t, requestWithBearer(t, plain, "/api/v1/auth/config", "")); got.OIDC != nil {
		t.Fatalf("auth config without OIDC = %+v, want none", got.OIDC)
	}
}
