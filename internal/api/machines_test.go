package api

import (
	"net/http"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func capiObject(kind, name string, labels map[string]string, fields map[string]any) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: fields}
	u.SetGroupVersionKind(schema.GroupVersionKind{Group: "cluster.x-k8s.io", Version: "v1beta2", Kind: kind})
	u.SetName(name)
	u.SetNamespace(testNamespace)
	u.SetLabels(labels)
	return u
}

func machine(name string, labels map[string]string, nodeName, phase, ready, ip string) *unstructured.Unstructured {
	status := map[string]any{"phase": phase, "conditions": []any{map[string]any{"type": "NodeReady", "status": ready}}}
	if nodeName != "" {
		status["nodeRef"] = map[string]any{"name": nodeName}
		status["addresses"] = []any{
			map[string]any{"type": "ExternalIP", "address": "203.0.113.1"},
			map[string]any{"type": "InternalIP", "address": ip},
		}
	}
	return capiObject("Machine", name, labels, map[string]any{"spec": map[string]any{"version": "v1.36.4"}, "status": status})
}

func TestListClusterNodes(t *testing.T) {
	scheme := testScheme(t)
	gv := schema.GroupVersion{Group: "cluster.x-k8s.io", Version: "v1beta2"}
	for _, kind := range []string{"Machine", "MachineDeployment"} {
		scheme.AddKnownTypeWithName(gv.WithKind(kind), &unstructured.Unstructured{})
		scheme.AddKnownTypeWithName(gv.WithKind(kind+"List"), &unstructured.UnstructuredList{})
	}
	cluster := map[string]string{capiClusterNameLabel: "c1"}
	pool := func(md string) map[string]string {
		return map[string]string{capiClusterNameLabel: "c1", capiDeploymentNameLabel: md}
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		capiObject("MachineDeployment", "c1-ak", cluster, map[string]any{"spec": map[string]any{"replicas": int64(2)}}),
		capiObject("MachineDeployment", "c1-k6", cluster, map[string]any{"spec": map[string]any{"replicas": int64(0)}}),
		machine("c1-cp-x", map[string]string{capiClusterNameLabel: "c1", capiControlPlaneLabel: ""}, "c1-cp-x", "Running", "True", "10.0.0.2"),
		machine("c1-ak-a", pool("c1-ak"), "c1-ak-a", "Running", "True", "10.0.0.3"),
		machine("c1-ak-b", pool("c1-ak"), "", "Provisioning", "Unknown", ""),
		// Another cluster's machine: not listed.
		machine("c2-ak-a", map[string]string{capiClusterNameLabel: "c2", capiDeploymentNameLabel: "c2-ak"}, "c2-ak-a", "Running", "True", "10.1.0.3"),
	).Build()
	s := New(c, Config{Namespace: testNamespace, Token: testToken, CAPIAvailable: true})

	rec := do(t, s, http.MethodGet, "/api/v1/clusters/c1/nodes", nil)
	requireStatus(t, rec, http.StatusOK)
	got := decodeBody[clusterNodesResponse](t, rec)

	wantPools := []ClusterPool{
		{Name: "control-plane", Desired: 1, Current: 1, Ready: 1},
		{Name: "ak", Desired: 2, Current: 2, Ready: 1},
		{Name: "k6", Desired: 0, Current: 0, Ready: 0},
	}
	if len(got.Pools) != len(wantPools) {
		t.Fatalf("pools = %+v", got.Pools)
	}
	for i, want := range wantPools {
		if got.Pools[i] != want {
			t.Errorf("pool %d = %+v, want %+v", i, got.Pools[i], want)
		}
	}
	if len(got.Machines) != 3 || got.Machines[0].Pool != "control-plane" {
		t.Fatalf("machines = %+v", got.Machines)
	}
	ak := got.Machines[1]
	if ak.Name != "c1-ak-a" || ak.Pool != "ak" || !ak.Ready || ak.InternalIP != "10.0.0.3" || ak.Version != "v1.36.4" || ak.NodeName != "c1-ak-a" {
		t.Errorf("ak machine = %+v", ak)
	}
	if pending := got.Machines[2]; pending.NodeName != "" || pending.Ready || pending.Phase != "Provisioning" {
		t.Errorf("provisioning machine = %+v", pending)
	}

	// The management cluster has no machines to show.
	rec = do(t, s, http.MethodGet, "/api/v1/clusters/"+ManagementClusterName+"/nodes", nil)
	requireStatus(t, rec, http.StatusOK)
	if got := decodeBody[clusterNodesResponse](t, rec); len(got.Machines) != 0 || len(got.Pools) != 0 {
		t.Errorf("management cluster = %+v", got)
	}
}
