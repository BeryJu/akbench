package render

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	benchv1alpha1 "github.com/authentik-labs/akbench/api/v1alpha1"
)

// TestPoolPlacementRequiresMatchingPool is the backward-compatibility
// guarantee the whole node-pool scheme depends on: a workload must never get
// a nodeSelector pointing at a pool label no node actually carries, since
// CRD-level defaulting only ever applies to a *newly created* object with
// workerPools omitted - every WorkloadCluster that already existed before
// this feature shipped keeps whatever pools it already had (all Generic),
// forever, and a nil WorkloadCluster (a deployment/runner on the management
// cluster, which this scheme never applies to) must behave identically.
func TestPoolPlacementRequiresMatchingPool(t *testing.T) {
	cases := []struct {
		name string
		wc   *benchv1alpha1.WorkloadCluster
	}{
		{"nil WorkloadCluster", nil},
		{"no worker pools at all", &benchv1alpha1.WorkloadCluster{}},
		{"only a Generic pool", &benchv1alpha1.WorkloadCluster{Spec: benchv1alpha1.WorkloadClusterSpec{
			WorkerPools: []benchv1alpha1.WorkerPoolSpec{{Name: "workers"}},
		}}},
		{"pools for other roles only", &benchv1alpha1.WorkloadCluster{Spec: benchv1alpha1.WorkloadClusterSpec{
			WorkerPools: []benchv1alpha1.WorkerPoolSpec{
				{Name: "k6", Role: benchv1alpha1.WorkerPoolRoleK6},
				{Name: "mon", Role: benchv1alpha1.WorkerPoolRoleMonitoring},
			},
		}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if sel, tol := AuthentikPoolPlacement(tc.wc); len(sel) != 0 || len(tol) != 0 {
				t.Errorf("AuthentikPoolPlacement = %v, %v, want empty", sel, tol)
			}
		})
	}
}

// TestPoolPlacementTargetsItsOwnPool is the positive case: once a
// WorkloadCluster actually has a pool for a role, that role's placement
// helper returns a nodeSelector/toleration pair that actually matches what
// WorkerKubeadmConfigTemplate registers that pool's nodes with.
func TestPoolPlacementTargetsItsOwnPool(t *testing.T) {
	wc := &benchv1alpha1.WorkloadCluster{Spec: benchv1alpha1.WorkloadClusterSpec{
		WorkerPools: []benchv1alpha1.WorkerPoolSpec{
			{Name: "ak", Role: benchv1alpha1.WorkerPoolRoleAuthentik},
			{Name: "k6", Role: benchv1alpha1.WorkerPoolRoleK6},
			{Name: "mon", Role: benchv1alpha1.WorkerPoolRoleMonitoring},
		},
	}}

	for _, tt := range []struct {
		name  string
		fn    func(*benchv1alpha1.WorkloadCluster) (map[string]string, []corev1.Toleration)
		value string
	}{
		{"authentik", AuthentikPoolPlacement, "authentik"},
		{"k6", K6PoolPlacement, "k6"},
		{"monitoring", MonitoringPoolPlacement, "monitoring"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sel, tol := tt.fn(wc)
			if sel[NodePoolLabel] != tt.value {
				t.Errorf("nodeSelector[%s] = %q, want %q", NodePoolLabel, sel[NodePoolLabel], tt.value)
			}
			if len(tol) != 1 || tol[0].Key != NodePoolLabel || tol[0].Value != tt.value || tol[0].Effect != corev1.TaintEffectNoSchedule {
				t.Errorf("tolerations = %+v, want one matching {%s %s NoSchedule}", tol, NodePoolLabel, tt.value)
			}
		})
	}
}
