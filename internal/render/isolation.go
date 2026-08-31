/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package render

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// TopologyLabels is what every pod belonging to a topology (server, worker,
// embedded/CNPG postgres) and its k6 runner carry, so a single label
// selector - not one per postgres mode or component - is enough for the
// anti-affinity terms below to find them all.
func TopologyLabels(topologyName string) map[string]string {
	return map[string]string{benchv1alpha1.LabelTopology: topologyName}
}

// crossTopologyAntiAffinityTerm requires scheduling away from any node that
// already runs a pod belonging to a *different* topology, so two
// topologies' benchmarks never share a node and interfere with each
// other's numbers - regardless of PlacementMode, which only ever governs
// scheduling within one topology.
//
// Two ANDed matchExpressions, not a bare NotIn: Kubernetes' own label
// selector semantics have NotIn (and DoesNotExist) match pods that don't
// have the key at all, not just ones with a different value - documented
// explicitly as a footgun in the upstream docs. A bare NotIn here would
// also required-anti-affinity away from every node running ordinary
// kube-system/unrelated pods with no topology label, likely making every
// topology unschedulable. Requiring Exists first excludes those.
func crossTopologyAntiAffinityTerm(topologyName string) corev1.PodAffinityTerm {
	return corev1.PodAffinityTerm{
		LabelSelector: &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{Key: benchv1alpha1.LabelTopology, Operator: metav1.LabelSelectorOpExists},
				{Key: benchv1alpha1.LabelTopology, Operator: metav1.LabelSelectorOpNotIn, Values: []string{topologyName}},
			},
		},
		TopologyKey: "kubernetes.io/hostname",
	}
}

// sameTopologyAntiAffinityTerm requires scheduling away from any node
// already running a pod belonging to the given topology specifically - what
// the k6 runner uses to guarantee it never lands on the same node as the
// topology it's driving, so the load generator never competes with the
// system under test for the same node's CPU.
func sameTopologyAntiAffinityTerm(topologyName string) corev1.PodAffinityTerm {
	return corev1.PodAffinityTerm{
		LabelSelector: &metav1.LabelSelector{
			MatchLabels: TopologyLabels(topologyName),
		},
		TopologyKey: "kubernetes.io/hostname",
	}
}
