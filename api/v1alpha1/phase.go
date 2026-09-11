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

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ReconcilePhase summarizes an ongoing, declaratively-reconciled resource's
// status at a glance (AuthentikTopology, TestCase) - distinct from
// TestRunPhase (a one-shot job's terminal outcome) and WorkloadCluster's
// own Phase field (a verbatim mirror of CAPI's external vocabulary), which
// each already have their own field and aren't affected by this type.
// +kubebuilder:validation:Enum=Pending;Reconciling;Ready;Failed
type ReconcilePhase string

const (
	// PhasePending means the resource has no Ready condition yet (just
	// created, not reconciled once).
	PhasePending ReconcilePhase = "Pending"
	// PhaseReconciling means Ready=False for a reason other than
	// ReasonReconcileError (waiting on a dependency, an in-progress Helm
	// install, ...).
	PhaseReconciling ReconcilePhase = "Reconciling"
	// PhaseReady means Ready=True.
	PhaseReady ReconcilePhase = "Ready"
	// PhaseFailed means Ready=False with reason ReasonReconcileError.
	PhaseFailed ReconcilePhase = "Failed"
)

// PhaseFor derives a ReconcilePhase from a resource's own Ready condition -
// computed on demand rather than tracked independently, so it can never
// drift from the conditions it summarizes.
func PhaseFor(conditions []metav1.Condition) ReconcilePhase {
	cond := meta.FindStatusCondition(conditions, ConditionReady)
	if cond == nil {
		return PhasePending
	}
	if cond.Status == metav1.ConditionTrue {
		return PhaseReady
	}
	if cond.Reason == ReasonReconcileError {
		return PhaseFailed
	}
	return PhaseReconciling
}
