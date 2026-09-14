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

// Package testtemplate is the small, code-maintained catalog of built-in k6
// test presets a TestCase can select via spec.template - not a CRD, since
// this is a fixed, developer-curated list (add a template, ship a release),
// not something users author. A template answers more than "which script
// runs": some flows need extra provisioning to make sense at all (the
// oauth-oidc template's own oidc-test-client + OAuth2 provider/application,
// owned by TestCaseReconciler - see its RequiresOAuthClient field).
package testtemplate

// Template is one built-in test preset.
type Template struct {
	// Name matches TestCaseSpec.Template and this package's Registry key.
	Name string
	// Description documents what this template exercises, for whoever's
	// adding the next one.
	Description string
	// ScriptName is the k6/scripts file this template runs, embedded via
	// k6/scripts/embed.go.
	ScriptName string
	// RequiresOAuthClient gates TestCaseReconciler's oidc-test-client
	// Deployment/Service + OAuth2 provider/application blueprint
	// provisioning (internal/controller/testcase_controller.go,
	// internal/render/oidctestclient.go) - scoped to the TestCase, not the
	// AuthentikDeployment it targets, so "what's being tested" stays fully
	// decoupled from "what's being tested against" the same way the k6
	// workload itself already is.
	RequiresOAuthClient bool
}

// DefaultName is TestCaseSpec.Template's own CRD default.
const DefaultName = "login"

// Registry is every built-in template this operator knows how to run.
//
// "saml" is intentionally not registered yet: an unregistered template name
// gives a clear "unknown template" validation error (see
// testcase_controller.go's Reconcile) rather than a silently-broken
// selectable-but-non-functional option. Once a concrete SAML test client is
// chosen, follow the oauth-oidc entry's own pattern: a render/*.go
// Deployment+Service for the SAML SP test client, a blueprint fragment
// registering a SAML provider/application, and a k6/scripts/saml.ts reusing
// lib/flow.ts the same way oauth.ts does.
var Registry = map[string]Template{
	DefaultName: {
		Name:        DefaultName,
		Description: "Walks the authentik flow executor through a standard username/password (+ optional MFA) login.",
		ScriptName:  "login.ts",
	},
	"oauth-oidc": {
		Name:                "oauth-oidc",
		Description:         "Drives an OAuth2/OIDC authorization-code flow through an oidc-test-client relying party, reusing the same flow-executor walk as login.",
		ScriptName:          "oauth.ts",
		RequiresOAuthClient: true,
	},
}

// Resolve looks up a template by name, defaulting an empty name to
// DefaultName (in case this is reached before CRD admission defaulting -
// e.g. a TestCase built by hand in a test). An unknown non-empty name
// returns ok=false; callers must surface that as a validation error rather
// than have it silently coerced here (see testcase_controller.go).
func Resolve(name string) (Template, bool) {
	if name == "" {
		name = DefaultName
	}
	t, ok := Registry[name]
	return t, ok
}
