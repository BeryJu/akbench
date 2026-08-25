package render

import (
	"encoding/json"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func mustJSON(t *testing.T, v any) *apiextensionsv1.JSON {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return &apiextensionsv1.JSON{Raw: raw}
}

func TestMergeExtraValuesDeepMerges(t *testing.T) {
	base := mustJSON(t, map[string]any{
		"authentik": map[string]any{"log_level": "info", "secret_key": "s3cret"},
		"server":    map[string]any{"replicas": 2},
	})

	merged, err := MergeExtraValues(base, "authentik:\n  log_level: debug\n  geoip:\n    enabled: true\n")
	if err != nil {
		t.Fatalf("MergeExtraValues: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(merged.Raw, &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	authentik := got["authentik"].(map[string]any)
	if authentik["log_level"] != "debug" {
		t.Errorf("log_level = %v, want overlay to win", authentik["log_level"])
	}
	if authentik["secret_key"] != "s3cret" {
		t.Errorf("secret_key = %v, want base value preserved", authentik["secret_key"])
	}
	geoip := authentik["geoip"].(map[string]any)
	if geoip["enabled"] != true {
		t.Errorf("geoip.enabled = %v, want true from overlay", geoip["enabled"])
	}
	server := got["server"].(map[string]any)
	if server["replicas"] != float64(2) {
		t.Errorf("server.replicas = %v, want base value preserved", server["replicas"])
	}
}

func TestMergeExtraValuesEmptyIsNoop(t *testing.T) {
	base := mustJSON(t, map[string]any{"authentik": map[string]any{"log_level": "info"}})
	merged, err := MergeExtraValues(base, "")
	if err != nil {
		t.Fatalf("MergeExtraValues: %v", err)
	}
	if string(merged.Raw) != string(base.Raw) {
		t.Errorf("empty overlay changed the base values: %s", merged.Raw)
	}
}

func TestMergeExtraValuesRejectsInvalidYAML(t *testing.T) {
	base := mustJSON(t, map[string]any{})
	if _, err := MergeExtraValues(base, "not: valid: yaml: [1,2"); err == nil {
		t.Fatal("expected an error for invalid YAML, got nil")
	}
}
