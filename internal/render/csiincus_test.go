package render

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCSIIncusServiceObjects(t *testing.T) {
	objs, err := CSIIncusServiceObjects()
	if err != nil {
		t.Fatalf("CSIIncusServiceObjects: %v", err)
	}

	found := map[string]bool{
		"CSIDriver":          false,
		"ServiceAccount":     false,
		"ClusterRole":        false,
		"ClusterRoleBinding": false,
		"Deployment":         false,
		"DaemonSet":          false,
		"StorageClass":       false,
	}
	var sc *storagev1.StorageClass
	for _, obj := range objs {
		if obj.GetName() == "" {
			t.Errorf("object of kind %s has no name", obj.GetObjectKind().GroupVersionKind().Kind)
		}
		found[obj.GetObjectKind().GroupVersionKind().Kind] = true
		if s, ok := obj.(*storagev1.StorageClass); ok {
			sc = s
		}
	}
	for kind, ok := range found {
		if !ok {
			t.Errorf("expected a %s object in CSIIncusServiceObjects, found none", kind)
		}
	}

	if sc == nil {
		t.Fatal("no StorageClass found in CSIIncusServiceObjects")
	}
	if sc.Name != CSIIncusStorageClassName {
		t.Errorf("StorageClass name = %q, want %q", sc.Name, CSIIncusStorageClassName)
	}
	if sc.Annotations["storageclass.kubernetes.io/is-default-class"] != "true" {
		t.Errorf("StorageClass is-default-class annotation = %q, want %q", sc.Annotations["storageclass.kubernetes.io/is-default-class"], "true")
	}
	if sc.Parameters["pool"] != CSIIncusStoragePool {
		t.Errorf("StorageClass pool parameter = %q, want %q", sc.Parameters["pool"], CSIIncusStoragePool)
	}
	if sc.Parameters["contentType"] != "block" {
		t.Errorf("StorageClass contentType parameter = %q, want %q", sc.Parameters["contentType"], "block")
	}
}

func TestCSIIncusCredentialsSecretRemapsKeys(t *testing.T) {
	source := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "lxc-secret", Namespace: "default"},
		Data: map[string][]byte{
			"server":     []byte("https://incus.example.com:8443"),
			"client-crt": []byte("client-cert-bytes"),
			"client-key": []byte("client-key-bytes"),
			"server-crt": []byte("server-cert-bytes"),
			"project":    []byte("default"),
		},
	}

	secret := CSIIncusCredentialsSecret(source)

	if secret.Name != CSIIncusCredentialsName {
		t.Errorf("name = %q, want %q", secret.Name, CSIIncusCredentialsName)
	}
	if secret.Namespace != CSIIncusCredentialsNamespace {
		t.Errorf("namespace = %q, want %q", secret.Namespace, CSIIncusCredentialsNamespace)
	}

	want := map[string]string{
		"addr":        "https://incus.example.com:8443",
		"client-cert": "client-cert-bytes",
		"client-key":  "client-key-bytes",
		"server-cert": "server-cert-bytes",
	}
	for k, v := range want {
		if got := string(secret.Data[k]); got != v {
			t.Errorf("secret.Data[%q] = %q, want %q", k, got, v)
		}
	}
	if _, ok := secret.Data["project"]; ok {
		t.Errorf("secret.Data should not carry a project key, got %q", secret.Data["project"])
	}
}

func TestCSIIncusCredentialsSecretMissingServerCert(t *testing.T) {
	source := &corev1.Secret{
		Data: map[string][]byte{
			"server":     []byte("https://incus.example.com:8443"),
			"client-crt": []byte("client-cert-bytes"),
			"client-key": []byte("client-key-bytes"),
		},
	}

	secret := CSIIncusCredentialsSecret(source)
	if _, ok := secret.Data["server-cert"]; ok {
		t.Error("secret.Data should not carry a server-cert key when the source has none")
	}
}
