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
	"context"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/go-chi/chi/v5"
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
	// NodeIP is a reachable InternalIP of one of this cluster's own nodes -
	// only ever populated for the management cluster, and only so the
	// Headlamp plugin's Grafana tab can reach cmd/apiserver's own NodePort
	// (see internal/api/grafana_proxy.go) directly from the browser,
	// bypassing the k8s API server's own service-proxy subresource
	// entirely (see grafana.go's doc comment for why that's necessary).
	NodeIP string `json:"nodeIP,omitempty"`
}

func (s *Server) managementCluster(ctx context.Context) Cluster {
	c := Cluster{
		Name:                ManagementClusterName,
		Management:          true,
		Phase:               "Provisioned",
		ControlPlaneReady:   true,
		InfrastructureReady: true,
	}
	if nodeIP, err := reachableNodeIP(ctx, s.client); err == nil {
		c.NodeIP = nodeIP
	}
	return c
}

// controlPlaneNodeRoleLabel is kubeadm's own standard label, set on every
// control-plane node - preferred over a worker node so the returned IP is
// stable across worker scaling.
const controlPlaneNodeRoleLabel = "node-role.kubernetes.io/control-plane"

// reachableNodeIP finds a node's InternalIP, preferring a control-plane
// node. Duplicated from internal/controller/workloadcluster_controller.go's
// own reachableNodeIP rather than shared/exported: it's a handful of lines,
// and internal/api importing internal/controller would be the wrong
// dependency direction.
func reachableNodeIP(ctx context.Context, c client.Client) (string, error) {
	var nodes corev1.NodeList
	if err := c.List(ctx, &nodes, client.MatchingLabels{controlPlaneNodeRoleLabel: ""}); err != nil {
		return "", fmt.Errorf("list control-plane nodes: %w", err)
	}
	if len(nodes.Items) == 0 {
		if err := c.List(ctx, &nodes); err != nil {
			return "", fmt.Errorf("list nodes: %w", err)
		}
	}
	for _, node := range nodes.Items {
		for _, addr := range node.Status.Addresses {
			if addr.Type == corev1.NodeInternalIP {
				return addr.Address, nil
			}
		}
	}
	return "", fmt.Errorf("no node with an InternalIP found")
}

// listClusters returns every place a topology can be deployed. Until the
// multi-cluster milestone installs Cluster API, that is exactly one entry:
// the management cluster itself.
func (s *Server) listClusters(w http.ResponseWriter, r *http.Request) {
	clusters := []Cluster{s.managementCluster(r.Context())}
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
		writeJSON(w, r, http.StatusOK, s.managementCluster(r.Context()))
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

// capiCluster reads a cluster.x-k8s.io/v1beta2 Cluster's readiness fields:
// status.initialization.{controlPlaneInitialized,infrastructureProvisioned}
// are the contract fields CAPI itself defines and never revises after first
// becoming true, unlike the higher-level Available/Ready conditions which
// can flap during normal operation (a health-check probe blip, a control
// plane node rolling) - not useful for "is this safe to deploy onto yet".
func capiCluster(obj *unstructured.Unstructured) Cluster {
	phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	controlPlaneReady, _, _ := unstructured.NestedBool(obj.Object, "status", "initialization", "controlPlaneInitialized")
	infraReady, _, _ := unstructured.NestedBool(obj.Object, "status", "initialization", "infrastructureProvisioned")
	return Cluster{
		Name:                obj.GetName(),
		Phase:               phase,
		ControlPlaneReady:   controlPlaneReady,
		InfrastructureReady: infraReady,
	}
}

// workloadClusterKind is the WorkloadCluster CRD registerCluster/deleteCluster
// provision through - the same generic resourceKind resources.go already
// serves at /api/v1/workloadclusters, reused here so /api/v1/clusters can
// offer a single "register/deprovision" action without duplicating the
// create/delete logic.
var workloadClusterKind = resourceKinds["workloadclusters"]

// registerCluster provisions a new workload cluster: the request body is a
// WorkloadCluster's metadata+spec (kubernetesVersion, lxcSecretRef,
// controlPlane/workers sizing), rendered by WorkloadClusterReconciler into
// real Cluster API objects on the Incus infrastructure provider.
func (s *Server) registerCluster(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.CAPIAvailable {
		writeError(w, r, http.StatusNotImplemented,
			"Cluster API is not installed on this management cluster")
		return
	}
	obj, err := s.decodeInto(w, r, workloadClusterKind)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if obj.GetName() == "" {
		writeError(w, r, http.StatusBadRequest, "metadata.name is required")
		return
	}
	if obj.GetName() == ManagementClusterName {
		writeError(w, r, http.StatusBadRequest, "name is reserved for the management cluster")
		return
	}
	if err := s.client.Create(r.Context(), obj); err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, obj)
}

// deleteCluster deprovisions a workload cluster: deleting the WorkloadCluster
// object lets WorkloadClusterReconciler tear down its Cluster API Cluster
// (and, through it, every machine CAPI/Incus created for it) before removing
// its own finalizer.
func (s *Server) deleteCluster(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == ManagementClusterName {
		writeError(w, r, http.StatusBadRequest, "cannot deprovision the management cluster")
		return
	}
	if !s.cfg.CAPIAvailable {
		writeError(w, r, http.StatusNotImplemented,
			"Cluster API is not installed on this management cluster")
		return
	}
	obj := workloadClusterKind.newObject()
	obj.SetName(name)
	obj.SetNamespace(s.cfg.Namespace)
	if err := s.client.Delete(r.Context(), obj); err != nil {
		writeAPIError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
