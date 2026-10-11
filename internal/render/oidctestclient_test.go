package render

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

func TestOIDCTestClientObjects(t *testing.T) {
	tc := &benchv1alpha1.TestCase{ObjectMeta: metav1.ObjectMeta{Name: "oauth-test", Namespace: "default"}}
	dep := OIDCTestClientDeployment(tc, "topo-a", "default", "http://authentik.example/application/o/oauth-test/", "client-id-123", "sekrit")
	svc := OIDCTestClientService(tc, "topo-a", "default")

	wantName := "oidc-test-client-oauth-test-topo-a"
	if dep.Name != wantName {
		t.Errorf("Deployment name = %q, want %q", dep.Name, wantName)
	}
	if svc.Name != wantName {
		t.Errorf("Service name = %q, want %q", svc.Name, wantName)
	}
	if dep.Spec.Selector.MatchLabels["app"] != wantName || svc.Spec.Selector["app"] != wantName {
		t.Error("Deployment/Service selector must agree on the same app label")
	}

	env := map[string]string{}
	for _, e := range dep.Spec.Template.Spec.Containers[0].Env {
		env[e.Name] = e.Value
	}
	if env["OIDC_CLIENT_ID"] != "client-id-123" {
		t.Errorf("OIDC_CLIENT_ID = %q, want client-id-123", env["OIDC_CLIENT_ID"])
	}
	if env["OIDC_CLIENT_SECRET"] != "sekrit" {
		t.Errorf("OIDC_CLIENT_SECRET = %q, want sekrit", env["OIDC_CLIENT_SECRET"])
	}
	if env["OIDC_PROVIDER"] != "http://authentik.example/application/o/oauth-test/" {
		t.Errorf("OIDC_PROVIDER = %q, want the issuer URL", env["OIDC_PROVIDER"])
	}
	wantRootURL := "http://oidc-test-client-oauth-test-topo-a.default.svc.cluster.local:9009"
	if env["OIDC_ROOT_URL"] != wantRootURL {
		t.Errorf("OIDC_ROOT_URL = %q, want %q", env["OIDC_ROOT_URL"], wantRootURL)
	}
	if got := OIDCTestClientRootURL(tc, "topo-a", "default"); got != wantRootURL {
		t.Errorf("OIDCTestClientRootURL = %q, want %q", got, wantRootURL)
	}

	if len(svc.Spec.Ports) != 1 || svc.Spec.Ports[0].Port != OIDCTestClientPort {
		t.Errorf("Service port = %+v, want a single port %d", svc.Spec.Ports, OIDCTestClientPort)
	}
}
