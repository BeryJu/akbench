package render

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

func TestCNPGClusterDefaults(t *testing.T) {
	topo := &benchv1alpha1.AuthentikTopology{
		ObjectMeta: metav1.ObjectMeta{Name: "authentik-1"},
		Spec: benchv1alpha1.AuthentikTopologySpec{
			Namespace: "authentik-1",
			Postgres:  benchv1alpha1.PostgresSpec{Mode: benchv1alpha1.PostgresCNPG},
		},
	}
	u := CNPGCluster(topo)

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
}

func TestCNPGClusterHonorsSpec(t *testing.T) {
	topo := &benchv1alpha1.AuthentikTopology{
		ObjectMeta: metav1.ObjectMeta{Name: "authentik-1"},
		Spec: benchv1alpha1.AuthentikTopologySpec{
			Postgres: benchv1alpha1.PostgresSpec{
				Mode: benchv1alpha1.PostgresCNPG,
				CNPG: &benchv1alpha1.CNPGPostgresSpec{Instances: 5, StorageSize: "50Gi", StorageClassName: "fast"},
			},
		},
	}
	u := CNPGCluster(topo)

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
