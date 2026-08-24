package render

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/utils/ptr"

	bootstrapv1 "sigs.k8s.io/cluster-api/api/bootstrap/kubeadm/v1beta2"
	controlplanev1 "sigs.k8s.io/cluster-api/api/controlplane/kubeadm/v1beta2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// The Incus/LXC infrastructure provider (github.com/lxc/cluster-api-provider-incus)
// is built as unstructured.Unstructured rather than importing its typed API:
// that module pins its own (older, conflicting) sigs.k8s.io/cluster-api
// version and drags in kind/cluster-api-test, none of which we need for two
// simple CRDs whose shape is small and stable. The core Cluster/
// MachineDeployment and in-tree kubeadm bootstrap/control-plane types come
// from sigs.k8s.io/cluster-api/api itself, which is lightweight and exactly
// the module internal/multicluster's ClusterCacheTracker needs anyway.
const (
	lxcInfraAPIVersion = "infrastructure.cluster.x-k8s.io/v1alpha2"
	lxcClusterKind     = "LXCCluster"
	lxcMachineTemplate = "LXCMachineTemplate"
)

// Name helpers, matching the reference cluster-api-provider-incus example
// (a Cluster named "x" pairs with a KubeadmControlPlane "x-control-plane" and
// a single worker MachineDeployment "x-md-0").
func ControlPlaneName(wc *benchv1alpha1.WorkloadCluster) string      { return wc.Name + "-control-plane" }
func MachineDeploymentName(wc *benchv1alpha1.WorkloadCluster) string { return wc.Name + "-md-0" }

func controlPlaneMachineTemplateName(wc *benchv1alpha1.WorkloadCluster) string {
	return ControlPlaneName(wc)
}
func workerMachineTemplateName(wc *benchv1alpha1.WorkloadCluster) string {
	return MachineDeploymentName(wc)
}
func workerKubeadmConfigTemplateName(wc *benchv1alpha1.WorkloadCluster) string {
	return MachineDeploymentName(wc)
}

// nodeRegistrationArgs are applied to both InitConfiguration and
// JoinConfiguration, on both control-plane and worker machines: LXC/Incus
// containers/VMs report host-level filesystem stats kubelet's default
// eviction thresholds reject, and provider-id is what lets CAPI match a
// Kubernetes Node back to the LXCMachine that created it.
func nodeRegistrationArgs() []bootstrapv1.Arg {
	return []bootstrapv1.Arg{
		{Name: "eviction-hard", Value: ptr.To("nodefs.available<0%,nodefs.inodesFree<0%,imagefs.available<0%")},
		{Name: "fail-swap-on", Value: ptr.To("false")},
		{Name: "provider-id", Value: ptr.To("lxc:///{{ v1.local_hostname }}")},
	}
}

// kubeProxyConfigFile disables per-core conntrack sizing, which fails inside
// an LXC container's restricted /proc. Written once and referenced by the
// preKubeadmCommands hack below, only when running inside a container
// (systemd-detect-virt -c) - a KubeadmControlPlane's virtual-machine
// instanceType doesn't need it, but the same kubeadmConfigSpec is shared by
// both, so the check has to be runtime, not build-time.
func kubeProxyConfigFile() bootstrapv1.File {
	return bootstrapv1.File{
		Path:        "/run/kubeadm/hack-kube-proxy-config-lxc.yaml",
		Owner:       "root:root",
		Permissions: "0444",
		Content: "---\n" +
			"kind: KubeProxyConfiguration\n" +
			"apiVersion: kubeproxy.config.k8s.io/v1alpha1\n" +
			"conntrack:\n" +
			"  maxPerCore: 0\n",
	}
}

func installKubeadmCommand(version string) string {
	return `sh /opt/cluster-api/install-kubeadm.sh "` + version + `"`
}

// LXCCluster is the Incus/LXC infrastructure for the Cluster: it stands up a
// load balancer in front of the control plane's machines.
func LXCCluster(wc *benchv1alpha1.WorkloadCluster) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion(lxcInfraAPIVersion)
	u.SetKind(lxcClusterKind)
	u.SetName(wc.Name)
	u.SetNamespace(wc.Namespace)
	_ = unstructured.SetNestedField(u.Object, wc.Spec.ControlPlane.Flavor, "spec", "loadBalancer", "lxc", "instanceSpec", "flavor")
	_ = unstructured.SetNestedStringSlice(u.Object, wc.Spec.Profiles, "spec", "loadBalancer", "lxc", "instanceSpec", "profiles")
	_ = unstructured.SetNestedField(u.Object, wc.Spec.LXCSecretRef.Name, "spec", "secretRef", "name")
	return u
}

// lxcMachineTemplateObject builds one LXCMachineTemplate - the same shape for
// both the control plane and the worker MachineDeployment, only the
// name/flavor differ.
func lxcMachineTemplateObject(wc *benchv1alpha1.WorkloadCluster, name string, flavor string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion(lxcInfraAPIVersion)
	u.SetKind(lxcMachineTemplate)
	u.SetName(name)
	u.SetNamespace(wc.Namespace)
	_ = unstructured.SetNestedSlice(u.Object, []any{}, "spec", "template", "spec", "devices")
	_ = unstructured.SetNestedField(u.Object, flavor, "spec", "template", "spec", "flavor")
	_ = unstructured.SetNestedField(u.Object, wc.Spec.Image, "spec", "template", "spec", "image", "name")
	_ = unstructured.SetNestedField(u.Object, "virtual-machine", "spec", "template", "spec", "instanceType")
	_ = unstructured.SetNestedStringSlice(u.Object, wc.Spec.Profiles, "spec", "template", "spec", "profiles")
	return u
}

// ControlPlaneLXCMachineTemplate and WorkerLXCMachineTemplate are the two
// LXCMachineTemplates a WorkloadCluster owns.
func ControlPlaneLXCMachineTemplate(wc *benchv1alpha1.WorkloadCluster) *unstructured.Unstructured {
	return lxcMachineTemplateObject(wc, controlPlaneMachineTemplateName(wc), wc.Spec.ControlPlane.Flavor)
}

func WorkerLXCMachineTemplate(wc *benchv1alpha1.WorkloadCluster) *unstructured.Unstructured {
	return lxcMachineTemplateObject(wc, workerMachineTemplateName(wc), wc.Spec.Workers.Flavor)
}

// KubeadmControlPlane renders the control plane: kubeadmConfigSpec is shared
// verbatim between InitConfiguration and JoinConfiguration (kubeadm applies
// Init to the first control-plane machine and Join to the rest), plus the
// LXC-specific kube-proxy workaround and kubeadm install script.
func KubeadmControlPlane(wc *benchv1alpha1.WorkloadCluster) *controlplanev1.KubeadmControlPlane {
	replicas := wc.Spec.ControlPlane.Replicas
	nodeReg := bootstrapv1.NodeRegistrationOptions{KubeletExtraArgs: nodeRegistrationArgs()}
	return &controlplanev1.KubeadmControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ControlPlaneName(wc),
			Namespace: wc.Namespace,
		},
		Spec: controlplanev1.KubeadmControlPlaneSpec{
			Replicas: &replicas,
			Version:  wc.Spec.KubernetesVersion,
			MachineTemplate: controlplanev1.KubeadmControlPlaneMachineTemplate{
				Spec: controlplanev1.KubeadmControlPlaneMachineTemplateSpec{
					InfrastructureRef: clusterv1.ContractVersionedObjectReference{
						APIGroup: "infrastructure.cluster.x-k8s.io",
						Kind:     lxcMachineTemplate,
						Name:     controlPlaneMachineTemplateName(wc),
					},
				},
			},
			KubeadmConfigSpec: bootstrapv1.KubeadmConfigSpec{
				Files:             []bootstrapv1.File{kubeProxyConfigFile()},
				InitConfiguration: bootstrapv1.InitConfiguration{NodeRegistration: nodeReg},
				JoinConfiguration: bootstrapv1.JoinConfiguration{NodeRegistration: nodeReg},
				PreKubeadmCommands: []string{
					"set -x",
					"if systemd-detect-virt -c -q 2>/dev/null; then\n" +
						"  cat /run/kubeadm/hack-kube-proxy-config-lxc.yaml | tee -a /run/kubeadm/kubeadm.yaml\n" +
						"fi",
					installKubeadmCommand(wc.Spec.KubernetesVersion),
				},
			},
		},
	}
}

// machineDeploymentSelectorLabels are applied to both the MachineDeployment's
// spec.selector and its machine template's metadata - they must match each
// other, and spec.selector is a required field with no useful zero value.
func machineDeploymentSelectorLabels(wc *benchv1alpha1.WorkloadCluster) map[string]string {
	return map[string]string{
		"cluster.x-k8s.io/cluster-name":    wc.Name,
		"cluster.x-k8s.io/deployment-name": MachineDeploymentName(wc),
	}
}

// MachineDeployment renders the single worker pool.
func MachineDeployment(wc *benchv1alpha1.WorkloadCluster) *clusterv1.MachineDeployment {
	replicas := wc.Spec.Workers.Replicas
	labels := machineDeploymentSelectorLabels(wc)
	return &clusterv1.MachineDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      MachineDeploymentName(wc),
			Namespace: wc.Namespace,
		},
		Spec: clusterv1.MachineDeploymentSpec{
			ClusterName: wc.Name,
			Replicas:    &replicas,
			Selector:    metav1.LabelSelector{MatchLabels: labels},
			Template: clusterv1.MachineTemplateSpec{
				ObjectMeta: clusterv1.ObjectMeta{Labels: labels},
				Spec: clusterv1.MachineSpec{
					ClusterName: wc.Name,
					Version:     wc.Spec.KubernetesVersion,
					Bootstrap: clusterv1.Bootstrap{
						ConfigRef: clusterv1.ContractVersionedObjectReference{
							APIGroup: "bootstrap.cluster.x-k8s.io",
							Kind:     "KubeadmConfigTemplate",
							Name:     workerKubeadmConfigTemplateName(wc),
						},
					},
					InfrastructureRef: clusterv1.ContractVersionedObjectReference{
						APIGroup: "infrastructure.cluster.x-k8s.io",
						Kind:     lxcMachineTemplate,
						Name:     workerMachineTemplateName(wc),
					},
				},
			},
		},
	}
}

// WorkerKubeadmConfigTemplate renders the join-only bootstrap config workers
// use (no InitConfiguration - only the first control-plane machine inits the
// cluster, everything else joins it).
func WorkerKubeadmConfigTemplate(wc *benchv1alpha1.WorkloadCluster) *bootstrapv1.KubeadmConfigTemplate {
	nodeReg := bootstrapv1.NodeRegistrationOptions{KubeletExtraArgs: nodeRegistrationArgs()}
	return &bootstrapv1.KubeadmConfigTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workerKubeadmConfigTemplateName(wc),
			Namespace: wc.Namespace,
		},
		Spec: bootstrapv1.KubeadmConfigTemplateSpec{
			Template: bootstrapv1.KubeadmConfigTemplateResource{
				Spec: bootstrapv1.KubeadmConfigSpec{
					JoinConfiguration: bootstrapv1.JoinConfiguration{NodeRegistration: nodeReg},
					PreKubeadmCommands: []string{
						"set -x",
						installKubeadmCommand(wc.Spec.KubernetesVersion),
					},
				},
			},
		},
	}
}

// Cluster ties the LXCCluster and KubeadmControlPlane together as the object
// CAPI's core controller actually reconciles.
func Cluster(wc *benchv1alpha1.WorkloadCluster) *clusterv1.Cluster {
	return &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      wc.Name,
			Namespace: wc.Namespace,
		},
		Spec: clusterv1.ClusterSpec{
			ClusterNetwork: clusterv1.ClusterNetwork{
				ServiceDomain: "cluster.local",
				Services:      clusterv1.NetworkRanges{CIDRBlocks: []string{"10.96.0.0/12"}},
				Pods:          clusterv1.NetworkRanges{CIDRBlocks: []string{"10.244.0.0/16"}},
			},
			ControlPlaneRef: clusterv1.ContractVersionedObjectReference{
				APIGroup: "controlplane.cluster.x-k8s.io",
				Kind:     "KubeadmControlPlane",
				Name:     ControlPlaneName(wc),
			},
			InfrastructureRef: clusterv1.ContractVersionedObjectReference{
				APIGroup: "infrastructure.cluster.x-k8s.io",
				Kind:     lxcClusterKind,
				Name:     wc.Name,
			},
		},
	}
}

// KubeconfigSecretName is the name CAPI writes a workload cluster's admin
// kubeconfig under, by convention (see sigs.k8s.io/cluster-api/util/secret).
func KubeconfigSecretName(wc *benchv1alpha1.WorkloadCluster) string {
	return wc.Name + "-kubeconfig"
}
