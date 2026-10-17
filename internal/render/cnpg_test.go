package render

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	benchv1alpha1 "github.com/authentik-labs/akbench/api/v1alpha1"
)

// TestCNPGClusterTargetsAuthentikPool guards the manual
// corev1.Toleration->unstructured conversion in CNPGCluster: a
// WorkloadCluster with an authentik-role pool must produce a real
// spec.affinity.nodeSelector/tolerations on the rendered Cluster, in the
// exact shape CNPG's own CRD expects.
func TestCNPGClusterTargetsAuthentikPool(t *testing.T) {
	topo := &benchv1alpha1.AuthentikDeployment{
		Name: "authentik-1",
		Spec: benchv1alpha1.AuthentikDeploymentSpec{
			Namespace:  "authentik-1",
			ClusterRef: corev1.LocalObjectReference{Name: "wc-1"},
			Postgres:   benchv1alpha1.PostgresSpec{Mode: benchv1alpha1.PostgresCNPG},
		},
	}
	wc := &benchv1alpha1.WorkloadCluster{Spec: benchv1alpha1.WorkloadClusterSpec{
		WorkerPools: []benchv1alpha1.WorkerPoolSpec{{Name: "ak", Role: benchv1alpha1.WorkerPoolRoleAuthentik}},
	}}
	u := CNPGCluster(topo, wc)

	sel, _, _ := unstructured.NestedStringMap(u.Object, "spec", "affinity", "nodeSelector")
	if sel[NodePoolLabel] != "authentik" {
		t.Errorf("spec.affinity.nodeSelector[%s] = %q, want authentik", NodePoolLabel, sel[NodePoolLabel])
	}
	tolerations, _, _ := unstructured.NestedSlice(u.Object, "spec", "affinity", "tolerations")
	if len(tolerations) != 1 {
		t.Fatalf("spec.affinity.tolerations = %v, want exactly one", tolerations)
	}
	tol, ok := tolerations[0].(map[string]any)
	if !ok || tol["key"] != NodePoolLabel || tol["value"] != "authentik" || tol["effect"] != string(corev1.TaintEffectNoSchedule) {
		t.Errorf("tolerations[0] = %+v, want a toleration matching {%s authentik NoSchedule}", tolerations[0], NodePoolLabel)
	}
}

func TestCNPGClusterDefaults(t *testing.T) {
	topo := &benchv1alpha1.AuthentikDeployment{
		Name: "authentik-1",
		Spec: benchv1alpha1.AuthentikDeploymentSpec{
			Namespace:  "authentik-1",
			ClusterRef: corev1.LocalObjectReference{Name: "wc-1"},
			Postgres:   benchv1alpha1.PostgresSpec{Mode: benchv1alpha1.PostgresCNPG},
		},
	}
	u := CNPGCluster(topo, nil)

	if got, want := u.GetName(), "authentik-1-postgres"; got != want {
		t.Errorf("name = %q, want %q", got, want)
	}
	instances, _, _ := unstructured.NestedInt64(u.Object, "spec", "instances")
	if instances != 3 {
		t.Errorf("default instances = %d, want 3", instances)
	}
	size, _, _ := unstructured.NestedString(u.Object, "spec", "storage", "size")
	if size != defaultCNPGStorageSize {
		t.Errorf("default storage size = %q, want %q", size, defaultCNPGStorageSize)
	}
	if got, want := CNPGAppSecretName(topo), "authentik-1-postgres-app"; got != want {
		t.Errorf("app secret name = %q, want %q", got, want)
	}

	// enablePodMonitor makes CNPG's own operator create a PodMonitor -
	// closes the gap docs/grafana-dashboards.md documents ("postgres.json
	// is empty until something scrapes pg_* series").
	enablePodMonitor, _, _ := unstructured.NestedBool(u.Object, "spec", "monitoring", "enablePodMonitor")
	if !enablePodMonitor {
		t.Error("spec.monitoring.enablePodMonitor = false, want true")
	}
}

// TestCNPGClusterNoPodMonitorWithoutClusterRef guards the same regression as
// helmrelease_test.go's TestHelmValuesNoServiceMonitorWithoutClusterRef: the
// PodMonitor CRD only exists on a WorkloadCluster (installed alongside
// ServiceMonitor by kube-prometheus-stack), never the management cluster a
// clusterRef-less deployment falls back to.
func TestCNPGClusterNoPodMonitorWithoutClusterRef(t *testing.T) {
	topo := &benchv1alpha1.AuthentikDeployment{
		Name: "authentik-1",
		Spec: benchv1alpha1.AuthentikDeploymentSpec{
			Namespace: "authentik-1",
			Postgres:  benchv1alpha1.PostgresSpec{Mode: benchv1alpha1.PostgresCNPG},
		},
	}
	u := CNPGCluster(topo, nil)

	if _, found, _ := unstructured.NestedBool(u.Object, "spec", "monitoring", "enablePodMonitor"); found {
		t.Error("spec.monitoring.enablePodMonitor was set with no clusterRef - the PodMonitor CRD won't exist on the management cluster")
	}
}

func TestCNPGClusterHonorsSpec(t *testing.T) {
	topo := &benchv1alpha1.AuthentikDeployment{
		Name: "authentik-1",
		Spec: benchv1alpha1.AuthentikDeploymentSpec{
			Postgres: benchv1alpha1.PostgresSpec{
				Mode: benchv1alpha1.PostgresCNPG,
				CNPG: &benchv1alpha1.CNPGPostgresSpec{Instances: 5, StorageSize: "50Gi", StorageClassName: "fast"},
			},
		},
	}
	u := CNPGCluster(topo, nil)

	instances, _, _ := unstructured.NestedInt64(u.Object, "spec", "instances")
	if instances != 5 {
		t.Errorf("instances = %d, want 5", instances)
	}
	size, _, _ := unstructured.NestedString(u.Object, "spec", "storage", "size")
	if size != "50Gi" {
		t.Errorf("storage size = %q, want 50Gi", size)
	}
	class, _, _ := unstructured.NestedString(u.Object, "spec", "storage", "storageClass")
	if class != "fast" {
		t.Errorf("storage class = %q, want fast", class)
	}
}

func TestCNPGClusterCrossDeploymentIsolation(t *testing.T) {
	topo := &benchv1alpha1.AuthentikDeployment{
		Name: "authentik-1",
		Spec: benchv1alpha1.AuthentikDeploymentSpec{
			Namespace: "authentik-1",
			Postgres:  benchv1alpha1.PostgresSpec{Mode: benchv1alpha1.PostgresCNPG},
		},
	}
	u := CNPGCluster(topo, nil)

	labels, _, _ := unstructured.NestedStringMap(u.Object, "spec", "inheritedMetadata", "labels")
	if labels["bench.goauthentik.io/deployment"] != "authentik-1" {
		t.Errorf("spec.inheritedMetadata.labels = %v, want bench.goauthentik.io/deployment=authentik-1", labels)
	}

	terms, _, _ := unstructured.NestedSlice(u.Object, "spec", "affinity", "additionalPodAntiAffinity", "requiredDuringSchedulingIgnoredDuringExecution")
	if len(terms) != 1 {
		t.Fatalf("spec.affinity.additionalPodAntiAffinity: got %d required terms, want 1", len(terms))
	}
	term, ok := terms[0].(map[string]any)
	if !ok {
		t.Fatalf("required term is not a map: %v", terms[0])
	}
	if term["topologyKey"] != "kubernetes.io/hostname" {
		t.Errorf("required term topologyKey = %v, want kubernetes.io/hostname", term["topologyKey"])
	}
}

func TestCNPGReady(t *testing.T) {
	notReady := &unstructured.Unstructured{Object: map[string]any{}}
	if CNPGReady(notReady) {
		t.Error("CNPGReady on an object with no status should be false")
	}

	ready := &unstructured.Unstructured{Object: map[string]any{
		"status": map[string]any{
			"conditions": []any{
				map[string]any{"type": "ContinuousArchiving", "status": "True"},
				map[string]any{"type": "Ready", "status": "True"},
			},
		},
	}}
	if !CNPGReady(ready) {
		t.Error("CNPGReady should be true when the Ready condition is True")
	}

	falseReady := &unstructured.Unstructured{Object: map[string]any{
		"status": map[string]any{
			"conditions": []any{
				map[string]any{"type": "Ready", "status": "False"},
			},
		},
	}}
	if CNPGReady(falseReady) {
		t.Error("CNPGReady should be false when the Ready condition is False")
	}
}
