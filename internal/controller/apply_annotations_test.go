package controller

import (
	"maps"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

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

func TestCreateIfMissingRollsOutChangedDeployments(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	deploy := func(image string) *appsv1.Deployment {
		return &appsv1.Deployment{
			Name: "tempo", Namespace: "bench-system",
			Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "tempo", Image: image}}}}},
		}
	}
	if err := createIfMissing(t.Context(), c, deploy("tempo:1")); err != nil {
		t.Fatal(err)
	}
	if err := createIfMissing(t.Context(), c, deploy("tempo:2")); err != nil {
		t.Fatal(err)
	}
	var got appsv1.Deployment
	if err := c.Get(t.Context(), client.ObjectKey{Namespace: "bench-system", Name: "tempo"}, &got); err != nil {
		t.Fatal(err)
	}
	if got.Spec.Template.Spec.Containers[0].Image != "tempo:2" {
		t.Errorf("changed render not rolled out: %s", got.Spec.Template.Spec.Containers[0].Image)
	}
	// An unchanged render doesn't touch the live object.
	rv := got.ResourceVersion
	if err := createIfMissing(t.Context(), c, deploy("tempo:2")); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(t.Context(), client.ObjectKey{Namespace: "bench-system", Name: "tempo"}, &got); err != nil || got.ResourceVersion != rv {
		t.Errorf("unchanged render updated the Deployment (rv %s -> %s, err %v)", rv, got.ResourceVersion, err)
	}
}
