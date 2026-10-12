package render

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestNewServiceMonitor(t *testing.T) {
	labels := map[string]string{"app": "widget"}
	sm := newServiceMonitor("widget", labels, "http")

	if sm.GetAPIVersion() != serviceMonitorAPIVersion {
		t.Errorf("apiVersion = %q, want %q", sm.GetAPIVersion(), serviceMonitorAPIVersion)
	}
	if sm.GetKind() != serviceMonitorKind {
		t.Errorf("kind = %q, want %q", sm.GetKind(), serviceMonitorKind)
	}
	if sm.GetName() != "widget" || sm.GetNamespace() != "bench-system" {
		t.Errorf("name/namespace = %q/%q, want widget/bench-system", sm.GetName(), sm.GetNamespace())
	}

	selector, found, err := unstructured.NestedStringMap(sm.Object, "spec", "selector", "matchLabels")
	if err != nil || !found {
		t.Fatalf("spec.selector.matchLabels: found=%v err=%v", found, err)
	}
	if selector["app"] != "widget" {
		t.Errorf("spec.selector.matchLabels = %v, want %v", selector, labels)
	}

	endpoints, found, err := unstructured.NestedSlice(sm.Object, "spec", "endpoints")
	if err != nil || !found || len(endpoints) != 1 {
		t.Fatalf("spec.endpoints: found=%v err=%v len=%d", found, err, len(endpoints))
	}
	endpoint, ok := endpoints[0].(map[string]any)
	if !ok {
		t.Fatalf("spec.endpoints[0] is not a map: %T", endpoints[0])
	}
	if endpoint["port"] != "http" {
		t.Errorf("spec.endpoints[0].port = %v, want http", endpoint["port"])
	}
	if endpoint["path"] != "/metrics" {
		t.Errorf("spec.endpoints[0].path = %v, want /metrics", endpoint["path"])
	}
}
