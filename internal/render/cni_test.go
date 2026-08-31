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
		kind := obj.GetObjectKind().GroupVersionKind().Kind
		if obj.GetName() == "" {
			t.Errorf("object of kind %q has no name", kind)
		}
		if _, ok := wantKinds[kind]; ok {
			wantKinds[kind] = true
		}
	}
	for kind, found := range wantKinds {
		if !found {
			t.Errorf("expected a %s object in the CNI manifest, found none", kind)
		}
	}
}
