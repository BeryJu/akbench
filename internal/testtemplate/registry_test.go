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

package testtemplate

import "testing"

func TestResolveDefaultsEmptyNameToLogin(t *testing.T) {
	got, ok := Resolve("")
	if !ok {
		t.Fatal("Resolve(\"\") = not ok, want the login default")
	}
	if got.Name != DefaultName || got.ScriptName != "login.ts" {
		t.Errorf("Resolve(\"\") = %+v, want name=%q scriptName=login.ts", got, DefaultName)
	}
}

func TestResolveKnownTemplates(t *testing.T) {
	for name, want := range Registry {
		got, ok := Resolve(name)
		if !ok {
			t.Errorf("Resolve(%q) = not ok", name)
			continue
		}
		if got != want {
			t.Errorf("Resolve(%q) = %+v, want %+v", name, got, want)
		}
	}
}

func TestResolveUnknownTemplateNotOK(t *testing.T) {
	if _, ok := Resolve("saml"); ok {
		t.Error("Resolve(\"saml\") = ok, want not-ok - saml is reserved but not yet registered")
	}
	if _, ok := Resolve("does-not-exist"); ok {
		t.Error("Resolve(\"does-not-exist\") = ok, want not-ok")
	}
}

func TestOAuthTemplateRequiresOAuthClient(t *testing.T) {
	got, ok := Resolve("oauth-oidc")
	if !ok {
		t.Fatal("Resolve(\"oauth-oidc\") = not ok")
	}
	if !got.RequiresOAuthClient {
		t.Error("oauth-oidc template RequiresOAuthClient = false, want true")
	}
	if got.ScriptName != "oauth.ts" {
		t.Errorf("oauth-oidc template ScriptName = %q, want oauth.ts", got.ScriptName)
	}
}
