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
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
	k6scripts "github.com/goauthentik/authentik-bench/k6/scripts"
)

// DefaultTemplateName is the built-in template TestCaseSpec.Template's own
// CRD default resolves to (see testcase_controller.go's resolveTemplate).
const DefaultTemplateName = "login"

// mustEmbeddedScript reads one of this binary's own embedded k6 scripts
// (k6/scripts/*.ts). A miss here means a built-in template name and its
// ScriptName have drifted out of sync within this same package - a build-time
// programming error, not a runtime condition any caller could recover from.
func mustEmbeddedScript(name string) string {
	c, ok := k6scripts.Get(name)
	if !ok {
		panic(fmt.Sprintf("embedded k6 script %q is missing", name))
	}
	return c
}

// defaultTestCaseTemplates seeds the operator's own built-in k6 test
// templates from the exact same embedded scripts (k6/scripts/*.ts) this
// binary already carries, so a fresh install has working, immediately
// editable TestCaseTemplate objects without anyone hand-authoring
// them first.
//
// "saml" is deliberately not seeded yet - see internal/testtemplate's own
// former Registry doc comment (that package is now gone, having been
// replaced by this CRD): once a concrete SAML test client is chosen, add a
// third entry here (plus a k6/scripts/saml.ts) following the oauth-oidc
// entry's own pattern.
func defaultTestCaseTemplates(namespace string) []benchv1alpha1.TestCaseTemplate {
	return []benchv1alpha1.TestCaseTemplate{
		{
			ObjectMeta: metav1.ObjectMeta{Name: DefaultTemplateName, Namespace: namespace},
			Spec: benchv1alpha1.TestCaseTemplateSpec{
				Description: "Walks the authentik flow executor through a standard username/password (+ optional MFA) login.",
				Script:      mustEmbeddedScript("login.ts"),
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "oauth-oidc", Namespace: namespace},
			Spec: benchv1alpha1.TestCaseTemplateSpec{
				Description:         "Drives an OAuth2/OIDC authorization-code flow through an oidc-test-client relying party, reusing the same flow-executor walk as login.",
				Script:              mustEmbeddedScript("oauth.ts"),
				RequiresOAuthClient: true,
			},
		},
	}
}

// SeedDefaultTestCaseTemplates creates the operator's built-in templates if
// they don't already exist yet - create-only, never overwritten, so editing
// one via the web UI (or plain kubectl) is never clobbered by a later operator
// restart/upgrade picking up a newer embedded script. Called once at manager
// startup, before the manager's own cache starts, with a plain uncached
// client - so any failure here (e.g. the CRD not installed yet) surfaces
// immediately as a clear startup error instead of racing the manager's own
// lifecycle.
func SeedDefaultTestCaseTemplates(ctx context.Context, c client.Client, namespace string) error {
	for _, tmpl := range defaultTestCaseTemplates(namespace) {
		if err := c.Create(ctx, &tmpl); err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("seed TestCaseTemplate %q: %w", tmpl.Name, err)
		}
	}
	return nil
}
