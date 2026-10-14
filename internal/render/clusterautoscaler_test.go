package render

import (
	"encoding/json"
	"testing"

	benchv1alpha1 "github.com/authentik-labs/akbench/api/v1alpha1"
)

func exampleAutoscalingCluster() *benchv1alpha1.WorkloadCluster {
	return &benchv1alpha1.WorkloadCluster{
		Name: "wc-1", Namespace: "default",
		Spec: benchv1alpha1.WorkloadClusterSpec{
			WorkerPools: []benchv1alpha1.WorkerPoolSpec{
				{Name: "workers", Replicas: 1, Autoscaling: &benchv1alpha1.AutoscalingSpec{MinReplicas: 1, MaxReplicas: 4}},
			},
		},
	}
}

func TestHasAutoscalingPool(t *testing.T) {
	if !HasAutoscalingPool(exampleAutoscalingCluster()) {
		t.Error("expected HasAutoscalingPool to be true when a pool sets autoscaling")
	}

	fixed := &benchv1alpha1.WorkloadCluster{
		Spec: benchv1alpha1.WorkloadClusterSpec{
			WorkerPools: []benchv1alpha1.WorkerPoolSpec{{Name: "workers", Replicas: 1}},
		},
	}
	if HasAutoscalingPool(fixed) {
		t.Error("expected HasAutoscalingPool to be false when no pool sets autoscaling")
	}
}

// TestClusterAutoscalerHelmValues guards the exact values verified live
// against the chart's own templates/deployment.yaml (see
// ClusterAutoscalerHelmRelease's doc comment): clusterAPIMode must be
// "kubeconfig-incluster" (not the more intuitive-sounding
// "incluster-kubeconfig"), and clusterAPIKubeconfigSecret must name the
// exact "<cluster>-kubeconfig" Secret CAPI writes.
func TestClusterAutoscalerHelmValues(t *testing.T) {
	wc := exampleAutoscalingCluster()
	raw, err := ClusterAutoscalerHelmValues(wc)
	if err != nil {
		t.Fatalf("ClusterAutoscalerHelmValues: %v", err)
	}

	var values map[string]any
	if err := json.Unmarshal(raw.Raw, &values); err != nil {
		t.Fatalf("unmarshal values: %v", err)
	}

	if values["cloudProvider"] != "clusterapi" {
		t.Errorf("cloudProvider = %v, want clusterapi", values["cloudProvider"])
	}
	if values["clusterAPIMode"] != "kubeconfig-incluster" {
		t.Errorf("clusterAPIMode = %v, want kubeconfig-incluster", values["clusterAPIMode"])
	}
	if want := KubeconfigSecretName(wc); values["clusterAPIKubeconfigSecret"] != want {
		t.Errorf("clusterAPIKubeconfigSecret = %v, want %q", values["clusterAPIKubeconfigSecret"], want)
	}

	autoDiscovery, _ := values["autoDiscovery"].(map[string]any)
	if autoDiscovery["namespace"] != wc.Namespace {
		t.Errorf("autoDiscovery.namespace = %v, want %q", autoDiscovery["namespace"], wc.Namespace)
	}
	if autoDiscovery["clusterName"] != wc.Name {
		t.Errorf("autoDiscovery.clusterName = %v, want %q", autoDiscovery["clusterName"], wc.Name)
	}

	// Confirmed live: without this, the chart's own default ClusterRole
	// (provider-agnostic Cluster API kinds only) leaves the pod unable to
	// list infrastructure.cluster.x-k8s.io/LXCMachineTemplate at all,
	// logging a continuous watch error - needed for scale-from-zero.
	// json.Unmarshal into map[string]any decodes nested arrays/objects as
	// []any/map[string]any, not the concrete types the values were built
	// from - asserting straight to []map[string]any would always fail.
	rbac, _ := values["rbac"].(map[string]any)
	rules, _ := rbac["additionalRules"].([]any)
	var grantsLXCMachineTemplates bool
	for _, raw := range rules {
		rule, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		resources, _ := rule["resources"].([]any)
		for _, resource := range resources {
			if resource == "lxcmachinetemplates" {
				grantsLXCMachineTemplates = true
			}
		}
	}
	if !grantsLXCMachineTemplates {
		t.Errorf("rbac.additionalRules = %v, want a rule granting lxcmachinetemplates", rules)
	}
}

// TestClusterAutoscalerHelmReleaseHasNoKubeConfig guards the one thing that
// makes this addon different from every other one in this package: it must
// NOT carry spec.kubeConfig, since it needs to install locally (wherever
// helm-controller itself runs - the management cluster), not remotely onto
// the workload cluster.
func TestClusterAutoscalerHelmReleaseHasNoKubeConfig(t *testing.T) {
	wc := exampleAutoscalingCluster()
	values, err := ClusterAutoscalerHelmValues(wc)
	if err != nil {
		t.Fatalf("ClusterAutoscalerHelmValues: %v", err)
	}
	hr := ClusterAutoscalerHelmRelease(wc, values)

	if hr.Spec.KubeConfig != nil {
		t.Errorf("HelmRelease.spec.kubeConfig = %v, want nil (this installs locally)", hr.Spec.KubeConfig)
	}
	if hr.Name != ClusterAutoscalerHelmReleaseName(wc) {
		t.Errorf("name = %q, want %q", hr.Name, ClusterAutoscalerHelmReleaseName(wc))
	}
	if hr.Spec.TargetNamespace != ClusterAutoscalerNamespace(wc) {
		t.Errorf("targetNamespace = %q, want %q", hr.Spec.TargetNamespace, ClusterAutoscalerNamespace(wc))
	}
}

// TestClusterAutoscalerNamesAreUniquePerCluster guards against two
// WorkloadClusters' cluster-autoscaler HelmReleases colliding: every other
// addon in this package can use a fixed release name because it installs
// once per *workload* cluster, but cluster-autoscaler installs onto the one
// shared management cluster for every WorkloadCluster that wants it -
// sharing wc.Namespace too (see ClusterAutoscalerNamespace's own doc
// comment for why it must), so the release name is what has to stay unique.
func TestClusterAutoscalerReleaseNamesAreUniquePerCluster(t *testing.T) {
	a := &benchv1alpha1.WorkloadCluster{Name: "cluster-a", Namespace: "default"}
	b := &benchv1alpha1.WorkloadCluster{Name: "cluster-b", Namespace: "default"}

	if ClusterAutoscalerHelmReleaseName(a) == ClusterAutoscalerHelmReleaseName(b) {
		t.Error("two different WorkloadClusters rendered the same cluster-autoscaler HelmRelease name")
	}
}

// TestClusterAutoscalerNamespaceMatchesKubeconfigSecret guards the actual
// bug this caught live: clusterAPIKubeconfigSecret names a plain Secret
// volume mount, which can only ever be satisfied from the same namespace
// the pod itself runs in - a dedicated per-cluster namespace (this file's
// first version) left the pod unable to mount CAPI's own
// "<cluster>-kubeconfig" Secret at all, stuck in ContainerCreating
// (FailedMount) forever.
func TestClusterAutoscalerNamespaceMatchesKubeconfigSecret(t *testing.T) {
	wc := exampleAutoscalingCluster()
	if ClusterAutoscalerNamespace(wc) != wc.Namespace {
		t.Errorf("ClusterAutoscalerNamespace = %q, want wc.Namespace (%q) - the kubeconfig Secret lives there",
			ClusterAutoscalerNamespace(wc), wc.Namespace)
	}
}
