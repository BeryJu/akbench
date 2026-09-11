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
	raw, err := HelmValues(topo, "the-secret-key", &PostgresConnection{Password: "pw"}, "http://pyroscope.example:4040", "http://tempo.example:4318", false)
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
// affinityOf decodes a server/worker values map's "affinity" key (always a
// *corev1.Affinity now, for every PlacementMode - see ServerWorkerAffinity)
// back into a plain map for assertions.
func affinityOf(t *testing.T, w map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(w["affinity"])
	if err != nil {
		t.Fatalf("affinity: marshal: %v", err)
	}
	if !json.Valid(raw) || string(raw) == "null" {
		t.Fatalf("affinity was not set")
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("affinity: unmarshal: %v", err)
	}
	return decoded
}

// TestHelmValuesDedicatedSetsServerWorkerAffinity guards that Dedicated mode
// now renders server.affinity/worker.affinity directly (a required
// anti-affinity term for this release's own pods, plus the
// always-present cross-topology term) instead of the chart's own
// global.affinity.podAntiAffinity preset, which server.affinity/
// worker.affinity being set at all makes the chart skip entirely.
func TestHelmValuesDedicatedSetsServerWorkerAffinity(t *testing.T) {
	topo := exampleTopology()
	topo.Spec.Placement = benchv1alpha1.PlacementSpec{Mode: benchv1alpha1.PlacementDedicated}
	raw, err := HelmValues(topo, "secret", &PostgresConnection{Password: "pw"}, "http://pyroscope.example:4040", "http://tempo.example:4318", false)
	if err != nil {
		t.Fatalf("HelmValues: %v", err)
	}
	values := decodeValues(t, raw.Raw)

	if _, ok := values.Global["affinity"]; ok {
		t.Error("global.affinity must not be set any more - the chart's preset mechanism is no longer used")
	}
	additionalLabels, _ := values.Global["additionalLabels"].(map[string]any)
	if additionalLabels["bench.goauthentik.io/topology"] != topo.Name {
		t.Errorf("global.additionalLabels[topology] = %v, want %q", additionalLabels, topo.Name)
	}
	for _, name := range []string{"server", "worker"} {
		w := values.Server
		if name == "worker" {
			w = values.Worker
		}
		decoded := affinityOf(t, w)
		podAntiAffinity, _ := decoded["podAntiAffinity"].(map[string]any)
		terms, _ := podAntiAffinity["requiredDuringSchedulingIgnoredDuringExecution"].([]any)
		if len(terms) != 2 {
			t.Errorf("%s.affinity: got %d required anti-affinity terms, want 2 (cross-topology + own-release)", name, len(terms))
		}
	}
}

// TestHelmValuesPacked guards that Packed mode's own pack-together
// affinity coexists with the always-present cross-topology anti-affinity
// term in the same server.affinity/worker.affinity value.
func TestHelmValuesPacked(t *testing.T) {
	topo := exampleTopology()
	topo.Spec.Placement = benchv1alpha1.PlacementSpec{Mode: benchv1alpha1.PlacementPacked}
	raw, err := HelmValues(topo, "secret", &PostgresConnection{Password: "pw"}, "http://pyroscope.example:4040", "http://tempo.example:4318", false)
	if err != nil {
		t.Fatalf("HelmValues: %v", err)
	}
	values := decodeValues(t, raw.Raw)

	for name, w := range map[string]map[string]any{"server": values.Server, "worker": values.Worker} {
		decoded := affinityOf(t, w)
		podAffinity, _ := decoded["podAffinity"].(map[string]any)
		if podAffinity == nil {
			t.Fatalf("%s.affinity.podAffinity missing: %v", name, decoded)
		}
		podAntiAffinity, _ := decoded["podAntiAffinity"].(map[string]any)
		terms, _ := podAntiAffinity["requiredDuringSchedulingIgnoredDuringExecution"].([]any)
		if len(terms) != 1 {
			t.Errorf("%s.affinity: got %d required anti-affinity terms, want 1 (cross-topology only)", name, len(terms))
		}
	}
}

// TestHelmValuesEmbeddedPostgresGetsCrossTopologyAffinity guards that the
// bundled postgresql subchart also gets the topology label and
// cross-topology anti-affinity - it's part of a topology's own footprint
// just as much as server/worker, so it must never share a node with
// another topology's pods either.
func TestHelmValuesEmbeddedPostgresGetsCrossTopologyAffinity(t *testing.T) {
	topo := exampleTopology()
	raw, err := HelmValues(topo, "secret", &PostgresConnection{Password: "pw"}, "http://pyroscope.example:4040", "http://tempo.example:4318", false)
	if err != nil {
		t.Fatalf("HelmValues: %v", err)
	}
	values := decodeValues(t, raw.Raw)

	commonLabels, _ := values.Postgres["commonLabels"].(map[string]any)
	if commonLabels["bench.goauthentik.io/topology"] != topo.Name {
		t.Errorf("postgresql.commonLabels[topology] = %v, want %q", commonLabels, topo.Name)
	}
	primary, _ := values.Postgres["primary"].(map[string]any)
	decoded := affinityOf(t, primary)
	podAntiAffinity, _ := decoded["podAntiAffinity"].(map[string]any)
	terms, _ := podAntiAffinity["requiredDuringSchedulingIgnoredDuringExecution"].([]any)
	if len(terms) != 1 {
		t.Errorf("postgresql.primary.affinity: got %d required anti-affinity terms, want 1", len(terms))
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
	}, "http://pyroscope.example:4040", "http://tempo.example:4318", false)
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
	raw, err := HelmValues(topo, "secret", nil, "http://pyroscope.example:4040", "http://tempo.example:4318", false)
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
	raw, err := HelmValues(topo, "secret", &PostgresConnection{Password: "pw"}, "http://pyroscope.example:4040", "http://tempo.example:4318", false)
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
	raw, err := HelmValues(topo, "secret", &PostgresConnection{Password: "pw"}, "http://pyroscope.example:4040", "http://tempo.example:4318", false)
	if err != nil {
		t.Fatalf("HelmValues: %v", err)
	}
	return decodeValues(t, raw.Raw)
}

// TestHelmValuesBlueprintVolumeSetsFsGroup guards against a real bug caught
// live: a freshly provisioned csi-incus block volume is root:root-owned
// (standard mkfs default), so the authentik container's non-root user
// (uid/gid 1000) can't write into it without an explicit fsGroup - without
// this, AuthentikTopologyReconciler.ApplyBlueprint's exec fails with
// "Permission denied" writing the fixtures blueprint.
func TestHelmValuesBlueprintVolumeSetsFsGroup(t *testing.T) {
	topo := exampleTopology()
	raw, err := HelmValues(topo, "secret", &PostgresConnection{Password: "pw"}, "http://pyroscope.example:4040", "http://tempo.example:4318", true)
	if err != nil {
		t.Fatalf("HelmValues: %v", err)
	}
	values := decodeValues(t, raw.Raw)

	sc, ok := values.Server["securityContext"].(map[string]any)
	if !ok {
		t.Fatalf("server.securityContext missing or wrong type: %v", values.Server["securityContext"])
	}
	if sc["fsGroup"] != float64(1000) {
		t.Errorf("server.securityContext.fsGroup = %v, want 1000", sc["fsGroup"])
	}
}
