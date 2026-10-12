package api

import (
	"net/http"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
	"github.com/goauthentik/authentik-bench/internal/incus"
)

func incusCluster(name, secret string) *benchv1alpha1.WorkloadCluster {
	return &benchv1alpha1.WorkloadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec: benchv1alpha1.WorkloadClusterSpec{
			Provider: benchv1alpha1.InfrastructureIncus,
			Incus:    &benchv1alpha1.IncusProviderSpec{LXCSecretRef: corev1.LocalObjectReference{Name: secret}},
		},
	}
}

func TestIncusCredentialLifecycle(t *testing.T) {
	s, k8s := newServer(t, Config{})

	// Create: no client certificate given, so one is generated.
	rec := do(t, s, http.MethodPost, "/api/v1/infrastructure/incus/credentials", incusCredentialInput{
		Name: "lab", Server: " https://incus.lab:8443/ ", Project: "bench",
	})
	requireStatus(t, rec, http.StatusCreated)
	created := decodeBody[incusCredential](t, rec)
	if created.Server != "https://incus.lab:8443" || !created.HasClientKey || created.ClientCertFingerprint == "" || !created.Labeled {
		t.Fatalf("created = %+v", created)
	}
	if strings.Contains(rec.Body.String(), "PRIVATE KEY") {
		t.Fatal("client key returned by the API")
	}

	var secret corev1.Secret
	if err := k8s.Get(t.Context(), types.NamespacedName{Namespace: testNamespace, Name: "lab"}, &secret); err != nil {
		t.Fatal(err)
	}
	stored := incus.FromSecret(&secret)
	if stored.Project != "bench" || stored.ClientKey == "" {
		t.Errorf("stored = %+v", stored)
	}

	// Update without a certificate keeps the stored one; pinning a server
	// certificate and switching projects is applied.
	serverCert, _, err := incus.GenerateClientCert("server")
	if err != nil {
		t.Fatal(err)
	}
	rec = do(t, s, http.MethodPut, "/api/v1/infrastructure/incus/credentials/lab", incusCredentialInput{
		Server: "https://incus.lab:8443", Project: "other", ServerCert: serverCert,
	})
	requireStatus(t, rec, http.StatusOK)
	updated := decodeBody[incusCredential](t, rec)
	if updated.ClientCertFingerprint != created.ClientCertFingerprint || updated.Project != "other" || updated.ServerCert == "" {
		t.Errorf("updated = %+v", updated)
	}

	// Regenerating replaces it.
	rec = do(t, s, http.MethodPut, "/api/v1/infrastructure/incus/credentials/lab", incusCredentialInput{
		Server: "https://incus.lab:8443", RegenerateClientCert: true,
	})
	requireStatus(t, rec, http.StatusOK)
	if decodeBody[incusCredential](t, rec).ClientCertFingerprint == created.ClientCertFingerprint {
		t.Error("client certificate not regenerated")
	}

	requireStatus(t, do(t, s, http.MethodDelete, "/api/v1/infrastructure/incus/credentials/lab", nil), http.StatusNoContent)
	requireStatus(t, do(t, s, http.MethodGet, "/api/v1/infrastructure/incus/credentials/lab", nil), http.StatusNotFound)
}

func TestIncusCredentialValidation(t *testing.T) {
	s, _ := newServer(t, Config{})
	for name, in := range map[string]incusCredentialInput{
		"bad name":        {Name: "Not_Valid", Server: "https://incus:8443"},
		"http server":     {Name: "a", Server: "http://incus:8443"},
		"cert, no key":    {Name: "a", Server: "https://incus:8443", ClientCert: "x"},
		"garbage pair":    {Name: "a", Server: "https://incus:8443", ClientCert: "x", ClientKey: "y"},
		"bring and regen": {Name: "a", Server: "https://incus:8443", ClientCert: "x", ClientKey: "y", RegenerateClientCert: true},
	} {
		if rec := do(t, s, http.MethodPost, "/api/v1/infrastructure/incus/credentials", in); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d (%s)", name, rec.Code, rec.Body.String())
		}
	}
}

func TestIncusCredentialsIncludeReferencedSecrets(t *testing.T) {
	cert, key, err := incus.GenerateClientCert("migrated")
	if err != nil {
		t.Fatal(err)
	}
	// A Secret created by hand on a `main`-era setup: no label, but a
	// WorkloadCluster uses it - listed, and protected from deletion.
	migrated := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "lxc-secret", Namespace: testNamespace},
		Data:       incus.Connection{Server: "https://incus:8443", ClientCert: cert, ClientKey: key}.SecretData(),
	}
	unrelated := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "db-password", Namespace: testNamespace},
		Data:       map[string][]byte{"password": []byte("hunter2")},
	}
	s, _ := newServer(t, Config{}, migrated, unrelated, incusCluster("wc1", "lxc-secret"))

	rec := do(t, s, http.MethodGet, "/api/v1/infrastructure/incus/credentials", nil)
	requireStatus(t, rec, http.StatusOK)
	list := decodeBody[struct{ Items []incusCredential }](t, rec)
	if len(list.Items) != 1 || list.Items[0].Name != "lxc-secret" || list.Items[0].Labeled || list.Items[0].UsedBy[0] != "wc1" {
		t.Fatalf("list = %+v", list.Items)
	}

	rec = do(t, s, http.MethodDelete, "/api/v1/infrastructure/incus/credentials/lxc-secret", nil)
	requireStatus(t, rec, http.StatusConflict)
	if !strings.Contains(rec.Body.String(), "wc1") {
		t.Errorf("body = %s", rec.Body.String())
	}

	// Arbitrary Secrets can't be read or overwritten through these endpoints.
	requireStatus(t, do(t, s, http.MethodGet, "/api/v1/infrastructure/incus/credentials/db-password", nil), http.StatusNotFound)
	requireStatus(t, do(t, s, http.MethodPut, "/api/v1/infrastructure/incus/credentials/db-password",
		incusCredentialInput{Server: "https://incus:8443"}), http.StatusNotFound)
}
