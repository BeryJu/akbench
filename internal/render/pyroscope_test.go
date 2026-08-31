package render

import "testing"

func TestPyroscopeObjectsMatchesDefaultPyroscopeHost(t *testing.T) {
	objs := PyroscopeObjects()
	found := map[string]bool{"Namespace": false, "Deployment": false, "Service": false}
	for _, obj := range objs {
		if obj.GetName() == "" {
			t.Errorf("object of kind %T has no name", obj)
		}
		found[obj.GetObjectKind().GroupVersionKind().Kind] = true
	}
	for kind, ok := range found {
		if !ok {
			t.Errorf("expected a %s object in PyroscopeObjects, found none", kind)
		}
	}
	// Must match defaultPyroscopeHost in authentiktopology_controller.go
	// ("http://pyroscope.bench-system.svc.cluster.local:4040") exactly.
	if PyroscopeServiceName != "pyroscope" {
		t.Errorf("PyroscopeServiceName = %q, want %q", PyroscopeServiceName, "pyroscope")
	}
	if ObservabilityNamespace != "bench-system" {
		t.Errorf("ObservabilityNamespace = %q, want %q", ObservabilityNamespace, "bench-system")
	}
	if PyroscopePort != 4040 {
		t.Errorf("PyroscopePort = %d, want 4040", PyroscopePort)
	}
}
