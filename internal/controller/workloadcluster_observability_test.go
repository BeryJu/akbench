package controller

import (
	"testing"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
	"github.com/goauthentik/authentik-bench/internal/render"
)

func remoteObservabilityClient(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	gv := schema.GroupVersion{Group: "monitoring.coreos.com", Version: "v1"}
	scheme.AddKnownTypeWithName(gv.WithKind("ServiceMonitor"), &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(gv.WithKind("ServiceMonitorList"), &unstructured.UnstructuredList{})
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

func inObservability(obj client.Object, name string, helm bool) client.Object {
	obj.SetName(name)
	obj.SetNamespace(render.ObservabilityNamespace)
	if helm {
		obj.SetLabels(map[string]string{"app.kubernetes.io/managed-by": "Helm"})
	}
	return obj
}

func TestRemoveLegacyObservabilityObjects(t *testing.T) {
	r, wc := addonFixture(t)
	legacySM := legacyServiceMonitor("grafana").(*unstructured.Unstructured)
	remote := remoteObservabilityClient(t,
		inObservability(&appsv1.Deployment{}, "grafana", false),
		inObservability(&corev1.Service{}, "grafana-nodeport", false),
		inObservability(&corev1.ConfigMap{}, "grafana-dashboards", false),
		legacySM,
		// Already the chart's: same name as a legacy object, but Helm's.
		inObservability(&corev1.Service{}, "grafana", true),
		// Unrelated to Grafana.
		inObservability(&appsv1.Deployment{}, "tempo", false),
	)

	if err := r.removeLegacyObservabilityObjects(t.Context(), wc, remote, render.GrafanaHelmReleaseName); err != nil {
		t.Fatal(err)
	}
	gone := func(obj client.Object, name string) bool {
		err := remote.Get(t.Context(), client.ObjectKey{Namespace: render.ObservabilityNamespace, Name: name}, obj)
		return apierrors.IsNotFound(err)
	}
	if !gone(&appsv1.Deployment{}, "grafana") || !gone(&corev1.Service{}, "grafana-nodeport") || !gone(&corev1.ConfigMap{}, "grafana-dashboards") {
		t.Error("legacy Grafana objects not removed")
	}
	sm := &unstructured.Unstructured{}
	sm.SetGroupVersionKind(legacySM.GroupVersionKind())
	if !gone(sm, "grafana") {
		t.Error("legacy ServiceMonitor not removed")
	}
	if gone(&corev1.Service{}, "grafana") {
		t.Error("the chart's own Service was deleted")
	}
	if gone(&appsv1.Deployment{}, "tempo") {
		t.Error("another add-on's object was deleted")
	}
}

func TestRemoveLegacyObservabilityObjectsStopsOnceReady(t *testing.T) {
	ready := func(wc *benchv1alpha1.WorkloadCluster, _ *runtime.Scheme) client.Object {
		hr := &helmv2.HelmRelease{Name: render.AddonHelmReleaseName(wc, render.TempoHelmReleaseName), Namespace: "default"}
		meta.SetStatusCondition(&hr.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "InstallSucceeded"})
		return hr
	}
	r, wc := addonFixture(t, ready)
	remote := remoteObservabilityClient(t, inObservability(&appsv1.Deployment{}, "tempo", false))
	if err := r.removeLegacyObservabilityObjects(t.Context(), wc, remote, render.TempoHelmReleaseName); err != nil {
		t.Fatal(err)
	}
	if err := remote.Get(t.Context(), client.ObjectKey{Namespace: render.ObservabilityNamespace, Name: "tempo"}, &appsv1.Deployment{}); err != nil {
		t.Errorf("object touched after the release was Ready: %v", err)
	}
}
