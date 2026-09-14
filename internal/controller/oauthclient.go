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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
	"github.com/goauthentik/authentik-bench/internal/render"
)

// oauthProviderBlueprint renders the OAuth2Provider + Application blueprint
// fragment the oauth-oidc template's oidc-test-client needs registered in
// authentik, verified directly against authentik's own source
// (blueprints/testing/oidc-conformance.yaml, authentik/providers/oauth2/models.py,
// authentik/core/models.py) rather than assumed - the model names, field
// shapes, and !Find/!KeyOf tags below match that real in-tree example
// blueprint. identifiers key on name/slug (matching that example precisely,
// not client_id, though client_id is equally unique); client_id/client_secret
// are always given as literal attrs rather than left to the model's own
// generate_id/generate_client_secret defaults, which is what keeps
// re-applying this same blueprint every reconcile idempotent (an update to
// the same row) rather than drifting.
//
// authorization_flow/invalidation_flow reference authentik's own built-in
// default flows by slug (default-provider-authorization-implicit-consent /
// default-provider-invalidation-flow) - the same slugs
// blueprints/testing/oidc-conformance.yaml itself uses, seeded by
// authentik's always-applied blueprints/default/*.yaml. scope-openid/-email/
// -profile-oidc-standard/-offline_access match exactly what oidc-test-client
// itself requests by default (its own OIDC_SCOPES default).
func oauthProviderBlueprint(id, clientID, clientSecret, redirectURI string) []byte {
	return fmt.Appendf(nil, `version: 1
entries:
  - model: authentik_providers_oauth2.oauth2provider
    id: %[1]s
    identifiers:
      name: %[1]s
    attrs:
      authorization_flow: !Find [authentik_flows.flow, [slug, default-provider-authorization-implicit-consent]]
      invalidation_flow: !Find [authentik_flows.flow, [slug, default-provider-invalidation-flow]]
      client_type: confidential
      client_id: %[2]s
      client_secret: %[3]s
      redirect_uris:
        - matching_mode: strict
          url: %[4]s
          redirect_uri_type: authorization
      grant_types:
        - authorization_code
      property_mappings:
        - !Find [authentik_providers_oauth2.scopemapping, [managed, goauthentik.io/providers/oauth2/scope-openid]]
        - !Find [authentik_providers_oauth2.scopemapping, [managed, goauthentik.io/providers/oauth2/scope-email]]
        - !Find [authentik_providers_oauth2.scopemapping, [managed, goauthentik.io/providers/oauth2/scope-profile-oidc-standard]]
        - !Find [authentik_providers_oauth2.scopemapping, [managed, goauthentik.io/providers/oauth2/scope-offline_access]]
      signing_key: !Find [authentik_crypto.certificatekeypair, [name, authentik Self-signed Certificate]]
  - model: authentik_core.application
    identifiers:
      slug: %[1]s
    attrs:
      provider: !KeyOf %[1]s
      name: %[1]s
`, id, clientID, clientSecret, redirectURI)
}

// ensureOAuthClient provisions what the oauth-oidc template's k6 script
// needs beyond the plain flow-executor walk login.ts already does: a
// running oidc-test-client (owned by tc, not topo - see
// testtemplate.Template.RequiresOAuthClient's own doc comment for why) plus
// a matching OAuth2Provider/Application registered in the target
// AuthentikDeployment via the same ak apply_blueprint mechanism
// AuthentikDeploymentReconciler's own fixtures blueprint already uses.
// oidc-test-client is created on rc (the same cluster the k6 workload
// itself runs on, not necessarily topo's own cluster) since that's the one
// cluster this reconcile already knows can reach BenchHost/BenchPort - the
// blueprint application, in contrast, always execs into topo's own cluster,
// resolved independently since it can differ from rc when
// spec.runnerClusterRef is set.
//
// Returns oidc-test-client's own in-cluster host/port, to render as
// OAUTH_TEST_CLIENT_HOST/PORT. RBAC markers for the extra resources this
// needs (services, secrets, pods, pods/exec) live on
// TestCaseReconciler.Reconcile in testcase_controller.go, not here -
// controller-gen's RBAC generator only picks up markers attached to a
// Reconcile method, confirmed by testing: the identical marker on this
// helper was silently dropped from the generated ClusterRole.
func (r *TestCaseReconciler) ensureOAuthClient(ctx context.Context, rc client.Client, remote bool, tc *benchv1alpha1.TestCase, topo *benchv1alpha1.AuthentikDeployment, host string, port int32) (string, int32, error) {
	id := "oauth-test-" + tc.Name

	clientSecret, err := ensureRandomSecret(ctx, rc,
		types.NamespacedName{Name: render.OIDCTestClientName(tc) + "-secret", Namespace: tc.Namespace},
		"client-secret", 32)
	if err != nil {
		return "", 0, fmt.Errorf("ensure oauth client secret: %w", err)
	}

	rootURL := render.OIDCTestClientRootURL(tc, tc.Namespace)
	redirectURI := rootURL + "/auth/callback"
	// authentik's own OAuth2/OIDC issuer for an Application - not
	// independently verified against a live instance in this change (unlike
	// the blueprint schema above, which was checked directly against
	// authentik's source), but a stable, documented part of its public API.
	issuerURL := fmt.Sprintf("http://%s:%d/application/o/%s/", host, port, id)

	dep := render.OIDCTestClientDeployment(tc, tc.Namespace, issuerURL, id, clientSecret)
	svc := render.OIDCTestClientService(tc, tc.Namespace)
	if !remote {
		if err := controllerutil.SetControllerReference(tc, dep, r.Scheme); err != nil {
			return "", 0, err
		}
		if err := controllerutil.SetControllerReference(tc, svc, r.Scheme); err != nil {
			return "", 0, err
		}
	}

	existingDep := &appsv1.Deployment{}
	switch err := rc.Get(ctx, types.NamespacedName{Name: dep.Name, Namespace: dep.Namespace}, existingDep); {
	case apierrors.IsNotFound(err):
		if err := rc.Create(ctx, dep); err != nil {
			return "", 0, fmt.Errorf("create oidc-test-client deployment: %w", err)
		}
	case err != nil:
		return "", 0, fmt.Errorf("get oidc-test-client deployment: %w", err)
	default:
		existingDep.Spec = dep.Spec
		if err := rc.Update(ctx, existingDep); err != nil {
			return "", 0, fmt.Errorf("update oidc-test-client deployment: %w", err)
		}
	}

	existingSvc := &corev1.Service{}
	switch err := rc.Get(ctx, types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}, existingSvc); {
	case apierrors.IsNotFound(err):
		if err := rc.Create(ctx, svc); err != nil {
			return "", 0, fmt.Errorf("create oidc-test-client service: %w", err)
		}
	case err != nil:
		return "", 0, fmt.Errorf("get oidc-test-client service: %w", err)
	default:
		existingSvc.Spec.Selector = svc.Spec.Selector
		existingSvc.Spec.Ports = svc.Spec.Ports
		if err := rc.Update(ctx, existingSvc); err != nil {
			return "", 0, fmt.Errorf("update oidc-test-client service: %w", err)
		}
	}

	deploymentClient, _, err := resolveRunnerClient(ctx, r.Tracker, r.Client, tc.Namespace, nil, topo.Spec.ClusterRef)
	if err != nil {
		return "", 0, fmt.Errorf("resolve deployment cluster client: %w", err)
	}
	restConfig, err := resolveClusterRESTConfig(ctx, r.RESTConfig, r.Tracker, tc.Namespace, topo.Spec.ClusterRef)
	if err != nil {
		return "", 0, fmt.Errorf("resolve deployment cluster REST config: %w", err)
	}
	if err := applyBlueprint(ctx, restConfig, deploymentClient, topo, oauthProviderBlueprint(id, id, clientSecret, redirectURI)); err != nil {
		return "", 0, fmt.Errorf("apply oauth2 provider blueprint: %w", err)
	}

	return render.OIDCTestClientName(tc) + "." + tc.Namespace + ".svc.cluster.local", render.OIDCTestClientPort, nil
}
