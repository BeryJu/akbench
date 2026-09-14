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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// TestCaseTemplateSpec is one reusable k6 test preset a TestCase can select
// via spec.template (by this object's name) - the editable, cluster-native
// replacement for what used to be a fixed, code-compiled catalog
// (internal/testtemplate's own Registry, now removed). Editing a template
// here - through Headlamp, or plain kubectl - takes effect the next time a
// TestCase referencing it reconciles; nothing needs rebuilding or
// redeploying.
type TestCaseTemplateSpec struct {
	// description documents what this template exercises, shown in the
	// Headlamp plugin's template picker.
	// +optional
	Description string `json:"description,omitempty"`

	// script is the k6 test script (TypeScript) this template runs -
	// mirrored verbatim into the k6-script-<testcase> ConfigMap by
	// TestCaseReconciler, the same way spec.inline already is on a TestCase
	// itself.
	Script string `json:"script"`

	// requiresOAuthClient gates TestCaseReconciler's oidc-test-client
	// Deployment/Service + OAuth2 provider/application blueprint
	// provisioning (internal/controller/testcase_controller.go,
	// internal/render/oidctestclient.go) for any TestCase selecting this
	// template - scoped to the TestCase, not the AuthentikDeployment it
	// targets, so "what is being tested" stays fully decoupled from "what
	// it's being tested against" the same way the k6 workload itself
	// already is.
	// +optional
	RequiresOAuthClient bool `json:"requiresOAuthClient,omitempty"`
}

// TestCaseTemplateStatus defines the observed state of TestCaseTemplate.
type TestCaseTemplateStatus struct {
	// conditions report Ready - True once spec.script is non-empty. A
	// template has no remote/owned resources of its own to reconcile; this
	// only exists so a broken template (e.g. accidentally emptied via
	// Headlamp) is visible the same way every other CRD's status already is.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// phase summarizes the Ready condition at a glance - computed from
	// conditions, never set independently. See ReconcilePhase.
	// +optional
	Phase ReconcilePhase `json:"phase,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="OAuth",type=boolean,JSONPath=`.spec.requiresOAuthClient`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`

// TestCaseTemplate is the Schema for the testcasetemplates API
type TestCaseTemplate struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec TestCaseTemplateSpec `json:"spec"`

	// +optional
	Status TestCaseTemplateStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TestCaseTemplateList contains a list of TestCaseTemplate
type TestCaseTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []TestCaseTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &TestCaseTemplate{}, &TestCaseTemplateList{})
		return nil
	})
}
