package controller

import (
	"errors"
	"fmt"
	"testing"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

func addonFixture(t *testing.T, objects ...func(*benchv1alpha1.WorkloadCluster, *runtime.Scheme) client.Object) (*WorkloadClusterReconciler, *benchv1alpha1.WorkloadCluster) {
	t.Helper()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{clientgoscheme.AddToScheme, helmv2.AddToScheme, benchv1alpha1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	wc := &benchv1alpha1.WorkloadCluster{Name: "c1", Namespace: "default", UID: "wc-uid"}
	b := fake.NewClientBuilder().WithScheme(scheme).WithObjects(wc)
	for _, o := range objects {
		b = b.WithObjects(o(wc, scheme))
	}
	return &WorkloadClusterReconciler{Client: b.Build(), Scheme: scheme}, wc
}

func helmRelease(name string, owned bool, generation, observed int64, ready *metav1.Condition) func(*benchv1alpha1.WorkloadCluster, *runtime.Scheme) client.Object {
	return func(wc *benchv1alpha1.WorkloadCluster, scheme *runtime.Scheme) client.Object {
		hr := &helmv2.HelmRelease{Name: name, Namespace: "default", Generation: generation}
		if owned {
			_ = controllerutil.SetControllerReference(wc, hr, scheme)
		}
		hr.Status.ObservedGeneration = observed
		if ready != nil {
			meta.SetStatusCondition(&hr.Status.Conditions, *ready)
		}
		return hr
	}
}

func TestAddonStateFromEnsureErrors(t *testing.T) {
	r, wc := addonFixture(t)
	noMatch := &meta.NoKindMatchError{GroupKind: schema.GroupKind{Group: "monitoring.coreos.com", Kind: "ServiceMonitor"}, SearchedVersions: []string{"v1"}}
	for name, tc := range map[string]struct {
		err  error
		want benchv1alpha1.AddonState
	}{
		"missing CRD":    {fmt.Errorf("get ServiceMonitor bench-system/tempo: %w", noMatch), benchv1alpha1.AddonWaiting},
		"discovery":      {errors.New("unable to retrieve the complete list of server APIs: monitoring.coreos.com/v1: no matches for monitoring.coreos.com/v1, Resource="), benchv1alpha1.AddonWaiting},
		"conflict":       {fmt.Errorf("apply descheduler HelmRelease: %w", apierrors.NewConflict(schema.GroupResource{Resource: "helmreleases"}, "descheduler", errors.New("modified"))), benchv1alpha1.AddonWaiting},
		"anything else":  {errors.New("get kubeconfig: secret not found"), benchv1alpha1.AddonFailed},
		"applied, no HR": {nil, benchv1alpha1.AddonReady},
	} {
		if got, msg := r.addonState(t.Context(), wc, "", tc.err); got != tc.want {
			t.Errorf("%s: state %s (%q), want %s", name, got, msg, tc.want)
		}
	}
}

func TestAddonStateFromHelmRelease(t *testing.T) {
	cond := func(status metav1.ConditionStatus, reason, msg string) *metav1.Condition {
		return &metav1.Condition{Type: "Ready", Status: status, Reason: reason, Message: msg}
	}
	for name, tc := range map[string]struct {
		hr   func(*benchv1alpha1.WorkloadCluster, *runtime.Scheme) client.Object
		want benchv1alpha1.AddonState
	}{
		"ready":          {helmRelease("hr", true, 2, 2, cond(metav1.ConditionTrue, "InstallSucceeded", "")), benchv1alpha1.AddonReady},
		"stale status":   {helmRelease("hr", true, 3, 2, cond(metav1.ConditionTrue, "InstallSucceeded", "")), benchv1alpha1.AddonProgressing},
		"no condition":   {helmRelease("hr", true, 1, 0, nil), benchv1alpha1.AddonProgressing},
		"installing":     {helmRelease("hr", true, 1, 1, cond(metav1.ConditionUnknown, "Progressing", "Running 'install' action")), benchv1alpha1.AddonProgressing},
		"install failed": {helmRelease("hr", true, 1, 1, cond(metav1.ConditionFalse, "InstallFailed", "boom")), benchv1alpha1.AddonFailed},
		"someone else's": {helmRelease("hr", false, 1, 1, cond(metav1.ConditionTrue, "InstallSucceeded", "")), benchv1alpha1.AddonFailed},
	} {
		r, wc := addonFixture(t, tc.hr)
		if got, msg := r.addonState(t.Context(), wc, "hr", nil); got != tc.want {
			t.Errorf("%s: state %s (%q), want %s", name, got, msg, tc.want)
		}
	}

	r, wc := addonFixture(t)
	if got, msg := r.addonState(t.Context(), wc, "hr", nil); got != benchv1alpha1.AddonReady || msg != "not needed" {
		t.Errorf("no HelmRelease applied: state %s (%q), want Ready/not needed", got, msg)
	}
}

func TestSetAddonStatusKeepsTransitionTime(t *testing.T) {
	wc := &benchv1alpha1.WorkloadCluster{}
	setAddonStatus(wc, "CNI", benchv1alpha1.AddonProgressing, "installing")
	first := metav1.NewTime(wc.Status.Addons[0].LastTransitionTime.Add(-60e9))
	wc.Status.Addons[0].LastTransitionTime = first

	setAddonStatus(wc, "CNI", benchv1alpha1.AddonProgressing, "still installing")
	if a := wc.Status.Addons[0]; !a.LastTransitionTime.Equal(&first) || a.Message != "still installing" {
		t.Errorf("same state: %+v", a)
	}
	setAddonStatus(wc, "CNI", benchv1alpha1.AddonReady, "")
	if a := wc.Status.Addons[0]; a.LastTransitionTime.Equal(&first) || a.State != benchv1alpha1.AddonReady {
		t.Errorf("new state: %+v", a)
	}
	setAddonStatus(wc, "Tempo", benchv1alpha1.AddonWaiting, "x")
	if len(wc.Status.Addons) != 2 || wc.Status.Addons[1].Name != "Tempo" {
		t.Errorf("addons = %+v", wc.Status.Addons)
	}
}

func TestMigrateLegacyAddonHelmRelease(t *testing.T) {
	// Owned by this cluster: suspended (so helm-controller won't uninstall
	// the release) and deleted. A finalizer stands in for helm-controller's
	// own, keeping the object around to inspect.
	legacy := func(wc *benchv1alpha1.WorkloadCluster, scheme *runtime.Scheme) client.Object {
		hr := helmRelease("flannel", true, 1, 1, nil)(wc, scheme)
		hr.SetFinalizers([]string{helmv2.HelmReleaseFinalizer})
		return hr
	}
	r, wc := addonFixture(t, legacy)
	if err := r.migrateLegacyAddonHelmRelease(t.Context(), wc, "flannel"); err != nil {
		t.Fatal(err)
	}
	var hr helmv2.HelmRelease
	if err := r.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "flannel"}, &hr); err != nil {
		t.Fatal(err)
	}
	if !hr.Spec.Suspend || hr.DeletionTimestamp.IsZero() {
		t.Errorf("legacy HelmRelease: suspend %v, deleting %v - want both", hr.Spec.Suspend, !hr.DeletionTimestamp.IsZero())
	}
	// Idempotent while helm-controller is still finalizing it.
	if err := r.migrateLegacyAddonHelmRelease(t.Context(), wc, "flannel"); err != nil {
		t.Fatal(err)
	}

	// Another cluster's (or anything else's): untouched.
	r, wc = addonFixture(t, helmRelease("flannel", false, 1, 1, nil))
	if err := r.migrateLegacyAddonHelmRelease(t.Context(), wc, "flannel"); err != nil {
		t.Fatal(err)
	}
	if err := r.Get(t.Context(), client.ObjectKey{Namespace: "default", Name: "flannel"}, &hr); err != nil || hr.Spec.Suspend {
		t.Errorf("foreign HelmRelease touched: err %v suspend %v", err, hr.Spec.Suspend)
	}

	// Nothing to migrate.
	r, wc = addonFixture(t)
	if err := r.migrateLegacyAddonHelmRelease(t.Context(), wc, "flannel"); err != nil {
		t.Fatal(err)
	}
}
