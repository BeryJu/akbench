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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// TestProfile decides how k6 runs a test - see the package doc comment for
// the quick/burn distinction ported from the Ansible benchmark harness.
// +kubebuilder:validation:Enum=Quick;Burn
type TestProfile string

const (
	// ProfileQuick runs vus*iterations logins then exits. Bounded, executed
	// by creating a TestRun.
	ProfileQuick TestProfile = "Quick"
	// ProfileBurn runs forever (until the TestCase is deleted or switched
	// back to Quick), self-healed by an owned Deployment. Used to keep a
	// target under sustained load.
	ProfileBurn TestProfile = "Burn"
)

// TestCaseSpec defines one k6 load test: which AuthentikDeployment it drives,
// which script/profile/load shape it uses, and which flow it walks. The
// object's name is the k6 `testid` tag every dashboard splits on.
type TestCaseSpec struct {
	// deploymentRef names the AuthentikDeployment this test drives.
	DeploymentRef corev1.LocalObjectReference `json:"deploymentRef"`

	// runnerClusterRef names the cluster.x-k8s.io/v1beta1 Cluster the k6
	// workload itself runs on. Defaults to the deployment's clusterRef.
	// +optional
	RunnerClusterRef *corev1.LocalObjectReference `json:"runnerClusterRef,omitempty"`

	// template names a built-in test template from internal/testtemplate's
	// Registry (e.g. "login", "oauth-oidc") - which k6 script runs, plus any
	// extra provisioning that template's flow needs (e.g. oauth-oidc's own
	// oidc-test-client and OAuth2 provider/application). Ignored when inline
	// or scriptConfigMapRef is set.
	// +kubebuilder:default=login
	// +optional
	Template string `json:"template,omitempty"`

	// inline is a raw k6 script body stored directly on this TestCase,
	// instead of picking one of the built-in templates or pointing at an
	// externally-managed ConfigMap. Overrides template; overridden by
	// scriptConfigMapRef.
	// +optional
	Inline string `json:"inline,omitempty"`

	// scriptConfigMapRef, if set, overrides template/inline with a
	// user-supplied script's contents (key "script.ts") in an
	// externally-managed ConfigMap.
	// +optional
	ScriptConfigMapRef *corev1.LocalObjectReference `json:"scriptConfigMapRef,omitempty"`

	Profile TestProfile `json:"profile"`

	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=8
	VUs int32 `json:"vus,omitempty"`

	// iterations is logins per VU. Quick only.
	// +kubebuilder:default=1000
	// +optional
	Iterations int32 `json:"iterations,omitempty"`

	// +kubebuilder:default=default-authentication-flow
	// +optional
	Flow string `json:"flow,omitempty"`

	// quickDuration caps a Quick run that cannot finish its iterations in time.
	// +kubebuilder:default="150s"
	// +optional
	QuickDuration string `json:"quickDuration,omitempty"`

	// burnDuration caps a Burn run - effectively unlimited by default.
	// +kubebuilder:default="8760h"
	// +optional
	BurnDuration string `json:"burnDuration,omitempty"`

	// traceSampleRate is the share of login flows k6 marks as sampled in the
	// traceparent header it sends.
	// +kubebuilder:default="0.01"
	// +optional
	TraceSampleRate string `json:"traceSampleRate,omitempty"`

	// extraTags are merged into the k6 --tag arguments alongside
	// testid/target/profile.
	// +optional
	ExtraTags map[string]string `json:"extraTags,omitempty"`
}

// TestCaseStatus defines the observed state of TestCase.
type TestCaseStatus struct {
	// conditions report Ready, DeploymentReady and ScriptReady.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// phase summarizes the Ready condition at a glance - computed from
	// conditions, never set independently. See ReconcilePhase.
	// +optional
	Phase ReconcilePhase `json:"phase,omitempty"`

	// burnWorkloadRef names the owned Deployment when profile is Burn.
	// +optional
	BurnWorkloadRef *corev1.LocalObjectReference `json:"burnWorkloadRef,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Deployment",type=string,JSONPath=`.spec.deploymentRef.name`
// +kubebuilder:printcolumn:name="Profile",type=string,JSONPath=`.spec.profile`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`

// TestCase is the Schema for the testcases API
type TestCase struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec TestCaseSpec `json:"spec"`

	// +optional
	Status TestCaseStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TestCaseList contains a list of TestCase
type TestCaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []TestCase `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &TestCase{}, &TestCaseList{})
		return nil
	})
}
