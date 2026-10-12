package render

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestPyroscopeObjectsMatchesDefaultPyroscopeHost(t *testing.T) {
	objs := PyroscopeObjects(nil)
	found := map[string]bool{"Namespace": false, "Deployment": false, "Service": false, "ServiceMonitor": false}
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
	// Must match defaultPyroscopeHost in authentikdeployment_controller.go
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

// TestPyroscopeServiceIsLabeledForItsServiceMonitor guards the actual bug
// class this whole feature depends on: a ServiceMonitor's spec.selector
// matches a Service's own .metadata.labels, not its .spec.selector (the pod
// selector) - a Service with no object-level labels would leave the
// ServiceMonitor matching nothing, silently.
func TestPyroscopeServiceIsLabeledForItsServiceMonitor(t *testing.T) {
	for _, obj := range PyroscopeObjects(nil) {
		if svc, ok := obj.(*corev1.Service); ok {
			if svc.Labels["app"] != PyroscopeServiceName {
				t.Errorf("pyroscope Service.Labels = %v, want app=%q so its ServiceMonitor can find it", svc.Labels, PyroscopeServiceName)
			}
		}
	}
}
