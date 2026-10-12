package render

import (
	"maps"

	corev1 "k8s.io/api/core/v1"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// NodePoolLabel is both the node label a pool's kubelets register themselves
// with (see workerNodeRegistrationArgs, internal/render/workloadcluster.go)
// and the taint key WorkerKubeadmConfigTemplate applies to the same pool's
// nodes (see poolTaints) - a workload's nodeSelector for this label is what
// actually *attracts* it to the pool; a toleration only ever *permits*
// scheduling onto a tainted node, it never requests one. Every
// pool-targeted workload in this package sets both together.
const NodePoolLabel = "bench.goauthentik.io/pool"

// poolLabelValue is the node label/taint value for a WorkerPoolRole -
// WorkerPoolRoleGeneric (and any future role this package doesn't know
// about) deliberately has no value: Generic pools carry no label and no
// taint at all.
func poolLabelValue(role benchv1alpha1.WorkerPoolRole) string {
	switch role {
	case benchv1alpha1.WorkerPoolRoleAuthentik:
		return "authentik"
	case benchv1alpha1.WorkerPoolRoleK6:
		return "k6"
	case benchv1alpha1.WorkerPoolRoleMonitoring:
		return "monitoring"
	default:
		return ""
	}
}

// poolTaint is the one taint WorkerKubeadmConfigTemplate registers a
// non-Generic pool's nodes with - always NoSchedule (not NoExecute: a node
// that already has the wrong pod running on it, e.g. right after a pool's
// role changes, shouldn't be forcibly evicted by the taint itself - the
// descheduler addon's own RemovePodsViolatingNodeTaints strategy is what
// corrects that instead, on its own schedule rather than immediately).
func poolTaint(role benchv1alpha1.WorkerPoolRole) *corev1.Taint {
	value := poolLabelValue(role)
	if value == "" {
		return nil
	}
	return &corev1.Taint{Key: NodePoolLabel, Value: value, Effect: corev1.TaintEffectNoSchedule}
}

// poolPlacement returns the nodeSelector/toleration pair that targets a
// pool reserved for role - nil/nil for Generic (or any role with no
// node-pool wired up at all on this WorkloadCluster), meaning "no pool
// preference, schedule anywhere the default scheduler allows," which is
// also what every workload in this package falls back to when wc doesn't
// (yet, or ever) have a pool for that role - see WorkerPoolSpec.Role's own
// doc comment for why this check matters for backward compatibility.
func poolPlacement(wc *benchv1alpha1.WorkloadCluster, role benchv1alpha1.WorkerPoolRole) (map[string]string, []corev1.Toleration) {
	if wc == nil || !wc.HasPoolRole(role) {
		return nil, nil
	}
	value := poolLabelValue(role)
	if value == "" {
		return nil, nil
	}
	return map[string]string{NodePoolLabel: value},
		[]corev1.Toleration{{Key: NodePoolLabel, Operator: corev1.TolerationOpEqual, Value: value, Effect: corev1.TaintEffectNoSchedule}}
}

// AuthentikPoolPlacement/K6PoolPlacement/MonitoringPoolPlacement are the
// three fixed pools this project wires its own workloads to by name - an
// AuthentikDeployment's server/worker/postgres pods, a TestCase's k6 runner,
// and this operator's own observability/addon stack, respectively. wc may be
// nil (a deployment/runner with no clusterRef, i.e. running on the
// management cluster, which this node-pool scheme doesn't apply to at all).
func AuthentikPoolPlacement(wc *benchv1alpha1.WorkloadCluster) (map[string]string, []corev1.Toleration) {
	return poolPlacement(wc, benchv1alpha1.WorkerPoolRoleAuthentik)
}

func K6PoolPlacement(wc *benchv1alpha1.WorkloadCluster) (map[string]string, []corev1.Toleration) {
	return poolPlacement(wc, benchv1alpha1.WorkerPoolRoleK6)
}

func MonitoringPoolPlacement(wc *benchv1alpha1.WorkloadCluster) (map[string]string, []corev1.Toleration) {
	return poolPlacement(wc, benchv1alpha1.WorkerPoolRoleMonitoring)
}

// mergeStringMaps returns a new map holding every key from base, then every
// key from override (override wins on a collision - none is expected here,
// since callers only ever combine two maps keyed on different label
// namespaces, but last-wins is the unsurprising behavior if one ever
// occurs). Either argument may be nil.
func mergeStringMaps(base, override map[string]string) map[string]string {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}
	out := make(map[string]string, len(base)+len(override))
	maps.Copy(out, base)
	maps.Copy(out, override)
	return out
}
