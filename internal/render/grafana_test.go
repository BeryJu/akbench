package render

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

func TestGrafanaProxyPathPrefix(t *testing.T) {
	wc := &benchv1alpha1.WorkloadCluster{Name: "default-dev-vm-incus"}
	got := GrafanaProxyPathPrefix(wc)
	want := "/grafana-proxy/default-dev-vm-incus/"
	if got != want {
		t.Errorf("grafanaProxyPathPrefix = %q, want %q", got, want)
	}
}

func TestGrafanaObjects(t *testing.T) {
	wc := &benchv1alpha1.WorkloadCluster{Name: "test-cluster"}
	objs := GrafanaObjects(wc)

	found := map[string]int{}
	var deployment *appsv1.Deployment
	var svc, nodePortSvc *corev1.Service
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
			if s.Name == GrafanaNodePortServiceName {
				nodePortSvc = s
			} else {
				svc = s
			}
		}
		if cm, ok := obj.(*corev1.ConfigMap); ok && cm.Name == grafanaDashboardsConfigMapName {
			dashboardsCM = cm
		}
	}
	if found["Namespace"] != 1 || found["ConfigMap"] != 3 || found["Deployment"] != 1 || found["Service"] != 2 || found["ServiceMonitor"] != 1 {
		t.Fatalf("unexpected object counts: %v", found)
	}

	if svc.Spec.Ports[0].Port != GrafanaPort {
		t.Errorf("service port = %d, want %d", svc.Spec.Ports[0].Port, GrafanaPort)
	}
	// The ClusterIP Service needs .metadata.labels (not just .spec.selector)
	// for the ServiceMonitor above to find it - a ServiceMonitor's own
	// spec.selector matches the target Service object's labels.
	if svc.Labels["app"] != GrafanaServiceName {
		t.Errorf("grafana Service.Labels = %v, want app=%q so its ServiceMonitor can find it", svc.Labels, GrafanaServiceName)
	}
	if nodePortSvc.Spec.Type != corev1.ServiceTypeNodePort {
		t.Errorf("nodeport service type = %q, want NodePort", nodePortSvc.Spec.Type)
	}
	if nodePortSvc.Spec.Ports[0].NodePort != int32(GrafanaNodePort) {
		t.Errorf("nodeport = %d, want %d", nodePortSvc.Spec.Ports[0].NodePort, GrafanaNodePort)
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
	// Without this, Grafana sends "X-Frame-Options: deny" on every response
	// (confirmed live against the real image) and the web UI's
	// iframe gets silently blocked by the browser - not a CORS issue, no
	// Access-Control-* header involved at all.
	if env["GF_SECURITY_ALLOW_EMBEDDING"] != "true" {
		t.Errorf("GF_SECURITY_ALLOW_EMBEDDING = %q, want true", env["GF_SECURITY_ALLOW_EMBEDDING"])
	}
	wantRootURL := "/grafana-proxy/test-cluster/"
	if env["GF_SERVER_ROOT_URL"] != wantRootURL {
		t.Errorf("GF_SERVER_ROOT_URL = %q, want %q", env["GF_SERVER_ROOT_URL"], wantRootURL)
	}
	// "true" is correct here: cmd/apiserver's own reverse proxy
	// (internal/api/grafana_proxy.go) deliberately preserves the request
	// path rather than stripping it - see the doc comment on this env var
	// in grafana.go.
	if env["GF_SERVER_SERVE_FROM_SUB_PATH"] != "true" {
		t.Errorf("GF_SERVER_SERVE_FROM_SUB_PATH = %q, want true", env["GF_SERVER_SERVE_FROM_SUB_PATH"])
	}

	for _, name := range []string{"compare-tests.json", "k6.json", "authentik.json", "postgres.json", "nodes.json"} {
		if dashboardsCM.Data[name] == "" {
			t.Errorf("dashboards ConfigMap missing/empty %q", name)
		}
	}
}
