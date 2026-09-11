package render

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

func TestGrafanaProxyPathPrefix(t *testing.T) {
	wc := &benchv1alpha1.WorkloadCluster{ObjectMeta: metav1.ObjectMeta{Name: "default-dev-vm-incus"}}
	got := grafanaProxyPathPrefix(wc)
	want := "/clusters/default-dev-vm-incus-admin@default-dev-vm-incus/api/v1/namespaces/bench-system/services/grafana:3000/proxy/"
	if got != want {
		t.Errorf("grafanaProxyPathPrefix = %q, want %q", got, want)
	}
}

func TestGrafanaObjects(t *testing.T) {
	wc := &benchv1alpha1.WorkloadCluster{ObjectMeta: metav1.ObjectMeta{Name: "test-cluster"}}
	objs := GrafanaObjects(wc)

	found := map[string]int{}
	var deployment *appsv1.Deployment
	var svc *corev1.Service
	var dashboardsCM *corev1.ConfigMap
	for _, obj := range objs {
		found[obj.GetObjectKind().GroupVersionKind().Kind]++
		if obj.GetName() == "" {
			t.Errorf("object of kind %T has no name", obj)
		}
		if d, ok := obj.(*appsv1.Deployment); ok {
			deployment = d
		}
		if s, ok := obj.(*corev1.Service); ok {
			svc = s
		}
		if cm, ok := obj.(*corev1.ConfigMap); ok && cm.Name == grafanaDashboardsConfigMapName {
			dashboardsCM = cm
		}
	}
	if found["Namespace"] != 1 || found["ConfigMap"] != 3 || found["Deployment"] != 1 || found["Service"] != 1 {
		t.Fatalf("unexpected object counts: %v", found)
	}

	if svc.Spec.Ports[0].Port != GrafanaPort {
		t.Errorf("service port = %d, want %d", svc.Spec.Ports[0].Port, GrafanaPort)
	}

	env := map[string]string{}
	for _, e := range deployment.Spec.Template.Spec.Containers[0].Env {
		env[e.Name] = e.Value
	}
	if env["GF_AUTH_ANONYMOUS_ENABLED"] != "true" {
		t.Errorf("GF_AUTH_ANONYMOUS_ENABLED = %q, want true", env["GF_AUTH_ANONYMOUS_ENABLED"])
	}
	if env["GF_AUTH_ANONYMOUS_ORG_ROLE"] != "Admin" {
		t.Errorf("GF_AUTH_ANONYMOUS_ORG_ROLE = %q, want Admin", env["GF_AUTH_ANONYMOUS_ORG_ROLE"])
	}
	wantRootURL := "/clusters/test-cluster-admin@test-cluster/api/v1/namespaces/bench-system/services/grafana:3000/proxy/"
	if env["GF_SERVER_ROOT_URL"] != wantRootURL {
		t.Errorf("GF_SERVER_ROOT_URL = %q, want %q", env["GF_SERVER_ROOT_URL"], wantRootURL)
	}
	if env["GF_SERVER_SERVE_FROM_SUB_PATH"] != "true" {
		t.Errorf("GF_SERVER_SERVE_FROM_SUB_PATH = %q, want true", env["GF_SERVER_SERVE_FROM_SUB_PATH"])
	}

	for _, name := range []string{"compare-tests.json", "k6.json", "authentik.json", "postgres.json", "nodes.json"} {
		if dashboardsCM.Data[name] == "" {
			t.Errorf("dashboards ConfigMap missing/empty %q", name)
		}
	}
}
