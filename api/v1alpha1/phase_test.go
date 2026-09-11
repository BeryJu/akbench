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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPhaseForNoConditions(t *testing.T) {
	if got := PhaseFor(nil); got != PhasePending {
		t.Errorf("PhaseFor(nil) = %q, want %q", got, PhasePending)
	}
}

func TestPhaseForReady(t *testing.T) {
	conditions := []metav1.Condition{
		{Type: ConditionReady, Status: metav1.ConditionTrue, Reason: ReasonReady},
	}
	if got := PhaseFor(conditions); got != PhaseReady {
		t.Errorf("PhaseFor(Ready=True) = %q, want %q", got, PhaseReady)
	}
}

func TestPhaseForFailed(t *testing.T) {
	conditions := []metav1.Condition{
		{Type: ConditionReady, Status: metav1.ConditionFalse, Reason: ReasonReconcileError},
	}
	if got := PhaseFor(conditions); got != PhaseFailed {
		t.Errorf("PhaseFor(Ready=False, ReconcileError) = %q, want %q", got, PhaseFailed)
	}
}

func TestPhaseForReconciling(t *testing.T) {
	conditions := []metav1.Condition{
		{Type: ConditionReady, Status: metav1.ConditionFalse, Reason: ReasonDependencyNotReady},
	}
	if got := PhaseFor(conditions); got != PhaseReconciling {
		t.Errorf("PhaseFor(Ready=False, DependencyNotReady) = %q, want %q", got, PhaseReconciling)
	}
}
