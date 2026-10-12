package render

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestTempoObjectsMatchesDefaultTracingEndpoint(t *testing.T) {
	objs := TempoObjects(nil)
	found := map[string]bool{"Namespace": false, "ConfigMap": false, "Deployment": false, "Service": false, "ServiceMonitor": false}
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
	// Must match defaultTracingEndpoint in authentikdeployment_controller.go
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

// TestTempoObjectsExposesQueryPort guards against a real gap: only the OTLP
// ingest port (4318) was ever exposed here, so nothing - including a future
// Grafana Tempo datasource - could ever query traces back out.
func TestTempoObjectsExposesQueryPort(t *testing.T) {
	if TempoQueryPort != 3200 {
		t.Errorf("TempoQueryPort = %d, want 3200", TempoQueryPort)
	}
	for _, obj := range TempoObjects(nil) {
		svc, ok := obj.(*corev1.Service)
		if !ok {
			continue
		}
		found := false
		for _, p := range svc.Spec.Ports {
			if p.Port == TempoQueryPort {
				found = true
			}
		}
		if !found {
			t.Errorf("Tempo Service has no port %d (query API)", TempoQueryPort)
		}
	}
}

// TestTempoServiceMonitorTargetsQueryPort guards against scraping the wrong
// port: Tempo's /metrics lives on its HTTP API port (tempo-query/3200,
// server.http_listen_port), not the OTLP ingest port.
func TestTempoServiceMonitorTargetsQueryPort(t *testing.T) {
	for _, obj := range TempoObjects(nil) {
		u, ok := obj.(*unstructured.Unstructured)
		if !ok || u.GetKind() != serviceMonitorKind {
			continue
		}
		endpoints, found, err := unstructured.NestedSlice(u.Object, "spec", "endpoints")
		if err != nil || !found || len(endpoints) != 1 {
			t.Fatalf("spec.endpoints: found=%v err=%v len=%d", found, err, len(endpoints))
		}
		endpoint, ok := endpoints[0].(map[string]any)
		if !ok || endpoint["port"] != "tempo-query" {
			t.Errorf("Tempo ServiceMonitor endpoint = %v, want port=tempo-query", endpoint)
		}
		return
	}
	t.Fatal("no ServiceMonitor found in TempoObjects")
}

// TestTempoServiceIsLabeledForItsServiceMonitor guards the same
// selector-matches-object-labels requirement as Pyroscope's own test.
func TestTempoServiceIsLabeledForItsServiceMonitor(t *testing.T) {
	for _, obj := range TempoObjects(nil) {
		if svc, ok := obj.(*corev1.Service); ok {
			if svc.Labels["app"] != TempoServiceName {
				t.Errorf("tempo Service.Labels = %v, want app=%q so its ServiceMonitor can find it", svc.Labels, TempoServiceName)
			}
		}
	}
}
