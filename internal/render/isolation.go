package render

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// DeploymentLabels is what every pod belonging to a deployment (server, worker,
// embedded/CNPG postgres) and its k6 runner carry, so a single label
// selector - not one per postgres mode or component - is enough for the
// anti-affinity terms below to find them all.
func DeploymentLabels(deploymentName string) map[string]string {
	return map[string]string{benchv1alpha1.LabelDeployment: deploymentName}
}

// crossDeploymentAntiAffinityTerm requires scheduling away from any node that
// already runs a pod belonging to a *different* deployment, so two
// deployments' benchmarks never share a node and interfere with each
// other's numbers - regardless of PlacementMode, which only ever governs
// scheduling within one deployment.
//
// Two ANDed matchExpressions, not a bare NotIn: Kubernetes' own label
// selector semantics have NotIn (and DoesNotExist) match pods that don't
// have the key at all, not just ones with a different value - documented
// explicitly as a footgun in the upstream docs. A bare NotIn here would
// also required-anti-affinity away from every node running ordinary
// kube-system/unrelated pods with no deployment label, likely making every
// deployment unschedulable. Requiring Exists first excludes those.
func crossDeploymentAntiAffinityTerm(deploymentName string) corev1.PodAffinityTerm {
	return corev1.PodAffinityTerm{
		LabelSelector: &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{Key: benchv1alpha1.LabelDeployment, Operator: metav1.LabelSelectorOpExists},
				{Key: benchv1alpha1.LabelDeployment, Operator: metav1.LabelSelectorOpNotIn, Values: []string{deploymentName}},
			},
		},
		TopologyKey: "kubernetes.io/hostname",
	}
}

// sameDeploymentAntiAffinityTerm requires scheduling away from any node
// already running a pod belonging to the given deployment specifically - what
// the k6 runner uses to guarantee it never lands on the same node as the
// deployment it's driving, so the load generator never competes with the
// system under test for the same node's CPU.
func sameDeploymentAntiAffinityTerm(deploymentName string) corev1.PodAffinityTerm {
	return corev1.PodAffinityTerm{
		LabelSelector: &metav1.LabelSelector{
			MatchLabels: DeploymentLabels(deploymentName),
		},
		TopologyKey: "kubernetes.io/hostname",
	}
}
