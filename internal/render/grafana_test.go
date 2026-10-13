package render

import (
	"testing"

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
