package render

import "testing"

func TestTempoObjectsMatchesDefaultTracingEndpoint(t *testing.T) {
	objs := TempoObjects()
	found := map[string]bool{"Namespace": false, "ConfigMap": false, "Deployment": false, "Service": false}
	for _, obj := range objs {
		if obj.GetName() == "" {
			t.Errorf("object of kind %T has no name", obj)
		}
		found[obj.GetObjectKind().GroupVersionKind().Kind] = true
	}
	for kind, ok := range found {
		if !ok {
			t.Errorf("expected a %s object in TempoObjects, found none", kind)
		}
	}
	// Must match defaultTracingEndpoint in authentiktopology_controller.go
	// ("http://tempo.bench-system.svc.cluster.local:4318") exactly.
	if TempoServiceName != "tempo" {
		t.Errorf("TempoServiceName = %q, want %q", TempoServiceName, "tempo")
	}
	if ObservabilityNamespace != "bench-system" {
		t.Errorf("ObservabilityNamespace = %q, want %q", ObservabilityNamespace, "bench-system")
	}
	if TempoOTLPHTTPPort != 4318 {
		t.Errorf("TempoOTLPHTTPPort = %d, want 4318", TempoOTLPHTTPPort)
	}
}
