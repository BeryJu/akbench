package controller

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
	"github.com/goauthentik/authentik-bench/internal/incus"
	"github.com/goauthentik/authentik-bench/internal/render"
)

func endpointFixture(t *testing.T, host string, created time.Time) (*WorkloadClusterReconciler, *benchv1alpha1.WorkloadCluster, *clusterv1.Cluster) {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{clientgoscheme.AddToScheme, clusterv1.AddToScheme, benchv1alpha1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	gvk := render.LXCClusterGVK()
	scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(gvk.GroupVersion().WithKind(gvk.Kind+"List"), &unstructured.UnstructuredList{})

	wc := &benchv1alpha1.WorkloadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c1", Namespace: "default"},
		Spec: benchv1alpha1.WorkloadClusterSpec{
			Provider: benchv1alpha1.InfrastructureIncus,
			Incus:    &benchv1alpha1.IncusProviderSpec{LXCSecretRef: corev1.LocalObjectReference{Name: "lab"}},
		},
	}
	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c1", Namespace: "default", CreationTimestamp: metav1.NewTime(created)},
		Spec:       clusterv1.ClusterSpec{ControlPlaneEndpoint: clusterv1.APIEndpoint{Host: host, Port: 6443}},
	}
	lxcCluster := render.LXCCluster(wc)
	_ = unstructured.SetNestedField(lxcCluster.Object, host, "spec", "controlPlaneEndpoint", "host")
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "lab", Namespace: "default"},
		Data:       incus.Connection{Server: "https://incus:8443"}.SecretData(),
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(wc, cluster, lxcCluster, secret).Build()
	return &WorkloadClusterReconciler{Client: c, Scheme: scheme}, wc, cluster
}

func stubAddresses(t *testing.T, ipv4 ...string) *string {
	t.Helper()
	var asked string
	orig := lookupInstanceAddresses
	lookupInstanceAddresses = func(_ context.Context, _ incus.Connection, name string) ([]string, []string, error) {
		asked = name
		return ipv4, []string{"fd42::5"}, nil
	}
	t.Cleanup(func() { lookupInstanceAddresses = orig })
	return &asked
}

func lxcEndpointHost(t *testing.T, r *WorkloadClusterReconciler) string {
	t.Helper()
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(render.LXCClusterGVK())
	if err := r.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "c1"}, u); err != nil {
		t.Fatal(err)
	}
	host, _, _ := unstructured.NestedString(u.Object, "spec", "controlPlaneEndpoint", "host")
	return host
}

func TestPreferIPv4EndpointSwapsIPv6ForIPv4(t *testing.T) {
	r, wc, cluster := endpointFixture(t, "fd42::5", time.Now())
	asked := stubAddresses(t, "10.28.104.7")

	wait, err := r.preferIPv4Endpoint(t.Context(), wc, cluster)
	if err != nil || wait {
		t.Fatalf("wait %v err %v", wait, err)
	}
	if *asked != render.LoadBalancerInstanceName(wc) {
		t.Errorf("looked up instance %q", *asked)
	}
	var got clusterv1.Cluster
	if err := r.Get(t.Context(), client.ObjectKeyFromObject(cluster), &got); err != nil {
		t.Fatal(err)
	}
	if got.Spec.ControlPlaneEndpoint.Host != "10.28.104.7" || got.Spec.ControlPlaneEndpoint.Port != 6443 {
		t.Errorf("Cluster endpoint = %+v", got.Spec.ControlPlaneEndpoint)
	}
	if h := lxcEndpointHost(t, r); h != "10.28.104.7" {
		t.Errorf("LXCCluster endpoint host = %q", h)
	}
}

func TestPreferIPv4EndpointWaitsThenGivesUp(t *testing.T) {
	r, wc, cluster := endpointFixture(t, "fd42::5", time.Now())
	stubAddresses(t)
	if wait, err := r.preferIPv4Endpoint(t.Context(), wc, cluster); err != nil || !wait {
		t.Fatalf("young cluster without IPv4: wait %v err %v, want a requeue", wait, err)
	}

	r, wc, cluster = endpointFixture(t, "fd42::5", time.Now().Add(-ipv4EndpointGracePeriod-time.Minute))
	stubAddresses(t)
	if wait, err := r.preferIPv4Endpoint(t.Context(), wc, cluster); err != nil || wait {
		t.Fatalf("IPv6-only network: wait %v err %v, want the IPv6 endpoint kept", wait, err)
	}
	if h := lxcEndpointHost(t, r); h != "fd42::5" {
		t.Errorf("LXCCluster endpoint host = %q", h)
	}
}

func TestPreferIPv4EndpointLeavesIPv4Alone(t *testing.T) {
	r, wc, cluster := endpointFixture(t, "10.28.104.7", time.Now())
	asked := stubAddresses(t, "10.28.104.9")
	if wait, err := r.preferIPv4Endpoint(t.Context(), wc, cluster); err != nil || wait {
		t.Fatalf("wait %v err %v", wait, err)
	}
	if *asked != "" {
		t.Error("Incus queried for an endpoint that was already IPv4")
	}
}
