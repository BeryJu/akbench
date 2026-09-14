package render

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// requiredAntiAffinityTerms is a small test helper returning the required
// pod anti-affinity terms of an affinity, or nil if there are none.
func requiredAntiAffinityTerms(aff *corev1.Affinity) []corev1.PodAffinityTerm {
	if aff == nil || aff.PodAntiAffinity == nil {
		return nil
	}
	return aff.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution
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

// TestServerWorkerAffinityAlwaysHasCrossDeploymentTerm guards the actual
// point of this function: regardless of PlacementMode, two deployments must
// never be able to share a node.
func TestServerWorkerAffinityAlwaysHasCrossDeploymentTerm(t *testing.T) {
	modes := []benchv1alpha1.PlacementMode{
		benchv1alpha1.PlacementDefault, "", benchv1alpha1.PlacementDedicated, benchv1alpha1.PlacementPacked,
	}
	for _, mode := range modes {
		aff := ServerWorkerAffinity(benchv1alpha1.PlacementSpec{Mode: mode}, "demo", "topo-a")
		terms := requiredAntiAffinityTerms(aff)
		found := false
		for _, term := range terms {
			if term.TopologyKey != "kubernetes.io/hostname" || term.LabelSelector == nil {
				continue
			}
			exprs := term.LabelSelector.MatchExpressions
			if len(exprs) == 2 && exprs[0].Key == "bench.goauthentik.io/deployment" &&
				exprs[1].Values != nil && len(exprs[1].Values) == 1 && exprs[1].Values[0] == "topo-a" {
				found = true
			}
		}
		if !found {
			t.Errorf("mode=%q: no cross-deployment required anti-affinity term found in %+v", mode, terms)
		}
	}
}

func TestServerWorkerAffinityDedicatedAddsOwnReleaseTerm(t *testing.T) {
	aff := ServerWorkerAffinity(benchv1alpha1.PlacementSpec{Mode: benchv1alpha1.PlacementDedicated}, "demo", "topo-a")
	terms := requiredAntiAffinityTerms(aff)
	if len(terms) != 2 {
		t.Fatalf("Dedicated mode: got %d required anti-affinity terms, want 2 (cross-deployment + own-release)", len(terms))
	}
	found := false
	for _, term := range terms {
		if term.LabelSelector != nil && term.LabelSelector.MatchLabels["app.kubernetes.io/instance"] == "demo" {
			found = true
		}
	}
	if !found {
		t.Errorf("Dedicated mode: no own-release (app.kubernetes.io/instance=demo) required term found in %+v", terms)
	}
}

func TestServerWorkerAffinityPackedAddsOwnReleasePodAffinity(t *testing.T) {
	aff := ServerWorkerAffinity(benchv1alpha1.PlacementSpec{Mode: benchv1alpha1.PlacementPacked}, "demo", "topo-a")
	if aff.PodAffinity == nil {
		t.Fatalf("Packed mode: want a PodAffinity, got nil")
	}
	terms := aff.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution
	if len(terms) != 1 {
		t.Fatalf("got %d required affinity terms, want 1", len(terms))
	}
	if got := terms[0].LabelSelector.MatchLabels["app.kubernetes.io/instance"]; got != "demo" {
		t.Errorf("required term matches instance=%q, want demo", got)
	}
	if terms[0].TopologyKey != "kubernetes.io/hostname" {
		t.Errorf("required term topologyKey = %q, want kubernetes.io/hostname", terms[0].TopologyKey)
	}
	// Packed mode must still refuse to share a node with another deployment.
	if terms := requiredAntiAffinityTerms(aff); len(terms) != 1 {
		t.Errorf("Packed mode: got %d required anti-affinity terms, want 1 (cross-deployment only)", len(terms))
	}
}

func TestServerWorkerAffinityDefaultHasOnlyCrossDeploymentTerm(t *testing.T) {
	aff := ServerWorkerAffinity(benchv1alpha1.PlacementSpec{Mode: benchv1alpha1.PlacementDefault}, "demo", "topo-a")
	if aff.PodAffinity != nil {
		t.Errorf("Default mode: want no PodAffinity, got %+v", aff.PodAffinity)
	}
	if terms := requiredAntiAffinityTerms(aff); len(terms) != 1 {
		t.Errorf("Default mode: got %d required anti-affinity terms, want 1 (cross-deployment only)", len(terms))
	}
}
