// Package render turns bench.goauthentik.io CRs into the Kubernetes/Flux/CNPG
// objects that implement them.
package render

import (
	corev1 "k8s.io/api/core/v1"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// AffinityPreset returns the authentik chart's own affinity-preset value
// (server.affinity.podAntiAffinity / worker.affinity.podAntiAffinity: one of
// "none", "soft", "hard" - see charts.goauthentik.io/authentik's
// _helpers.tpl) for the given placement mode. Dedicated maps to "hard"
// (required pod anti-affinity, one pod per node - the topology key the chart
// uses for "hard" is fixed at kubernetes.io/hostname, matching
// PlacementSpec's own default; PlacementSpec.TopologyKey has no effect here
// since the chart doesn't expose it as a value - use ExtraValuesRef for a
// custom key). Default maps to "none": the chart's own root default is
// "soft", which would already bias pods apart, so Default must say "none"
// explicitly to let pods land on the same node (the noisy-neighbor case the
// benchmark compares against).
func AffinityPreset(spec benchv1alpha1.PlacementSpec) string {
	if spec.Mode == benchv1alpha1.PlacementDedicated {
		return "hard"
	}
	return "none"
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
