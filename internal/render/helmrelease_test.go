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
	raw, err := HelmValues(topo, "the-secret-key", &PostgresConnection{Password: "pw"}, "http://pyroscope.example:4040", "http://tempo.example:4318")
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
	// authentik.secret_key as a literal value, not authentik.existingSecret:
	// existingSecret makes the chart skip rendering its own config Secret
	// entirely, silently dropping postgresql.*/log_level/error_reporting too
	// (verified against a live install of this exact bug).
	if values.Authentik["secret_key"] != "the-secret-key" {
		t.Errorf("authentik.secret_key = %v, want the-secret-key", values.Authentik["secret_key"])
	}
	if _, ok := values.Authentik["existingSecret"]; ok {
		t.Error("authentik.existingSecret must not be set")
	}
}

// TestHelmValuesPlacementUsesGlobalAffinityPreset guards against regressing
// into setting server.affinity/worker.affinity directly: verified against a
// live chart install that those fields are a raw corev1.Affinity override in
// this chart (not the preset mechanism), so a preset string there fails with
// "cannot unmarshal string into Go struct field ... PodAntiAffinity".
func TestHelmValuesPlacementUsesGlobalAffinityPreset(t *testing.T) {
	topo := exampleTopology()
	topo.Spec.Placement = benchv1alpha1.PlacementSpec{Mode: benchv1alpha1.PlacementDedicated}
	raw, err := HelmValues(topo, "secret", &PostgresConnection{Password: "pw"}, "http://pyroscope.example:4040", "http://tempo.example:4318")
	if err != nil {
		t.Fatalf("HelmValues: %v", err)
	}
	values := decodeValues(t, raw.Raw)

	if _, ok := values.Server["affinity"]; ok {
		t.Error("server.affinity must not be set - it's a raw corev1.Affinity override in this chart, not the preset mechanism")
	}
	if _, ok := values.Worker["affinity"]; ok {
		t.Error("worker.affinity must not be set - it's a raw corev1.Affinity override in this chart, not the preset mechanism")
	}
	affinity, _ := values.Global["affinity"].(map[string]any)
	if affinity["podAntiAffinity"] != "hard" {
		t.Errorf("global.affinity.podAntiAffinity = %v, want hard", affinity["podAntiAffinity"])
	}
}

// TestHelmValuesPacked guards the opposite case from
// TestHelmValuesPlacementUsesGlobalAffinityPreset: Packed mode has no preset
// to reuse, so it's the one mode that *must* set server.affinity/
// worker.affinity, keyed on the pinned release name so it also reaches the
// bundled postgres pod (see PackedAffinity's doc comment).
func TestHelmValuesPacked(t *testing.T) {
	topo := exampleTopology()
	topo.Spec.Placement = benchv1alpha1.PlacementSpec{Mode: benchv1alpha1.PlacementPacked}
	raw, err := HelmValues(topo, "secret", &PostgresConnection{Password: "pw"}, "http://pyroscope.example:4040", "http://tempo.example:4318")
	if err != nil {
		t.Fatalf("HelmValues: %v", err)
	}
	values := decodeValues(t, raw.Raw)

	affinity, _ := values.Global["affinity"].(map[string]any)
	if affinity["podAntiAffinity"] != "none" {
		t.Errorf("global.affinity.podAntiAffinity = %v, want none", affinity["podAntiAffinity"])
	}
	for name, w := range map[string]map[string]any{"server": values.Server, "worker": values.Worker} {
		raw, err := json.Marshal(w["affinity"])
		if err != nil {
			t.Fatalf("%s.affinity: marshal: %v", name, err)
		}
		if !json.Valid(raw) || string(raw) == "null" {
			t.Fatalf("%s.affinity was not set", name)
		}
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("%s.affinity: unmarshal: %v", name, err)
		}
		podAffinity, _ := decoded["podAffinity"].(map[string]any)
		if podAffinity == nil {
			t.Fatalf("%s.affinity.podAffinity missing: %v", name, decoded)
		}
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
	}, "http://pyroscope.example:4040", "http://tempo.example:4318")
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
	raw, err := HelmValues(topo, "secret", nil, "http://pyroscope.example:4040", "http://tempo.example:4318")
	if err != nil {
		t.Fatalf("HelmValues: %v", err)
	}
	hr := HelmRelease(topo, "", raw)

	// Flux's own default release-name composition is "[TargetNamespace-]Name",
	// which would double up here since both are "authentik-1" - pinning
	// ReleaseName explicitly is what keeps ServiceEndpoint's
	// "<release>-authentik-server" naming correct.
	if hr.Spec.ReleaseName != "authentik-1" {
		t.Errorf("releaseName = %q, want %q", hr.Spec.ReleaseName, "authentik-1")
	}
	if got, want := ServiceEndpoint(topo), "authentik-1-authentik-server.authentik-1.svc.cluster.local:80"; got != want {
		t.Errorf("ServiceEndpoint() = %q, want %q", got, want)
	}
}

func envValue(t *testing.T, w map[string]any, name string) (string, bool) {
	t.Helper()
	raw, err := json.Marshal(w["env"])
	if err != nil {
		t.Fatalf("marshal env: %v", err)
	}
	var entries []map[string]string
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("unmarshal env: %v", err)
	}
	for _, e := range entries {
		if e["name"] == name {
			return e["value"], true
		}
	}
	return "", false
}

// TestHelmValuesPyroscopeAlwaysSet guards the chart's actual values.yaml
// having no pyroscope key at all (checked against a live pulled chart
// tarball) - profiling only reaches authentik via a raw env var, on both
// workloads, unconditionally (no separate "pyroscope enabled" toggle exists,
// matching the Ansible harness this project replaces).
func TestHelmValuesPyroscopeAlwaysSet(t *testing.T) {
	topo := exampleTopology()
	raw, err := HelmValues(topo, "secret", &PostgresConnection{Password: "pw"}, "http://pyroscope.example:4040", "http://tempo.example:4318")
	if err != nil {
		t.Fatalf("HelmValues: %v", err)
	}
	values := decodeValues(t, raw.Raw)

	for name, w := range map[string]map[string]any{"server": values.Server, "worker": values.Worker} {
		got, ok := envValue(t, w, "AUTHENTIK_PYROSCOPE_HOST")
		if !ok || got != "http://pyroscope.example:4040" {
			t.Errorf("%s env AUTHENTIK_PYROSCOPE_HOST = %q, ok=%v, want http://pyroscope.example:4040", name, got, ok)
		}
	}
}

// TestHelmValuesTracingOnlyWhenErrorReportingEnabled guards the chart's
// error_reporting section having no OTLP endpoint/sample-rate key either
// (only enabled/environment/send_pii) - both go in as raw env vars, and only
// when errorReporting.enabled is set, matching env.j2's {% if
// authentik_error_reporting %} guard in the Ansible harness this project
// replaces.
func TestHelmValuesTracingOnlyWhenErrorReportingEnabled(t *testing.T) {
	topo := exampleTopology()
	if _, ok := envValue(t, mustValues(t, topo).Server, "AUTHENTIK_ERROR_REPORTING__OTEL_ENDPOINT"); ok {
		t.Error("AUTHENTIK_ERROR_REPORTING__OTEL_ENDPOINT must not be set when errorReporting.enabled is false")
	}

	topo.Spec.Authentik.ErrorReporting = benchv1alpha1.ErrorReportingSpec{Enabled: true, SampleRate: "0.25"}
	values := mustValues(t, topo)

	er, _ := values.Authentik["error_reporting"].(map[string]any)
	if er["enabled"] != true {
		t.Errorf("authentik.error_reporting.enabled = %v, want true", er["enabled"])
	}
	if er["environment"] != "authentik-1" {
		t.Errorf("authentik.error_reporting.environment = %v, want the topology's target name", er["environment"])
	}
	for name, w := range map[string]map[string]any{"server": values.Server, "worker": values.Worker} {
		if got, ok := envValue(t, w, "AUTHENTIK_ERROR_REPORTING__OTEL_ENDPOINT"); !ok || got != "http://tempo.example:4318" {
			t.Errorf("%s env AUTHENTIK_ERROR_REPORTING__OTEL_ENDPOINT = %q, ok=%v", name, got, ok)
		}
		if got, ok := envValue(t, w, "AUTHENTIK_ERROR_REPORTING__SAMPLE_RATE"); !ok || got != "0.25" {
			t.Errorf("%s env AUTHENTIK_ERROR_REPORTING__SAMPLE_RATE = %q, ok=%v, want 0.25", name, got, ok)
		}
	}
}

func mustValues(t *testing.T, topo *benchv1alpha1.AuthentikTopology) AuthentikTopologyHelmValues {
	t.Helper()
	raw, err := HelmValues(topo, "secret", &PostgresConnection{Password: "pw"}, "http://pyroscope.example:4040", "http://tempo.example:4318")
	if err != nil {
		t.Fatalf("HelmValues: %v", err)
	}
	return decodeValues(t, raw.Raw)
}
