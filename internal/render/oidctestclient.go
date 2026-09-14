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

package render

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// OIDCTestClientImage/OIDCTestClientPort match github.com/BeryJu/oidc-test-client's
// own README: a Go OIDC relying-party test client (uses
// github.com/coreos/go-oidc), OIDC_BIND defaulting to 0.0.0.0:9009, serving
// "/" (starts an authorization-code flow) and "/auth/callback" (receives
// it). The image tag is a placeholder, not yet confirmed against a real
// published registry - matching this project's own posture elsewhere
// (K6BrowserImage) for a dependency whose exact release artifact hasn't
// been independently verified.
const (
	OIDCTestClientImage = "ghcr.io/beryju/oidc-test-client:latest"
	OIDCTestClientPort  = 9009
)

// OIDCTestClientName is deterministic from the TestCase name - one
// oidc-test-client per oauth-oidc TestCase, owned by it (not the
// AuthentikDeployment it targets), the same scoping the k6 workload itself
// already uses (see testtemplate.Template.RequiresOAuthClient's own doc
// comment for why: keeps "what's being tested" out of the deployment).
func OIDCTestClientName(tc *benchv1alpha1.TestCase) string {
	return "oidc-test-client-" + tc.Name
}

// OIDCTestClientRootURL is this TestCase's own oidc-test-client's
// cluster-relative address - what OIDC_ROOT_URL is set to, and what the
// OAuth2Provider blueprint's own redirect_uri must point at
// (<rootURL>/auth/callback).
func OIDCTestClientRootURL(tc *benchv1alpha1.TestCase, namespace string) string {
	return fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", OIDCTestClientName(tc), namespace, OIDCTestClientPort)
}

func oidcTestClientLabels(tc *benchv1alpha1.TestCase) map[string]string {
	return map[string]string{"app": OIDCTestClientName(tc)}
}

// OIDCTestClientDeployment renders the oidc-test-client Deployment for one
// oauth-oidc TestCase. issuerURL is the target AuthentikDeployment's own
// OAuth2 issuer; clientID/clientSecret must match the OAuth2Provider
// blueprint applied against that same deployment (see
// internal/controller/oauthclient.go) - both sides are driven from the same
// values so they can never drift.
func OIDCTestClientDeployment(tc *benchv1alpha1.TestCase, namespace, issuerURL, clientID, clientSecret string) *appsv1.Deployment {
	name := OIDCTestClientName(tc)
	labels := oidcTestClientLabels(tc)
	return &appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{Kind: "Deployment", APIVersion: "apps/v1"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "oidc-test-client",
						Image: OIDCTestClientImage,
						Env: []corev1.EnvVar{
							{Name: "OIDC_CLIENT_ID", Value: clientID},
							{Name: "OIDC_CLIENT_SECRET", Value: clientSecret},
							{Name: "OIDC_PROVIDER", Value: issuerURL},
							{Name: "OIDC_ROOT_URL", Value: OIDCTestClientRootURL(tc, namespace)},
							{Name: "OIDC_BIND", Value: fmt.Sprintf("0.0.0.0:%d", OIDCTestClientPort)},
						},
						Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: OIDCTestClientPort}},
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{Path: "/health", Port: intstr.FromInt32(OIDCTestClientPort)},
							},
						},
					}},
				},
			},
		},
	}
}

// OIDCTestClientService is the ClusterIP Service OIDC_ROOT_URL and the k6
// oauth-oidc script both reach oidc-test-client through.
func OIDCTestClientService(tc *benchv1alpha1.TestCase, namespace string) *corev1.Service {
	name := OIDCTestClientName(tc)
	return &corev1.Service{
		TypeMeta:   metav1.TypeMeta{Kind: "Service", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: corev1.ServiceSpec{
			Selector: oidcTestClientLabels(tc),
			Ports: []corev1.ServicePort{
				{Name: "http", Port: OIDCTestClientPort, TargetPort: intstr.FromInt32(OIDCTestClientPort)},
			},
		},
	}
}
