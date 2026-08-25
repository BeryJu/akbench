// Package render turns bench.goauthentik.io CRs into the Kubernetes/Flux/CNPG
// objects that implement them.
package render

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// AffinityPreset returns the authentik chart's own affinity-preset value for
// global.affinity.podAntiAffinity (one of "none", "soft", "hard" - see
// charts.goauthentik.io/authentik's _helpers.tpl, "authentik.affinity") for
// the given placement mode.
//
// This must go under global.affinity, not server.affinity/worker.affinity:
// the per-component fields are a *different* knob that, if set to anything
// non-empty at all, the chart uses verbatim as the Pod's real
// corev1.Affinity - a preset string like "hard" there fails to unmarshal.
// global.affinity is the only place the preset mechanism itself lives, and
// it applies to both server and worker uniformly, which is fine since
// Dedicated/Default both apply symmetrically to both.
//
// Dedicated maps to "hard" (required pod anti-affinity, one pod per node -
// the topology key the chart uses for "hard" is fixed at
// kubernetes.io/hostname, matching PlacementSpec's own default;
// PlacementSpec.TopologyKey has no effect here since the chart doesn't
// expose it as a value - use ExtraValuesRef for a custom key). Default maps
// to "none": the chart's own root default is "soft", which would already
// bias pods apart, so Default must say "none" explicitly to let pods land on
// the same node (the noisy-neighbor case the benchmark compares against).
func AffinityPreset(spec benchv1alpha1.PlacementSpec) string {
	if spec.Mode == benchv1alpha1.PlacementDedicated {
		return "hard"
	}
	return "none"
}

// PackedAffinity returns the raw pod affinity Packed mode needs - the
// chart's own preset mechanism (AffinityPreset/authentik.affinity) only ever
// generates *anti*-affinity, there is no "pack together" preset to reuse, so
// this goes through server.affinity/worker.affinity's verbatim-Affinity
// escape hatch instead (see the doc comment on AffinityPreset).
//
// It requires scheduling onto a node that already has a pod carrying this
// release's app.kubernetes.io/instance label - a label every pod the
// authentik chart or its bundled postgresql subchart produces carries
// (confirmed against both charts' own label helpers), so this reaches the
// bundled postgres pod too, not just server/worker. Nil for every other
// mode.
func PackedAffinity(spec benchv1alpha1.PlacementSpec, releaseName string) *corev1.Affinity {
	if spec.Mode != benchv1alpha1.PlacementPacked {
		return nil
	}
	return &corev1.Affinity{
		PodAffinity: &corev1.PodAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app.kubernetes.io/instance": releaseName},
					},
					TopologyKey: "kubernetes.io/hostname",
				},
			},
		},
	}
}

// Tolerations returns the tolerations to apply in Dedicated mode, letting
// pods land on pre-tainted nodes. Empty in Default mode.
func Tolerations(spec benchv1alpha1.PlacementSpec) []corev1.Toleration {
	if spec.Mode != benchv1alpha1.PlacementDedicated {
		return nil
	}
	return spec.Tolerations
}

// NodeSelector returns the node selector to apply in Dedicated mode. Empty in
// Default mode.
func NodeSelector(spec benchv1alpha1.PlacementSpec) map[string]string {
	if spec.Mode != benchv1alpha1.PlacementDedicated {
		return nil
	}
	return spec.NodeSelector
}
