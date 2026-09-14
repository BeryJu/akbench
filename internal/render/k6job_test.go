package render

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// TestPodTemplateNeverSharesNodeWithItsDeployment guards the k6 runner's own
// required anti-affinity: it must never land on the same node as the
// deployment it's driving (deploymentRef.name, not TargetName, which is only a
// metric label and can differ), so the load generator never competes with
// the system under test for the same node's CPU.
func TestPodTemplateNeverSharesNodeWithItsDeployment(t *testing.T) {
	p := K6WorkloadParams{
		TestCase: &benchv1alpha1.TestCase{
			ObjectMeta: metav1.ObjectMeta{Name: "login-a"},
			Spec: benchv1alpha1.TestCaseSpec{
				DeploymentRef: corev1.LocalObjectReference{Name: "topo-a"},
				Profile:       benchv1alpha1.ProfileBurn,
			},
		},
		TargetName: "some-other-display-name",
	}
	tmpl := p.PodTemplate()

	if got := tmpl.ObjectMeta.Labels[benchv1alpha1.LabelDeployment]; got != "topo-a" {
		t.Errorf("pod template deployment label = %q, want topo-a", got)
	}
	if tmpl.Spec.Affinity == nil || tmpl.Spec.Affinity.PodAntiAffinity == nil {
		t.Fatal("pod template has no PodAntiAffinity")
	}
	terms := tmpl.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution
	if len(terms) != 1 {
		t.Fatalf("got %d required anti-affinity terms, want 1", len(terms))
	}
	if got := terms[0].LabelSelector.MatchLabels[benchv1alpha1.LabelDeployment]; got != "topo-a" {
		t.Errorf("required term matches deployment=%q, want topo-a", got)
	}
	if terms[0].TopologyKey != "kubernetes.io/hostname" {
		t.Errorf("required term topologyKey = %q, want kubernetes.io/hostname", terms[0].TopologyKey)
	}
}
