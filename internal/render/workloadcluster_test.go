package render

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

func TestKubeadmControlPlaneBindsDualStack(t *testing.T) {
	wc := &benchv1alpha1.WorkloadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "wc-a", Namespace: "default"},
		Spec: benchv1alpha1.WorkloadClusterSpec{
			KubernetesVersion: "v1.31.0",
			Provider:          benchv1alpha1.InfrastructureIncus,
			ControlPlane:      benchv1alpha1.MachineSpec{Replicas: 1},
			WorkerPools:       []benchv1alpha1.WorkerPoolSpec{{Name: "workers", Replicas: 2}},
		},
	}

	kcp := KubeadmControlPlane(wc)

	var bindAddress *string
	for _, arg := range kcp.Spec.KubeadmConfigSpec.ClusterConfiguration.APIServer.ExtraArgs {
		if arg.Name == "bind-address" {
			bindAddress = arg.Value
		}
	}
	if bindAddress == nil || *bindAddress != "::" {
		t.Fatalf("apiServer.extraArgs bind-address = %v, want \"::\" - kube-apiserver must listen on "+
			"both address families since Incus's load balancer address family isn't under our control",
			bindAddress)
	}
}

func TestLXCClusterOverridesHAProxyForDualStack(t *testing.T) {
	wc := &benchv1alpha1.WorkloadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "wc-a", Namespace: "default"},
		Spec: benchv1alpha1.WorkloadClusterSpec{
			KubernetesVersion: "v1.31.0",
			Provider:          benchv1alpha1.InfrastructureIncus,
			ControlPlane:      benchv1alpha1.MachineSpec{Replicas: 1},
			WorkerPools:       []benchv1alpha1.WorkerPoolSpec{{Name: "workers", Replicas: 2}},
			Incus: &benchv1alpha1.IncusProviderSpec{
				LXCSecretRef: corev1.LocalObjectReference{Name: "lxc-secret"},
			},
		},
	}

	lxcCluster := LXCCluster(wc)

	tmpl, found, err := unstructured.NestedString(lxcCluster.Object, "spec", "loadBalancer", "lxc", "customHAProxyConfigTemplate")
	if err != nil || !found {
		t.Fatalf("customHAProxyConfigTemplate not set: found=%v err=%v", found, err)
	}
	// cluster-api-provider-incus's own default template only adds this line
	// when a configData.IPv6 flag is set, which its own code never sets - see
	// dualStackHAProxyConfigTemplate's doc comment. Our override must not
	// depend on that flag at all.
	if !strings.Contains(tmpl, "bind :::{{ .FrontendControlPlanePort }}") {
		t.Fatalf("customHAProxyConfigTemplate missing the unconditional IPv6 bind line:\n%s", tmpl)
	}
	if !strings.Contains(tmpl, "bind *:{{ .FrontendControlPlanePort }}") {
		t.Fatalf("customHAProxyConfigTemplate missing the IPv4 bind line:\n%s", tmpl)
	}
	// Must stay compatible with the exact fields/funcs
	// internal/loadbalancer.renderHaproxyConfiguration feeds into the
	// template today.
	for _, want := range []string{"{{ if .EnableHealthzCheck", "{{range $server, $backend := .BackendServers}}", "JoinHostPort"} {
		if !strings.Contains(tmpl, want) {
			t.Fatalf("customHAProxyConfigTemplate missing expected upstream template fragment %q:\n%s", want, tmpl)
		}
	}
}

func wcWithPools(pools ...benchv1alpha1.WorkerPoolSpec) *benchv1alpha1.WorkloadCluster {
	return &benchv1alpha1.WorkloadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "wc-a", Namespace: "default"},
		Spec: benchv1alpha1.WorkloadClusterSpec{
			KubernetesVersion: "v1.31.0",
			Provider:          benchv1alpha1.InfrastructureIncus,
			ControlPlane:      benchv1alpha1.MachineSpec{Replicas: 1},
			WorkerPools:       pools,
			Incus: &benchv1alpha1.IncusProviderSpec{
				LXCSecretRef: corev1.LocalObjectReference{Name: "lxc-secret"},
				Image:        "ubuntu:24.04",
			},
		},
	}
}

// TestMultiplePoolsGetDistinctNames guards the "add another pool of workers
// with a different instance type" feature: two pools with different names
// must render distinct MachineDeployments/LXCMachineTemplates, not collide
// or overwrite each other.
func TestMultiplePoolsGetDistinctNames(t *testing.T) {
	wc := wcWithPools(
		benchv1alpha1.WorkerPoolSpec{Name: "small", Replicas: 1, Incus: benchv1alpha1.IncusMachineSpec{Flavor: "c1-m2"}},
		benchv1alpha1.WorkerPoolSpec{Name: "large", Replicas: 3, Incus: benchv1alpha1.IncusMachineSpec{Flavor: "c8-m16"}},
	)

	small := MachineDeployment(wc, wc.Spec.WorkerPools[0])
	large := MachineDeployment(wc, wc.Spec.WorkerPools[1])

	if small.Name == large.Name {
		t.Fatalf("both pools rendered the same MachineDeployment name %q", small.Name)
	}
	if *small.Spec.Replicas != 1 || *large.Spec.Replicas != 3 {
		t.Fatalf("replicas = %d/%d, want 1/3", *small.Spec.Replicas, *large.Spec.Replicas)
	}
	if small.Spec.Template.Spec.InfrastructureRef.Name == large.Spec.Template.Spec.InfrastructureRef.Name {
		t.Fatalf("both pools' MachineDeployments reference the same LXCMachineTemplate %q - a flavor edit to one pool would roll the other too",
			small.Spec.Template.Spec.InfrastructureRef.Name)
	}
}

// TestWorkerMachineTemplateNameChangesWithFlavor guards the day-2 "change a
// pool's instance type" path: WorkloadClusterReconciler relies on the
// template's *name* changing to get Cluster API to roll the pool's machines
// (see workerMachineTemplateName's doc comment) - editing the template
// object's content in place would silently do nothing.
func TestWorkerMachineTemplateNameChangesWithFlavor(t *testing.T) {
	small := benchv1alpha1.WorkerPoolSpec{Name: "workers", Replicas: 2, Incus: benchv1alpha1.IncusMachineSpec{Flavor: "c1-m2"}}
	large := benchv1alpha1.WorkerPoolSpec{Name: "workers", Replicas: 2, Incus: benchv1alpha1.IncusMachineSpec{Flavor: "c8-m16"}}

	nameForSmall := workerMachineTemplateName(wcWithPools(small), small)
	nameForLarge := workerMachineTemplateName(wcWithPools(large), large)

	if nameForSmall == nameForLarge {
		t.Fatalf("workerMachineTemplateName did not change across a flavor edit: both = %q", nameForSmall)
	}

	// A no-op reconcile (same flavor) must render the exact same name, or
	// applyCreateOnly would create a redundant new template every time.
	again := workerMachineTemplateName(wcWithPools(small), small)
	if again != nameForSmall {
		t.Fatalf("workerMachineTemplateName is not stable across identical input: %q vs %q", nameForSmall, again)
	}
}

// TestMachineDeploymentAutoscalingAnnotations guards the two annotations
// cluster-autoscaler itself watches for (cloudprovider/clusterapi's own
// README, verified directly - see ClusterAutoscalerHelmRelease's doc
// comment) - without them on the rendered object, cluster-autoscaler would
// never recognize this pool as a node group to manage at all, regardless of
// how the rest of its Helm install is configured.
func TestMachineDeploymentAutoscalingAnnotations(t *testing.T) {
	pool := benchv1alpha1.WorkerPoolSpec{
		Name:        "workers",
		Replicas:    2,
		Incus:       benchv1alpha1.IncusMachineSpec{Flavor: "c1-m2"},
		Autoscaling: &benchv1alpha1.AutoscalingSpec{MinReplicas: 1, MaxReplicas: 5},
	}
	md := MachineDeployment(wcWithPools(pool), pool)

	if got := md.Annotations[ClusterAutoscalerMinSizeAnnotation]; got != "1" {
		t.Errorf("min-size annotation = %q, want %q", got, "1")
	}
	if got := md.Annotations[ClusterAutoscalerMaxSizeAnnotation]; got != "5" {
		t.Errorf("max-size annotation = %q, want %q", got, "5")
	}

	// A pool with no autoscaling set must render neither annotation - a
	// present-but-empty annotation would still make cluster-autoscaler
	// treat it as a managed node group.
	fixed := benchv1alpha1.WorkerPoolSpec{Name: "fixed", Replicas: 2, Incus: benchv1alpha1.IncusMachineSpec{Flavor: "c1-m2"}}
	fixedMD := MachineDeployment(wcWithPools(fixed), fixed)
	if _, ok := fixedMD.Annotations[ClusterAutoscalerMinSizeAnnotation]; ok {
		t.Errorf("non-autoscaled pool got the min-size annotation anyway: %v", fixedMD.Annotations)
	}
}
