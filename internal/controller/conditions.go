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

package controller

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// setCondition is a small wrapper around meta.SetStatusCondition so every
// controller records conditions the same way - it also emits a matching
// Kubernetes Event on obj, so anyone looking at the object in `kubectl
// describe`/Headlamp sees the same history without reading operator logs.
// Called on every reconcile pass that reaches a given branch, not only on
// state transitions: this is deliberate, not an oversight - client-go's
// EventRecorder already aggregates identical repeats (same object/reason/
// message) into one Event object with an incrementing count rather than
// creating a new object each time, the same way core Kubernetes controllers
// behave (e.g. kubelet's repeated "Pulling image" events).
func setCondition(recorder record.EventRecorder, obj runtime.Object, conditions *[]metav1.Condition, generation int64, condType string, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: generation,
	})
	eventType := corev1.EventTypeNormal
	if status == metav1.ConditionFalse && reason == benchv1alpha1.ReasonReconcileError {
		eventType = corev1.EventTypeWarning
	}
	recorder.Event(obj, eventType, reason, message)
}
