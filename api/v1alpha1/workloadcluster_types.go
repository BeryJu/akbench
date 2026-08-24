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

// WorkloadClusterMachineSpec sizes one set of machines (the control plane or
// the worker MachineDeployment) on the Incus/LXC infrastructure provider.
type WorkloadClusterMachineSpec struct {
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	Replicas int32 `json:"replicas,omitempty"`
	// flavor is an Incus/LXC instance flavor (e.g. "c2-m4" - 2 vCPU, 4GiB).
	// +kubebuilder:default="c2-m4"
	// +optional
	Flavor string `json:"flavor,omitempty"`
}

// WorkloadClusterSpec describes a Kubernetes cluster this operator asks
// Cluster API's Incus infrastructure provider to provision, so
// AuthentikTopology/TestCase can target it via clusterRef instead of the
// management cluster.
type WorkloadClusterSpec struct {
	// kubernetesVersion is the version kubeadm installs, e.g. "v1.36.1".
	// +kubebuilder:validation:Pattern=`^v[0-9]+\.[0-9]+\.[0-9]+$`
	KubernetesVersion string `json:"kubernetesVersion"`

	// lxcSecretRef names a Secret (in this object's namespace) holding the
	// Incus/LXC daemon connection: server, project, client-crt, client-key
	// (matching the keys cluster-api-provider-incus itself expects).
	LXCSecretRef corev1.LocalObjectReference `json:"lxcSecretRef"`

	// image is the Incus/LXC image new machines boot from.
	// +kubebuilder:default="ubuntu:24.04"
	// +optional
	Image string `json:"image,omitempty"`

	// profiles are the Incus/LXC profiles applied to every machine and to
	// the control plane's load balancer.
	// +kubebuilder:default={"default"}
	// +optional
	Profiles []string `json:"profiles,omitempty"`

	// +optional
	ControlPlane WorkloadClusterMachineSpec `json:"controlPlane,omitempty"`
	// +optional
	Workers WorkloadClusterMachineSpec `json:"workers,omitempty"`
}

// WorkloadClusterStatus mirrors the underlying CAPI Cluster's readiness, plus
// where to find the kubeconfig once it exists.
type WorkloadClusterStatus struct {
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// phase mirrors the CAPI Cluster's own status.phase (Provisioning,
	// Provisioned, Deleting...).
	// +optional
	Phase string `json:"phase,omitempty"`
	// +optional
	ControlPlaneReady bool `json:"controlPlaneReady,omitempty"`
	// +optional
	InfrastructureReady bool `json:"infrastructureReady,omitempty"`

	// KubeconfigSecretRef names the "<cluster>-kubeconfig" Secret CAPI
	// writes once the control plane is up - what
	// internal/multicluster.Tracker resolves a clusterRef against.
	// +optional
	KubeconfigSecretRef *corev1.LocalObjectReference `json:"kubeconfigSecretRef,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="ControlPlane",type=boolean,JSONPath=`.status.controlPlaneReady`
// +kubebuilder:printcolumn:name="Infrastructure",type=boolean,JSONPath=`.status.infrastructureReady`

// WorkloadCluster is the Schema for the workloadclusters API
type WorkloadCluster struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec WorkloadClusterSpec `json:"spec"`

	// +optional
	Status WorkloadClusterStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// WorkloadClusterList contains a list of WorkloadCluster
type WorkloadClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []WorkloadCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &WorkloadCluster{}, &WorkloadClusterList{})
		return nil
	})
}
