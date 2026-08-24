package render

import (
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

func exampleTopology() *benchv1alpha1.AuthentikTopology {
	return &benchv1alpha1.AuthentikTopology{
		ObjectMeta: metav1.ObjectMeta{Name: "authentik-1"},
		Spec: benchv1alpha1.AuthentikTopologySpec{
			Namespace: "authentik-1",
			Server:    benchv1alpha1.WorkloadSpec{Replicas: 2},
			Worker:    benchv1alpha1.WorkloadSpec{Replicas: 1},
			Postgres:  benchv1alpha1.PostgresSpec{Mode: benchv1alpha1.PostgresEmbedded},
		},
	}
}

func decodeValues(t *testing.T, raw json.RawMessage) AuthentikTopologyHelmValues {
	t.Helper()
	var v AuthentikTopologyHelmValues
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("unmarshal values: %v", err)
	}
	return v
}

func TestHelmValuesEmbedded(t *testing.T) {
	topo := exampleTopology()
	raw, err := HelmValues(topo, "authentik-1-authentik-config", &PostgresConnection{Password: "pw"})
	if err != nil {
		t.Fatalf("HelmValues: %v", err)
	}
	values := decodeValues(t, raw.Raw)

	if values.Server["replicas"] != float64(2) {
		t.Errorf("server.replicas = %v, want 2", values.Server["replicas"])
	}
	if values.Postgres["enabled"] != true {
		t.Errorf("postgresql.enabled = %v, want true", values.Postgres["enabled"])
	}
	auth, _ := values.Postgres["auth"].(map[string]any)
	if auth["password"] != "pw" {
		t.Errorf("postgresql.auth.password = %v, want pw", auth["password"])
	}
	akPg, _ := values.Authentik["postgresql"].(map[string]any)
	if akPg["password"] != "pw" {
		t.Errorf("authentik.postgresql.password = %v, want pw (must match the bundled subchart's own password)", akPg["password"])
	}
	existingSecret, _ := values.Authentik["existingSecret"].(map[string]any)
	if existingSecret["secretName"] != "authentik-1-authentik-config" {
		t.Errorf("authentik.existingSecret.secretName = %v, want authentik-1-authentik-config", existingSecret["secretName"])
	}
}

func TestHelmValuesExternalDisablesBundledPostgres(t *testing.T) {
	topo := exampleTopology()
	topo.Spec.Postgres = benchv1alpha1.PostgresSpec{
		Mode:     benchv1alpha1.PostgresExternal,
		External: &benchv1alpha1.ExternalPostgresSpec{},
	}
	raw, err := HelmValues(topo, "secret", &PostgresConnection{
		Host: "pg.example.com", Port: 5432, Database: "authentik", User: "authentik", Password: "s3cret",
	})
	if err != nil {
		t.Fatalf("HelmValues: %v", err)
	}
	values := decodeValues(t, raw.Raw)

	if values.Postgres["enabled"] != false {
		t.Errorf("postgresql.enabled = %v, want false for External mode", values.Postgres["enabled"])
	}
	if _, hasAuth := values.Postgres["auth"]; hasAuth {
		t.Error("postgresql.auth should not be set for External mode")
	}
	akPg, _ := values.Authentik["postgresql"].(map[string]any)
	if akPg["host"] != "pg.example.com" || akPg["password"] != "s3cret" {
		t.Errorf("authentik.postgresql = %+v, want the external connection", akPg)
	}
}

func TestHelmReleaseUsesExplicitReleaseName(t *testing.T) {
	topo := exampleTopology()
	raw, err := HelmValues(topo, "secret", nil)
	if err != nil {
		t.Fatalf("HelmValues: %v", err)
	}
	hr := HelmRelease(topo, "", raw)

	// Flux's own default release-name composition is "[TargetNamespace-]Name",
	// which would double up here since both are "authentik-1" - pinning
	// ReleaseName explicitly is what keeps ServiceEndpoint's "<release>-server"
	// naming correct.
	if hr.Spec.ReleaseName != "authentik-1" {
		t.Errorf("releaseName = %q, want %q", hr.Spec.ReleaseName, "authentik-1")
	}
	if got, want := ServiceEndpoint(topo), "authentik-1-server.authentik-1.svc.cluster.local:80"; got != want {
		t.Errorf("ServiceEndpoint() = %q, want %q", got, want)
	}
}
