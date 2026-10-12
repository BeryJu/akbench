package controller

import (
	"maps"
	"testing"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	"github.com/goauthentik/authentik-bench/internal/render"
)

func TestSyncAutoscalerAnnotations(t *testing.T) {
	const revision = "machinedeployment.clusters.x-k8s.io/revision"
	md := &clusterv1.MachineDeployment{Annotations: map[string]string{
		revision: "3",
		render.ClusterAutoscalerMinSizeAnnotation:       "1",
		"capacity.cluster-autoscaler.kubernetes.io/cpu": "2",
	}}

	desired := map[string]string{
		render.ClusterAutoscalerMinSizeAnnotation: "0",
		render.ClusterAutoscalerMaxSizeAnnotation: "10",
	}
	if !syncAutoscalerAnnotations(md, desired) {
		t.Fatal("expected a change")
	}
	want := map[string]string{revision: "3", render.ClusterAutoscalerMinSizeAnnotation: "0", render.ClusterAutoscalerMaxSizeAnnotation: "10"}
	if !maps.Equal(md.Annotations, want) {
		t.Errorf("annotations = %v, want %v", md.Annotations, want)
	}
	if syncAutoscalerAnnotations(md, desired) {
		t.Error("second sync reported a change")
	}

	// Autoscaling turned off: ours go, Cluster API's stays.
	if !syncAutoscalerAnnotations(md, nil) || !maps.Equal(md.Annotations, map[string]string{revision: "3"}) {
		t.Errorf("annotations = %v", md.Annotations)
	}
}
