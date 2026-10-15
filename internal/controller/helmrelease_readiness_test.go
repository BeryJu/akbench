package controller

import (
	"testing"

	benchv1alpha1 "github.com/authentik-labs/akbench/api/v1alpha1"
	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestHelmReleaseReadiness(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := helmv2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	hr := func(generation, observed int64, ready *metav1.Condition) *helmv2.HelmRelease {
		h := &helmv2.HelmRelease{Name: "authentik", Namespace: "default", Generation: generation}
		h.Status.ObservedGeneration = observed
		if ready != nil {
			ready.Type = "Ready"
			h.Status.Conditions = []metav1.Condition{*ready}
		}
		return h
	}
	for name, tc := range map[string]struct {
		hr      *helmv2.HelmRelease
		status  metav1.ConditionStatus
		message string
	}{
		"first install running": {
			hr(1, -1, &metav1.Condition{Status: metav1.ConditionUnknown, ObservedGeneration: 1, Reason: "Progressing", Message: "Running 'install' action with timeout of 15m0s"}),
			metav1.ConditionFalse, "Running 'install' action with timeout of 15m0s",
		},
		"stale Ready=True from the previous generation": {
			hr(2, 1, &metav1.Condition{Status: metav1.ConditionTrue, ObservedGeneration: 1, Reason: "InstallSucceeded", Message: "Helm install succeeded"}),
			metav1.ConditionFalse, "waiting for helm-controller to pick up the latest spec",
		},
		"ready": {
			hr(2, 2, &metav1.Condition{Status: metav1.ConditionTrue, ObservedGeneration: 2, Reason: "UpgradeSucceeded"}),
			metav1.ConditionTrue, "HelmRelease is Ready",
		},
		"failed": {
			hr(1, 1, &metav1.Condition{Status: metav1.ConditionFalse, ObservedGeneration: 1, Reason: "InstallFailed", Message: "boom"}),
			metav1.ConditionFalse, "boom",
		},
	} {
		r := &AuthentikDeploymentReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(tc.hr).Build()}
		status, message, err := r.helmReleaseReadiness(t.Context(), tc.hr)
		if err != nil || status != tc.status || message != tc.message {
			t.Errorf("%s: got %s %q %v, want %s %q", name, status, message, err, tc.status, tc.message)
		}
	}
}

func TestRecordChartVersion(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := helmv2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	topo := &benchv1alpha1.AuthentikDeployment{Name: "authentik", Namespace: "default"}

	r := &AuthentikDeploymentReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).Build()}
	if r.recordChartVersion(t.Context(), topo); topo.Status.ChartVersion != "" {
		t.Fatalf("no HelmRelease yet: got %q, want empty", topo.Status.ChartVersion)
	}

	hr := &helmv2.HelmRelease{Name: "authentik", Namespace: "default"}
	hr.Status.History = helmv2.Snapshots{
		{Version: 1, ChartVersion: "2026.8.3", Status: "superseded"},
		{Version: 2, ChartVersion: "2026.8.4", Status: "deployed"},
	}
	r = &AuthentikDeploymentReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(hr).Build()}
	if r.recordChartVersion(t.Context(), topo); topo.Status.ChartVersion != "2026.8.4" {
		t.Fatalf("got %q, want 2026.8.4", topo.Status.ChartVersion)
	}
}
