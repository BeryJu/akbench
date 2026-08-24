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

package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ManagementClusterName is the name the UI's cluster picker shows for "the
// cluster the operator itself runs on", which is what an empty
// AuthentikTopology.spec.clusterRef selects.
const ManagementClusterName = "management"

// capiClusterGVK is the Cluster API kind the operator resolves clusterRef
// against from M4 on.
var capiClusterGVK = schema.GroupVersionKind{Group: "cluster.x-k8s.io", Version: "v1beta2", Kind: "Cluster"}

// Cluster is the UI's view of somewhere a topology can be deployed.
type Cluster struct {
	Name string `json:"name"`
	// Management marks the cluster the operator runs on - the implicit
	// target of an empty clusterRef.
	Management bool `json:"management"`
	// Phase mirrors CAPI's Cluster.status.phase (Provisioning, Provisioned,
	// Deleting...). The management cluster reports Provisioned: it exists by
	// definition, since the backend is talking to it.
	Phase               string `json:"phase,omitempty"`
	ControlPlaneReady   bool   `json:"controlPlaneReady"`
	InfrastructureReady bool   `json:"infrastructureReady"`
}

func managementCluster() Cluster {
	return Cluster{
		Name:                ManagementClusterName,
		Management:          true,
		Phase:               "Provisioned",
		ControlPlaneReady:   true,
		InfrastructureReady: true,
	}
}

// listClusters returns every place a topology can be deployed. Until the
// multi-cluster milestone installs Cluster API, that is exactly one entry:
// the management cluster itself.
func (s *Server) listClusters(w http.ResponseWriter, r *http.Request) {
	clusters := []Cluster{managementCluster()}
	if s.cfg.CAPIAvailable {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(capiClusterGVK.GroupVersion().WithKind("ClusterList"))
		if err := s.client.List(r.Context(), list, client.InNamespace(s.cfg.Namespace)); err != nil {
			writeAPIError(w, r, err)
			return
		}
		for i := range list.Items {
			clusters = append(clusters, capiCluster(&list.Items[i]))
		}
	}
	writeJSON(w, r, http.StatusOK, map[string]any{"items": clusters})
}

func (s *Server) getCluster(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == ManagementClusterName {
		writeJSON(w, r, http.StatusOK, managementCluster())
		return
	}
	if !s.cfg.CAPIAvailable {
		writeError(w, r, http.StatusNotFound, "no such cluster")
		return
	}
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(capiClusterGVK)
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: name, Namespace: s.cfg.Namespace}, obj); err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, capiCluster(obj))
}

func capiCluster(obj *unstructured.Unstructured) Cluster {
	phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	controlPlaneReady, _, _ := unstructured.NestedBool(obj.Object, "status", "controlPlaneReady")
	infraReady, _, _ := unstructured.NestedBool(obj.Object, "status", "infrastructureReady")
	return Cluster{
		Name:                obj.GetName(),
		Phase:               phase,
		ControlPlaneReady:   controlPlaneReady,
		InfrastructureReady: infraReady,
	}
}

// registerCluster and deleteCluster are the multi-cluster milestone's job:
// clusters are provisioned by a WorkloadCluster CR the operator renders into
// Cluster API objects, not by uploading a kubeconfig here. They answer 501 so
// the UI can grey out the button against a reason rather than guess.
func (s *Server) registerCluster(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, http.StatusNotImplemented,
		"cluster provisioning is not implemented yet; topologies currently deploy onto the management cluster")
}

func (s *Server) deleteCluster(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, http.StatusNotImplemented,
		"cluster deprovisioning is not implemented yet")
}
