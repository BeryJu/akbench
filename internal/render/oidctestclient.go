package render

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	benchv1alpha1 "github.com/authentik-labs/akbench/api/v1alpha1"
)

// OIDCTestClientImage/OIDCTestClientPort match github.com/BeryJu/oidc-test-client's
// own README: a Go OIDC relying-party test client (uses
// github.com/coreos/go-oidc), OIDC_BIND defaulting to 0.0.0.0:9009, serving
// "/" (starts an authorization-code flow) and "/auth/callback" (receives
// it). The tag is pinned to the newest version actually published to
// ghcr.io/beryju/oidc-test-client (confirmed directly against the registry's
// own tag list, not assumed) rather than left floating on :latest - bump
// deliberately.
const (
	OIDCTestClientImage = "ghcr.io/beryju/oidc-test-client:2.7.1"
	OIDCTestClientPort  = 9009
)

// OIDCTestClientName is deterministic from the TestCase name plus the
// specific AuthentikDeployment it targets - a TestCase driving more than
// one deployment needs one independent oidc-test-client per target, since
// each instance has exactly one issuer baked into its own env at creation
// time (OIDC_PROVIDER). Owned by the TestCase (not the AuthentikDeployment),
// the same scoping the k6 workload itself already uses (see
// TestCaseTemplateSpec.RequiresOAuthClient's own doc comment for why: keeps
// "what's being tested" out of the deployment).
func OIDCTestClientName(tc *benchv1alpha1.TestCase, deploymentName string) string {
	return "oidc-test-client-" + tc.Name + "-" + deploymentName
}

// OIDCTestClientRootURL is this TestCase/deployment pair's own
// oidc-test-client's cluster-relative address - what OIDC_ROOT_URL is set
// to, and what the OAuth2Provider blueprint's own redirect_uri must point at
// (<rootURL>/auth/callback).
func OIDCTestClientRootURL(tc *benchv1alpha1.TestCase, deploymentName, namespace string) string {
	return fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", OIDCTestClientName(tc, deploymentName), namespace, OIDCTestClientPort)
}

func oidcTestClientLabels(tc *benchv1alpha1.TestCase, deploymentName string) map[string]string {
	return map[string]string{labelKeyApp: OIDCTestClientName(tc, deploymentName)}
}

// OIDCTestClientDeployment renders the oidc-test-client Deployment for one
// oauth-oidc TestCase/deployment pair. issuerURL is that target
// AuthentikDeployment's own OAuth2 issuer; clientID/clientSecret must match
// the OAuth2Provider blueprint applied against that same deployment (see
// internal/controller/oauthclient.go) - both sides are driven from the same
// values so they can never drift.
func OIDCTestClientDeployment(tc *benchv1alpha1.TestCase, deploymentName, namespace, issuerURL, clientID, clientSecret string) *appsv1.Deployment {
	name := OIDCTestClientName(tc, deploymentName)
	labels := oidcTestClientLabels(tc, deploymentName)
	return &appsv1.Deployment{
		Kind: kindDeployment, APIVersion: apiVersionAppsV1,
		Name: name, Namespace: namespace, Labels: labels,
		Spec: appsv1.DeploymentSpec{
			Replicas: new(int32(1)),
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
							{Name: "OIDC_ROOT_URL", Value: OIDCTestClientRootURL(tc, deploymentName, namespace)},
							{Name: "OIDC_BIND", Value: fmt.Sprintf("0.0.0.0:%d", OIDCTestClientPort)},
						},
						Ports: []corev1.ContainerPort{{Name: portNameHTTP, ContainerPort: OIDCTestClientPort}},
						ReadinessProbe: &corev1.Probe{
							HTTPGet: &corev1.HTTPGetAction{Path: "/health", Port: intstr.FromInt32(OIDCTestClientPort)},
						},
					}},
				},
			},
		},
	}
}

// OIDCTestClientService is the ClusterIP Service OIDC_ROOT_URL and the k6
// oauth-oidc script both reach oidc-test-client through.
func OIDCTestClientService(tc *benchv1alpha1.TestCase, deploymentName, namespace string) *corev1.Service {
	name := OIDCTestClientName(tc, deploymentName)
	return &corev1.Service{
		Kind: kindService, APIVersion: apiVersionV1,
		Name: name, Namespace: namespace,
		Spec: corev1.ServiceSpec{
			Selector: oidcTestClientLabels(tc, deploymentName),
			Ports: []corev1.ServicePort{
				{Name: portNameHTTP, Port: OIDCTestClientPort, TargetPort: intstr.FromInt32(OIDCTestClientPort)},
			},
		},
	}
}
