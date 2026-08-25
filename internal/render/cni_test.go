package render

import "testing"

func TestCNIObjectsParsesFlannelManifest(t *testing.T) {
	objs, err := CNIObjects()
	if err != nil {
		t.Fatalf("CNIObjects: %v", err)
	}
	wantKinds := map[string]bool{
		"Namespace": false, "ClusterRole": false, "ClusterRoleBinding": false,
		"ServiceAccount": false, "ConfigMap": false, "DaemonSet": false,
	}
	for _, obj := range objs {
		if obj.GetName() == "" {
			t.Errorf("object of kind %q has no name", obj.GetKind())
		}
		if _, ok := wantKinds[obj.GetKind()]; ok {
			wantKinds[obj.GetKind()] = true
		}
	}
	for kind, found := range wantKinds {
		if !found {
			t.Errorf("expected a %s object in the CNI manifest, found none", kind)
		}
	}
}
