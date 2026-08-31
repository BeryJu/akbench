package render

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestMetricsServerObjectsParsesManifest(t *testing.T) {
	objs, err := MetricsServerObjects()
	if err != nil {
		t.Fatalf("MetricsServerObjects: %v", err)
	}
	wantKinds := map[string]bool{
		"ServiceAccount": false, "ClusterRole": false, "ClusterRoleBinding": false,
		"RoleBinding": false, "Service": false, "Deployment": false, "APIService": false,
	}
	var deployment *unstructured.Unstructured
	for _, obj := range objs {
		kind := obj.GetObjectKind().GroupVersionKind().Kind
		if obj.GetName() == "" {
			t.Errorf("object of kind %q has no name", kind)
		}
		if _, ok := wantKinds[kind]; ok {
			wantKinds[kind] = true
		}
		if kind == "Deployment" {
			if u, ok := obj.(*unstructured.Unstructured); ok {
				deployment = u
			}
		}
	}
	for kind, found := range wantKinds {
		if !found {
			t.Errorf("expected a %s object in the metrics-server manifest, found none", kind)
		}
	}

	// kubeadm clusters don't wire up a kubelet serving certificate signed by
	// a CA metrics-server trusts by default - without this flag every
	// scrape fails and `kubectl top` never returns data. Guards against a
	// future re-vendor of components.yaml silently dropping the patch.
	if deployment == nil {
		t.Fatal("no Deployment found in the metrics-server manifest")
	}
	containers, _, err := unstructured.NestedSlice(deployment.Object, "spec", "template", "spec", "containers")
	if err != nil {
		t.Fatalf("read Deployment containers: %v", err)
	}
	found := false
	for _, c := range containers {
		container, ok := c.(map[string]any)
		if !ok {
			continue
		}
		args, _, _ := unstructured.NestedSlice(container, "args")
		for _, a := range args {
			if a == "--kubelet-insecure-tls" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected --kubelet-insecure-tls in the metrics-server container args")
	}
}
