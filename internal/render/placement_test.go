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
		// Packed doesn't use the preset mechanism at all - see PackedAffinity -
		// but it must not fall through to "hard" and fight the raw affinity.
		{benchv1alpha1.PlacementPacked, "none"},
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

func TestPackedAffinity(t *testing.T) {
	if aff := PackedAffinity(benchv1alpha1.PlacementSpec{Mode: benchv1alpha1.PlacementDefault}, "demo"); aff != nil {
		t.Errorf("PackedAffinity in Default mode = %v, want nil", aff)
	}
	if aff := PackedAffinity(benchv1alpha1.PlacementSpec{Mode: benchv1alpha1.PlacementDedicated}, "demo"); aff != nil {
		t.Errorf("PackedAffinity in Dedicated mode = %v, want nil", aff)
	}

	aff := PackedAffinity(benchv1alpha1.PlacementSpec{Mode: benchv1alpha1.PlacementPacked}, "demo")
	if aff == nil || aff.PodAffinity == nil {
		t.Fatalf("PackedAffinity in Packed mode = %v, want a PodAffinity", aff)
	}
	terms := aff.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution
	if len(terms) != 1 {
		t.Fatalf("got %d required terms, want 1", len(terms))
	}
	if got := terms[0].LabelSelector.MatchLabels["app.kubernetes.io/instance"]; got != "demo" {
		t.Errorf("required term matches instance=%q, want demo", got)
	}
	if terms[0].TopologyKey != "kubernetes.io/hostname" {
		t.Errorf("required term topologyKey = %q, want kubernetes.io/hostname", terms[0].TopologyKey)
	}
}
