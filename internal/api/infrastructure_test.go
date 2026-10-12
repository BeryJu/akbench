package api

import (
	"net/http"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/goauthentik/authentik-bench/internal/appliance"
)

func applianceStatus(shipped string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: appliance.StatusConfigMap, Namespace: appliance.SystemNamespace},
		Data: map[string]string{
			appliance.StatusKeyShipped:  shipped,
			appliance.StatusKeyError:    "",
			appliance.StatusKeySyncedAt: "2026-10-12T10:00:00Z",
		},
	}
}

func findProvider(t *testing.T, view infrastructureView, id string) infrastructureProvider {
	t.Helper()
	for _, p := range view.Providers {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("provider %q missing from %+v", id, view.Providers)
	return infrastructureProvider{}
}

func TestInfrastructureOutsideAppliance(t *testing.T) {
	// No clusterctl inventory CRD in the fake scheme at all: treated as
	// nothing installed rather than an error.
	s, _ := newServer(t, Config{})
	rec := do(t, s, http.MethodGet, "/api/v1/infrastructure", nil)
	requireStatus(t, rec, http.StatusOK)
	view := decodeBody[infrastructureView](t, rec)
	if view.Managed {
		t.Error("managed outside the appliance")
	}
	p := findProvider(t, view, "incus")
	if p.ClusterProvider != "Incus" || p.InstalledVersion != "" || p.Enabled {
		t.Errorf("incus = %+v", p)
	}

	rec = do(t, s, http.MethodPost, "/api/v1/infrastructure/providers/incus/enable", nil)
	requireStatus(t, rec, http.StatusConflict)
	if !strings.Contains(rec.Body.String(), "clusterctl init --infrastructure incus") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestInfrastructureEnableInAppliance(t *testing.T) {
	s, k8s := newServer(t, Config{Appliance: true}, applianceStatus(`{"incus":"v0.9.1","aws":"v2.9.0"}`))

	view := decodeBody[infrastructureView](t, do(t, s, http.MethodGet, "/api/v1/infrastructure", nil))
	if !view.Managed || view.SyncedAt == "" {
		t.Fatalf("view = %+v", view)
	}
	if p := findProvider(t, view, "aws"); p.AvailableVersion != "v2.9.0" || p.ClusterProvider != "" {
		t.Errorf("aws = %+v (shipped, but not something WorkloadCluster can use yet)", p)
	}

	rec := do(t, s, http.MethodPost, "/api/v1/infrastructure/providers/incus/enable", nil)
	requireStatus(t, rec, http.StatusOK)
	if p := findProvider(t, decodeBody[infrastructureView](t, rec), "incus"); !p.Enabled || p.AvailableVersion != "v0.9.1" {
		t.Errorf("incus after enable = %+v", p)
	}
	// Enabling twice and then a second provider appends without duplicates.
	requireStatus(t, do(t, s, http.MethodPost, "/api/v1/infrastructure/providers/incus/enable", nil), http.StatusOK)
	requireStatus(t, do(t, s, http.MethodPost, "/api/v1/infrastructure/providers/aws/enable", nil), http.StatusOK)

	var cm corev1.ConfigMap
	if err := k8s.Get(t.Context(), types.NamespacedName{Namespace: appliance.SystemNamespace, Name: appliance.ProvidersConfigMap}, &cm); err != nil {
		t.Fatal(err)
	}
	if got := cm.Data[appliance.ProvidersKeyInfrastructure]; got != "incus,aws" {
		t.Errorf("desired providers = %q, want incus,aws", got)
	}

	requireStatus(t, do(t, s, http.MethodPost, "/api/v1/infrastructure/providers/vsphere/enable", nil), http.StatusNotFound)
}

func TestInfrastructureReportsInstalledProviders(t *testing.T) {
	scheme := testScheme(t)
	gv := schema.GroupVersion{Group: "clusterctl.cluster.x-k8s.io", Version: "v1alpha3"}
	scheme.AddKnownTypeWithName(gv.WithKind("Provider"), &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(gv.WithKind("ProviderList"), &unstructured.UnstructuredList{})
	inventory := func(name, typ, provider, version string) *unstructured.Unstructured {
		u := &unstructured.Unstructured{Object: map[string]any{"type": typ, "providerName": provider, "version": version}}
		u.SetGroupVersionKind(gv.WithKind("Provider"))
		u.SetName(name)
		u.SetNamespace(provider + "-system")
		return u
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		inventory("cluster-api", "CoreProvider", "cluster-api", "v1.14.3"),
		inventory("infrastructure-incus", "InfrastructureProvider", "incus", "v0.9.1"),
		inventory("infrastructure-docker", "InfrastructureProvider", "docker", "v1.14.3"),
	).Build()
	s := New(c, Config{Namespace: testNamespace, Token: testToken})

	view := decodeBody[infrastructureView](t, do(t, s, http.MethodGet, "/api/v1/infrastructure", nil))
	if p := findProvider(t, view, "incus"); p.InstalledVersion != "v0.9.1" {
		t.Errorf("incus = %+v", p)
	}
	// Installed by hand, unknown to akbench: still reported, just not usable.
	if p := findProvider(t, view, "docker"); p.InstalledVersion != "v1.14.3" || p.ClusterProvider != "" {
		t.Errorf("docker = %+v", p)
	}
	for _, p := range view.Providers {
		if p.ID == "cluster-api" {
			t.Error("core provider listed as infrastructure")
		}
	}
}
