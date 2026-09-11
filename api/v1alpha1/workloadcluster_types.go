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

// InfrastructureProvider selects which Cluster API infrastructure provider
// WorkloadClusterReconciler provisions a cluster through. Incus is the only
// one implemented today (github.com/lxc/cluster-api-provider-incus, already
// installed on the management cluster) - this is an enum rather than a bare
// string, and each provider gets its own optional sub-spec below (mirroring
// PostgresSpec's Mode/Embedded/CNPG/External split), so adding e.g. AWS or
// vSphere later is a matter of another case and another sub-spec, not a
// breaking change to existing WorkloadClusters.
// +kubebuilder:validation:Enum=Incus
type InfrastructureProvider string

const (
	// InfrastructureIncus provisions via the Incus/LXC infrastructure
	// provider.
	InfrastructureIncus InfrastructureProvider = "Incus"
)

// MachineSpec sizes one set of machines (the control plane or the worker
// MachineDeployment) in a way every infrastructure provider understands -
// only the replica count. Provider-specific sizing (instance
// type/flavor/image/...) lives in that provider's own sub-spec instead (e.g.
// IncusMachineSpec), since it doesn't mean the same thing across providers.
type MachineSpec struct {
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	Replicas int32 `json:"replicas,omitempty"`
}

// IncusMachineSpec sizes one set of machines on the Incus/LXC infrastructure
// provider beyond the provider-agnostic replica count in MachineSpec.
type IncusMachineSpec struct {
	// flavor is an Incus/LXC instance flavor (e.g. "c2-m4" - 2 vCPU, 4GiB).
	// +kubebuilder:default="c2-m4"
	// +optional
	Flavor string `json:"flavor,omitempty"`
}

// IncusProviderSpec configures provisioning via the Incus/LXC infrastructure
// provider. Required when spec.provider is Incus (the default).
type IncusProviderSpec struct {
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
	ControlPlane IncusMachineSpec `json:"controlPlane,omitempty"`
}

// WorkerPoolSpec is one named, independently-sized group of worker machines,
// letting a WorkloadCluster run more than one worker shape at once - e.g. a
// small pool for the k6 runner alongside a larger one sized for load
// generation. Pool names must be unique within a WorkloadCluster.
//
// Editing an existing pool's replicas or flavor is applied in place by
// WorkloadClusterReconciler: replicas patches the MachineDeployment
// directly, and a flavor change rolls that pool's machines through Cluster
// API's normal MachineDeployment rollout (a new, immutably-named
// LXCMachineTemplate is created and the MachineDeployment is repointed at
// it - the old template is left behind rather than garbage collected, the
// same way an unused ReplicaSet lingers after a Deployment rollout).
// Removing a pool from the list deletes its MachineDeployment; adding a new
// name creates one. The control plane itself is not resizable/reconfigurable
// this way yet.
type WorkerPoolSpec struct {
	// name identifies this pool; it becomes part of the MachineDeployment's
	// own name, so it follows the same naming rules as a Kubernetes object.
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`

	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// incus is this pool's Incus-specific sizing. Required when
	// spec.provider is Incus.
	// +optional
	Incus IncusMachineSpec `json:"incus,omitempty"`
}

// WorkloadClusterSpec describes a Kubernetes cluster this operator asks
// Cluster API to provision through the selected infrastructure provider, so
// AuthentikTopology/TestCase can target it via clusterRef instead of the
// management cluster.
type WorkloadClusterSpec struct {
	// kubernetesVersion is the version kubeadm installs, e.g. "v1.36.1".
	// +kubebuilder:validation:Pattern=`^v[0-9]+\.[0-9]+\.[0-9]+$`
	KubernetesVersion string `json:"kubernetesVersion"`

	// provider selects the Cluster API infrastructure provider to use.
	// +kubebuilder:default=Incus
	// +optional
	Provider InfrastructureProvider `json:"provider,omitempty"`

	// +optional
	ControlPlane MachineSpec `json:"controlPlane,omitempty"`

	// workerPools are the WorkloadCluster's worker machine groups - at least
	// one is required. See WorkerPoolSpec for how editing this list after
	// creation behaves.
	// +kubebuilder:validation:MinItems=1
	WorkerPools []WorkerPoolSpec `json:"workerPools"`

	// incus configures provisioning when provider is Incus.
	// +optional
	Incus *IncusProviderSpec `json:"incus,omitempty"`
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

	// PrometheusURL is where this cluster's own Prometheus (installed by
	// WorkloadClusterReconciler's ensurePrometheus) is reachable *from the
	// management cluster* - a node's InternalIP plus the fixed NodePort
	// render.PrometheusNodePort exposes, not the cluster-relative DNS name
	// k6 workloads push metrics to (that only resolves from inside this
	// same cluster). cmd/apiserver reads this to query per-testid metrics
	// without a central receiver.
	// +optional
	PrometheusURL string `json:"prometheusURL,omitempty"`

	// GrafanaURL is where this cluster's own Grafana (installed by
	// WorkloadClusterReconciler's ensureGrafana) is reachable *from the
	// management cluster* - a node's InternalIP plus the fixed NodePort
	// render.GrafanaNodePort exposes. cmd/apiserver's own reverse proxy
	// (internal/api/grafana_proxy.go) connects to this to embed a
	// dashboard in Headlamp's Grafana tab.
	// +optional
	GrafanaURL string `json:"grafanaURL,omitempty"`

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
