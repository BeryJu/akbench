package render

import "testing"

func TestPrometheusObjectsHasExpectedKinds(t *testing.T) {
	objs := PrometheusObjects()
	wantKinds := map[string]bool{
		"Namespace": false, "ConfigMap": false, "Deployment": false, "Service": false,
	}
	nodePortServices := 0
	for _, obj := range objs {
		if obj.GetName() == "" {
			t.Errorf("object of kind %T has no name", obj)
		}
		kind := obj.GetObjectKind().GroupVersionKind().Kind
		if _, ok := wantKinds[kind]; ok {
			wantKinds[kind] = true
		}
		if obj.GetName() == PrometheusNodePortServiceName {
			nodePortServices++
		}
	}
	for kind, found := range wantKinds {
		if !found {
			t.Errorf("expected a %s object in PrometheusObjects, found none", kind)
		}
	}
	if nodePortServices != 1 {
		t.Errorf("expected exactly one %s Service, found %d", PrometheusNodePortServiceName, nodePortServices)
	}
}
