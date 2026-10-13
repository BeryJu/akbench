package api

import (
	"cmp"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// A workload cluster's actual nodes, as Cluster API sees them from the
// management cluster: one Machine per node, grouped into the
// MachineDeployments WorkloadClusterReconciler renders per worker pool
// (render.MachineDeployment). Read here rather than from the workload
// cluster itself, so it also shows machines still provisioning (no node
// yet) and works while the cluster's API is unreachable.

var (
	capiMachineListGVK           = capiGV.WithKind("MachineList")
	capiMachineDeploymentListGVK = capiGV.WithKind("MachineDeploymentList")
)

const (
	capiClusterNameLabel    = "cluster.x-k8s.io/cluster-name"
	capiDeploymentNameLabel = "cluster.x-k8s.io/deployment-name"
	capiControlPlaneLabel   = "cluster.x-k8s.io/control-plane"
	// controlPlanePool is the pool name control-plane machines are reported
	// under - not a WorkerPoolSpec, but shown alongside them.
	controlPlanePool = "control-plane"
)

// ClusterPool is one worker pool's (or the control plane's) size right now.
type ClusterPool struct {
	Name string `json:"name"`
	// Desired is the replica count currently asked for - set by
	// cluster-autoscaler for an autoscaled pool, so it moves within the
	// pool's range.
	Desired int64 `json:"desired"`
	// Current counts the pool's machines, Ready the ones whose node is
	// Ready.
	Current int64 `json:"current"`
	Ready   int64 `json:"ready"`
}

// ClusterMachine is one machine - and, once it has joined, its node.
type ClusterMachine struct {
	Name string `json:"name"`
	Pool string `json:"pool"`
	// NodeName is empty until the machine's node has joined the cluster.
	NodeName string `json:"nodeName,omitempty"`
	// Phase is Cluster API's machine phase (Pending, Provisioning,
	// Provisioned, Running, Deleting, Failed, ...).
	Phase      string `json:"phase,omitempty"`
	Ready      bool   `json:"ready"`
	InternalIP string `json:"internalIP,omitempty"`
	Version    string `json:"version,omitempty"`
	Created    string `json:"created,omitempty"`
}

type clusterNodesResponse struct {
	Pools    []ClusterPool    `json:"pools"`
	Machines []ClusterMachine `json:"machines"`
}

// listClusterNodes serves GET /api/v1/clusters/{name}/nodes.
func (s *Server) listClusterNodes(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	resp := clusterNodesResponse{Pools: []ClusterPool{}, Machines: []ClusterMachine{}}
	if name == ManagementClusterName || !s.cfg.CAPIAvailable {
		writeJSON(w, r, http.StatusOK, resp)
		return
	}
	inCluster := []client.ListOption{client.InNamespace(s.cfg.Namespace), client.MatchingLabels{capiClusterNameLabel: name}}

	machines := &unstructured.UnstructuredList{}
	machines.SetGroupVersionKind(capiMachineListGVK)
	if err := s.client.List(r.Context(), machines, inCluster...); err != nil {
		writeAPIError(w, r, err)
		return
	}
	deployments := &unstructured.UnstructuredList{}
	deployments.SetGroupVersionKind(capiMachineDeploymentListGVK)
	if err := s.client.List(r.Context(), deployments, inCluster...); err != nil {
		writeAPIError(w, r, err)
		return
	}

	pools := map[string]*ClusterPool{}
	pool := func(name string) *ClusterPool {
		if p, ok := pools[name]; ok {
			return p
		}
		p := &ClusterPool{Name: name}
		pools[name] = p
		return p
	}
	for _, md := range deployments.Items {
		desired, _, _ := unstructured.NestedInt64(md.Object, "spec", "replicas")
		pool(poolName(name, md.GetName())).Desired = desired
	}
	for _, item := range machines.Items {
		m := toClusterMachine(name, &item)
		resp.Machines = append(resp.Machines, m)
		p := pool(m.Pool)
		p.Current++
		if m.Ready {
			p.Ready++
		}
		if m.Pool == controlPlanePool {
			p.Desired = p.Current
		}
	}

	for _, p := range pools {
		resp.Pools = append(resp.Pools, *p)
	}
	slices.SortFunc(resp.Pools, func(a, b ClusterPool) int {
		// Control plane first, then pools by name.
		return cmp.Or(cmp.Compare(boolRank(a.Name != controlPlanePool), boolRank(b.Name != controlPlanePool)), strings.Compare(a.Name, b.Name))
	})
	slices.SortFunc(resp.Machines, func(a, b ClusterMachine) int {
		return cmp.Or(cmp.Compare(boolRank(a.Pool != controlPlanePool), boolRank(b.Pool != controlPlanePool)),
			strings.Compare(a.Pool, b.Pool), strings.Compare(a.Name, b.Name))
	})
	writeJSON(w, r, http.StatusOK, resp)
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// poolName maps a MachineDeployment name back to its WorkerPoolSpec name -
// render.MachineDeploymentName is "<cluster>-<pool>".
func poolName(cluster, deployment string) string {
	return strings.TrimPrefix(deployment, cluster+"-")
}

func toClusterMachine(cluster string, obj *unstructured.Unstructured) ClusterMachine {
	m := ClusterMachine{Name: obj.GetName(), Created: obj.GetCreationTimestamp().UTC().Format("2006-01-02T15:04:05Z")}
	labels := obj.GetLabels()
	if _, ok := labels[capiControlPlaneLabel]; ok {
		m.Pool = controlPlanePool
	} else {
		m.Pool = poolName(cluster, labels[capiDeploymentNameLabel])
	}
	m.NodeName, _, _ = unstructured.NestedString(obj.Object, "status", "nodeRef", "name")
	m.Phase, _, _ = unstructured.NestedString(obj.Object, "status", "phase")
	m.Version, _, _ = unstructured.NestedString(obj.Object, "spec", "version")
	addresses, _, _ := unstructured.NestedSlice(obj.Object, "status", "addresses")
	for _, a := range addresses {
		if addr, ok := a.(map[string]any); ok && addr["type"] == "InternalIP" {
			m.InternalIP, _ = addr["address"].(string)
			break
		}
	}
	// Cluster API's NodeReady condition mirrors the node's own Ready.
	conditions, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	for _, c := range conditions {
		if cond, ok := c.(map[string]any); ok && cond["type"] == "NodeReady" {
			m.Ready = cond["status"] == "True"
		}
	}
	return m
}
