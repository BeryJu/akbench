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

package controller

import (
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// TestOAuthProviderBlueprintIsValidYAML guards the one thing an envtest
// suite can't (there's no real authentik to apply this against): that the
// generated text is at least well-formed YAML with the shape
// ak apply_blueprint expects (version/entries), and that every value this
// function is given actually ends up in the rendered text - a plain string
// mismatch here would silently register the wrong OAuth2 client against a
// live authentik with no compiler or envtest to catch it.
func TestOAuthProviderBlueprintIsValidYAML(t *testing.T) {
	got := oauthProviderBlueprint("oauth-test-my-case", "client-id-123", "sekrit-secret", "http://oidc-test-client-my-case.default.svc.cluster.local:9009/auth/callback")

	var parsed map[string]any
	if err := yaml.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("blueprint is not valid YAML: %v\n%s", err, got)
	}
	if parsed["version"] != float64(1) {
		t.Errorf("version = %v, want 1", parsed["version"])
	}
	entries, ok := parsed["entries"].([]any)
	if !ok || len(entries) != 2 {
		t.Fatalf("entries = %v, want a 2-item list (provider, application)", parsed["entries"])
	}

	text := string(got)
	for _, want := range []string{
		"model: authentik_providers_oauth2.oauth2provider",
		"model: authentik_core.application",
		"id: oauth-test-my-case",
		"client_id: client-id-123",
		"client_secret: sekrit-secret",
		"url: http://oidc-test-client-my-case.default.svc.cluster.local:9009/auth/callback",
		"!KeyOf oauth-test-my-case",
		"!Find [authentik_flows.flow, [slug, default-provider-authorization-implicit-consent]]",
		"!Find [authentik_flows.flow, [slug, default-provider-invalidation-flow]]",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("blueprint missing %q\n%s", want, text)
		}
	}
}
