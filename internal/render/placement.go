// Package render turns bench.goauthentik.io CRs into the Kubernetes/Flux/CNPG
// objects that implement them.
package render

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// defaultTopologyKey mirrors PlacementSpec's own kubebuilder default, used
// when a spec was built up in Go rather than defaulted by the API server
// (e.g. in unit tests).
const defaultTopologyKey = "kubernetes.io/hostname"

// Affinity returns the pod affinity for a workload scheduled under the given
// placement spec. Dedicated mode requires one pod per node - Default mode
// applies no affinity, letting the scheduler share nodes with other
// workloads (the noisy-neighbor case the benchmark compares against).
func Affinity(spec benchv1alpha1.PlacementSpec, matchLabels map[string]string) *corev1.Affinity {
	if spec.Mode != benchv1alpha1.PlacementDedicated {
		return nil
	}
	topologyKey := spec.TopologyKey
	if topologyKey == "" {
		topologyKey = defaultTopologyKey
	}
	return &corev1.Affinity{
		PodAntiAffinity: &corev1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
				{
					TopologyKey:   topologyKey,
					LabelSelector: &metav1.LabelSelector{MatchLabels: matchLabels},
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
