package render

import (
	"testing"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

func TestAffinityPreset(t *testing.T) {
	cases := []struct {
		mode benchv1alpha1.PlacementMode
		want string
	}{
		{benchv1alpha1.PlacementDefault, "none"},
		{"", "none"}, // zero value behaves like Default
		{benchv1alpha1.PlacementDedicated, "hard"},
	}
	for _, c := range cases {
		got := AffinityPreset(benchv1alpha1.PlacementSpec{Mode: c.mode})
		if got != c.want {
			t.Errorf("AffinityPreset(mode=%q) = %q, want %q", c.mode, got, c.want)
		}
	}
}

func TestNodeSelectorAndTolerationsOnlyApplyInDedicatedMode(t *testing.T) {
	spec := benchv1alpha1.PlacementSpec{
		Mode:         benchv1alpha1.PlacementDefault,
		NodeSelector: map[string]string{"pool": "benchmark"},
	}
	if sel := NodeSelector(spec); sel != nil {
		t.Errorf("NodeSelector in Default mode = %v, want nil", sel)
	}

	spec.Mode = benchv1alpha1.PlacementDedicated
	if sel := NodeSelector(spec); sel["pool"] != "benchmark" {
		t.Errorf("NodeSelector in Dedicated mode = %v, want pool=benchmark", sel)
	}
}
