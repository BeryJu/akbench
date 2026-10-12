package render

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

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

	// infrastructureAPIGroup is every InfrastructureRef's APIGroup below,
	// regardless of which provider-specific Kind it names.
	infrastructureAPIGroup = "infrastructure.cluster.x-k8s.io"
)

// infrastructureClusterKind and infrastructureMachineTemplateKind are the
// only two things that differ, at the Cluster/KubeadmControlPlane/
// MachineDeployment level, between infrastructure providers: which Kind
// their InfrastructureRef points at. Adding a second provider (AWS, vSphere,
// ...) means adding a case here and a set of provider-specific object
// builders alongside LXCCluster/lxcMachineTemplateObject below - Cluster,
// KubeadmControlPlane, and MachineDeployment themselves don't change.
//
//nolint:unparam // always LXCCluster today - Incus is the only implemented provider; the switch is the extension point a second provider adds a case to.
func infrastructureClusterKind(wc *benchv1alpha1.WorkloadCluster) string {
	switch wc.Spec.Provider {
	case benchv1alpha1.InfrastructureIncus, "":
		return lxcClusterKind
	default:
		return lxcClusterKind
	}
}

//nolint:unparam // always LXCMachineTemplate today - Incus is the only implemented provider; the switch is the extension point a second provider adds a case to.
func infrastructureMachineTemplateKind(wc *benchv1alpha1.WorkloadCluster) string {
	switch wc.Spec.Provider {
	case benchv1alpha1.InfrastructureIncus, "":
		return lxcMachineTemplate
	default:
		return lxcMachineTemplate
	}
}

// Name helpers, matching the reference cluster-api-provider-incus example
// (a Cluster named "x" pairs with a KubeadmControlPlane "x-control-plane" and
// a worker MachineDeployment per pool, "x-<pool>").
func ControlPlaneName(wc *benchv1alpha1.WorkloadCluster) string { return wc.Name + "-control-plane" }

func MachineDeploymentName(wc *benchv1alpha1.WorkloadCluster, pool benchv1alpha1.WorkerPoolSpec) string {
	return wc.Name + "-" + pool.Name
}

func controlPlaneMachineTemplateName(wc *benchv1alpha1.WorkloadCluster) string {
	return ControlPlaneName(wc)
}

// workerMachineTemplateName includes a short hash of the pool's flavor/image:
// LXCMachineTemplates are meant to be immutable once a MachineDeployment
// references them (editing one in place doesn't trigger Cluster API's
// rolling-replace machinery, only pointing the MachineDeployment at a
// differently-*named* template does), so a flavor edit has to produce a new
// name here rather than update the existing template's content.
func workerMachineTemplateName(wc *benchv1alpha1.WorkloadCluster, pool benchv1alpha1.WorkerPoolSpec) string {
	h := fnv.New32a()
	image := ""
	if wc.Spec.Incus != nil {
		image = wc.Spec.Incus.Image
	}
	_, _ = h.Write([]byte(pool.Incus.Flavor + "|" + image))
	return fmt.Sprintf("%s-%x", MachineDeploymentName(wc, pool), h.Sum32())
}

func workerKubeadmConfigTemplateName(wc *benchv1alpha1.WorkloadCluster, pool benchv1alpha1.WorkerPoolSpec) string {
	return MachineDeploymentName(wc, pool)
}

// nodeRegistrationArgs are applied to both InitConfiguration and
// JoinConfiguration, on both control-plane and worker machines: LXC/Incus
// containers/VMs report host-level filesystem stats kubelet's default
// eviction thresholds reject, and provider-id is what lets CAPI match a
// Kubernetes Node back to the LXCMachine that created it.
func nodeRegistrationArgs(extra ...bootstrapv1.Arg) []bootstrapv1.Arg {
	args := make([]bootstrapv1.Arg, 0, 3+len(extra))
	args = append(args,
		bootstrapv1.Arg{Name: "eviction-hard", Value: new("nodefs.available<0%,nodefs.inodesFree<0%,imagefs.available<0%")},
		bootstrapv1.Arg{Name: "fail-swap-on", Value: new("false")},
		bootstrapv1.Arg{Name: "provider-id", Value: new("lxc:///{{ v1.local_hostname }}")},
	)
	return append(args, extra...)
}

// workerNodeRegistrationArgs is nodeRegistrationArgs plus, for a pool with a
// non-Generic role, a --node-labels registering this node with
// render.NodePoolLabel - what a pool-targeted workload's nodeSelector
// actually matches against (its own toleration, set via poolTaints below on
// the same NodeRegistrationOptions, only ever *permits* scheduling onto the
// node, it doesn't request one). Custom-domain labels like this one are
// allowed via kubelet's own --node-labels flag regardless of
// NodeRestriction admission - that only blocks a kubelet self-labeling under
// the reserved kubernetes.io/k8s.io namespaces.
func workerNodeRegistrationArgs(pool benchv1alpha1.WorkerPoolSpec) []bootstrapv1.Arg {
	value := poolLabelValue(pool.Role)
	if value == "" {
		return nodeRegistrationArgs()
	}
	return nodeRegistrationArgs(bootstrapv1.Arg{Name: "node-labels", Value: new(NodePoolLabel + "=" + value)})
}

// poolTaints is the worker JoinConfiguration's NodeRegistration.Taints for
// pool - always a non-nil pointer, never left nil: kubeadm's own doc comment
// on this field treats a nil Taints specially (it would otherwise apply
// kubeadm's own init-time defaulting, meant for a control-plane node, not a
// worker join), so every pool gets an explicit value - an empty slice for
// Generic (no taint at all), or exactly the one taint matching this pool's
// role.
func poolTaints(pool benchv1alpha1.WorkerPoolSpec) *[]corev1.Taint {
	taint := poolTaint(pool.Role)
	if taint == nil {
		return &[]corev1.Taint{}
	}
	return &[]corev1.Taint{*taint}
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

// dualStackHAProxyConfigTemplate overrides cluster-api-provider-incus's own
// haproxy config template (internal/loadbalancer/config.go's
// DefaultHaproxyTemplate) to unconditionally bind the control-plane frontend
// on both address families. Upstream's template only adds the `bind :::` line
// when configData.IPv6 is true, but nothing in that provider ever sets that
// field (internal/loadbalancer/util.go's getLoadBalancerConfiguration has a
// standing "TODO: care about ipv4 vs ipv6 addresses" and always leaves it
// false) - so the frontend is always IPv4-only in practice, while the address
// it reports as the Cluster's controlPlaneEndpoint can be either family
// depending on which of an instance's addresses Incus happens to list first.
// Confirmed live: a workload cluster whose controlPlaneEndpoint came back
// IPv6 was unreachable through the load balancer ("connection refused")
// despite the control-plane node itself being healthy. This is otherwise
// byte-for-byte upstream's own template - only the frontend's bind lines
// differ - so it stays compatible with every other field
// getLoadBalancerConfiguration feeds in (FrontendControlPlanePort,
// EnableHealthzCheck, BackendServers, the JoinHostPort template func).
const dualStackHAProxyConfigTemplate = `# generated by authentik-bench (dual-stack frontend bind, see internal/render/workloadcluster.go)
global
  log /dev/log local0
  log /dev/log local1 notice
  daemon
  maxconn 100000

defaults
  log global
  mode tcp
  option dontlognull
  timeout connect 5000
  timeout client 50000
  timeout server 50000
  default-server init-addr none

frontend stats
  mode http
  bind *:8404
  stats enable
  stats uri /stats
  stats refresh 1s
  stats admin if TRUE

frontend control-plane
  bind *:{{ .FrontendControlPlanePort }}
  bind :::{{ .FrontendControlPlanePort }}
  default_backend kube-apiservers

backend kube-apiservers
  {{ if .EnableHealthzCheck -}}
  option httpchk GET /healthz
  {{- end }}
  {{range $server, $backend := .BackendServers}}
  server {{ $server }} {{ JoinHostPort $backend.Address $.BackendControlPlanePort }} weight {{ $backend.Weight }} check check-ssl verify none
  {{- end}}
`

// LXCCluster is the Incus/LXC infrastructure for the Cluster: it stands up a
// load balancer in front of the control plane's machines. Callers must only
// call this (and the other Incus-specific builders below) once the
// controller has confirmed wc.Spec.Provider is Incus and wc.Spec.Incus is
// non-nil.
func LXCCluster(wc *benchv1alpha1.WorkloadCluster) *unstructured.Unstructured {
	incus := wc.Spec.Incus
	u := &unstructured.Unstructured{}
	u.SetAPIVersion(lxcInfraAPIVersion)
	u.SetKind(lxcClusterKind)
	u.SetName(wc.Name)
	u.SetNamespace(wc.Namespace)
	_ = unstructured.SetNestedField(u.Object, incus.ControlPlane.Flavor, "spec", "loadBalancer", "lxc", "instanceSpec", "flavor")
	_ = unstructured.SetNestedStringSlice(u.Object, incus.Profiles, "spec", "loadBalancer", "lxc", "instanceSpec", "profiles")
	_ = unstructured.SetNestedField(u.Object, incus.LXCSecretRef.Name, "spec", "secretRef", "name")
	_ = unstructured.SetNestedField(u.Object, dualStackHAProxyConfigTemplate, "spec", "loadBalancer", "lxc", "customHAProxyConfigTemplate")
	return u
}

// AddonHelmReleaseName is the HelmRelease object name for one of wc's
// add-ons (base being the add-on's fixed Helm release name, e.g.
// CNIHelmReleaseName). Every WorkloadCluster's add-on HelmReleases live in
// its own namespace on the management cluster, so a fixed object name would
// have two clusters in one namespace share - and keep overwriting - one
// HelmRelease. Only the object is per-cluster: spec.releaseName stays base,
// so the Helm release on the workload cluster itself keeps its name.
// Truncated with a hash suffix to stay within the 63-character limit.
func AddonHelmReleaseName(wc *benchv1alpha1.WorkloadCluster, base string) string {
	name := base + "-" + wc.Name
	if len(name) <= 63 {
		return name
	}
	sum := sha256.Sum256([]byte(wc.Name))
	return strings.TrimRight(name[:54], "-") + "-" + hex.EncodeToString(sum[:])[:8]
}

// LoadBalancerInstanceName is the Incus instance name cluster-api-provider-
// incus gives the LXCCluster's load balancer container - mirrored from its
// LXCCluster.GetLoadBalancerInstanceName (api/v1alpha2/lxccluster_types.go),
// which derives it from the LXCCluster's own name and namespace rather than
// recording it anywhere this operator could read it back.
func LoadBalancerInstanceName(wc *benchv1alpha1.WorkloadCluster) string {
	hash := sha256.Sum256([]byte(wc.Namespace))
	return fmt.Sprintf("%s-%s-lb", wc.Name, hex.EncodeToString(hash[:3])[:5])
}

// LXCClusterGVK identifies the LXCCluster kind for unstructured reads.
func LXCClusterGVK() schema.GroupVersionKind {
	return schema.FromAPIVersionAndKind(lxcInfraAPIVersion, lxcClusterKind)
}

// lxcMachineTemplateObject builds one LXCMachineTemplate - the same shape for
// both the control plane and the worker MachineDeployment, only the
// name/flavor differ.
func lxcMachineTemplateObject(wc *benchv1alpha1.WorkloadCluster, name string, flavor string) *unstructured.Unstructured {
	incus := wc.Spec.Incus
	u := &unstructured.Unstructured{}
	u.SetAPIVersion(lxcInfraAPIVersion)
	u.SetKind(lxcMachineTemplate)
	u.SetName(name)
	u.SetNamespace(wc.Namespace)
	_ = unstructured.SetNestedSlice(u.Object, []any{}, "spec", "template", "spec", "devices")
	_ = unstructured.SetNestedField(u.Object, flavor, "spec", "template", "spec", "flavor")
	_ = unstructured.SetNestedField(u.Object, incus.Image, "spec", "template", "spec", "image", "name")
	_ = unstructured.SetNestedField(u.Object, "virtual-machine", "spec", "template", "spec", "instanceType")
	_ = unstructured.SetNestedStringSlice(u.Object, incus.Profiles, "spec", "template", "spec", "profiles")
	return u
}

// ControlPlaneLXCMachineTemplate and WorkerLXCMachineTemplate are the two
// LXCMachineTemplates a WorkloadCluster owns.
func ControlPlaneLXCMachineTemplate(wc *benchv1alpha1.WorkloadCluster) *unstructured.Unstructured {
	return lxcMachineTemplateObject(wc, controlPlaneMachineTemplateName(wc), wc.Spec.Incus.ControlPlane.Flavor)
}

func WorkerLXCMachineTemplate(wc *benchv1alpha1.WorkloadCluster, pool benchv1alpha1.WorkerPoolSpec) *unstructured.Unstructured {
	return lxcMachineTemplateObject(wc, workerMachineTemplateName(wc, pool), pool.Incus.Flavor)
}

// KubeadmControlPlane renders the control plane: kubeadmConfigSpec is shared
// verbatim between InitConfiguration and JoinConfiguration (kubeadm applies
// Init to the first control-plane machine and Join to the rest), plus the
// LXC-specific kube-proxy workaround and kubeadm install script.
func KubeadmControlPlane(wc *benchv1alpha1.WorkloadCluster) *controlplanev1.KubeadmControlPlane {
	replicas := wc.Spec.ControlPlane.Replicas
	nodeReg := bootstrapv1.NodeRegistrationOptions{KubeletExtraArgs: nodeRegistrationArgs()}
	return &controlplanev1.KubeadmControlPlane{
		Name:      ControlPlaneName(wc),
		Namespace: wc.Namespace,
		Spec: controlplanev1.KubeadmControlPlaneSpec{
			Replicas: &replicas,
			Version:  wc.Spec.KubernetesVersion,
			MachineTemplate: controlplanev1.KubeadmControlPlaneMachineTemplate{
				Spec: controlplanev1.KubeadmControlPlaneMachineTemplateSpec{
					InfrastructureRef: clusterv1.ContractVersionedObjectReference{
						APIGroup: infrastructureAPIGroup,
						Kind:     infrastructureMachineTemplateKind(wc),
						Name:     controlPlaneMachineTemplateName(wc),
					},
				},
			},
			KubeadmConfigSpec: bootstrapv1.KubeadmConfigSpec{
				Files: []bootstrapv1.File{kubeProxyConfigFile()},
				// kube-apiserver's --bind-address defaults to 0.0.0.0
				// (IPv4-only) even though kubeadm auto-detects an
				// --advertise-address that may not match the address family
				// Cluster.spec.controlPlaneEndpoint ends up with - the LXC
				// load balancer's address, whose family Incus assigns and
				// this operator doesn't choose. "::" listens on both
				// families (standard Linux dual-stack socket behavior), so
				// the apiserver is reachable through the load balancer
				// regardless of which family it picked.
				ClusterConfiguration: bootstrapv1.ClusterConfiguration{
					APIServer: bootstrapv1.APIServer{
						ExtraArgs: []bootstrapv1.Arg{
							{Name: "bind-address", Value: new("::")},
						},
					},
				},
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
// Also applied to the MachineDeployment's own metadata.labels, so the
// controller can list a WorkloadCluster's pools back out by cluster-name to
// find any whose pool no longer exists in spec.workerPools.
func machineDeploymentSelectorLabels(wc *benchv1alpha1.WorkloadCluster, pool benchv1alpha1.WorkerPoolSpec) map[string]string {
	return map[string]string{
		"cluster.x-k8s.io/cluster-name":    wc.Name,
		"cluster.x-k8s.io/deployment-name": MachineDeploymentName(wc, pool),
	}
}

// clusterAutoscalerMinSizeAnnotation/clusterAutoscalerMaxSizeAnnotation are
// cluster-autoscaler's own well-known annotations
// (cloudprovider/clusterapi's README, verified directly rather than
// assumed): applying both to a MachineDeployment is what tells
// cluster-autoscaler to manage it as a node group. Exported so
// applyMachineDeploymentPool (internal/controller/apply.go) can detect
// autoscaling is enabled for a pool without re-deriving it from
// WorkerPoolSpec itself.
const (
	ClusterAutoscalerMinSizeAnnotation = "cluster.x-k8s.io/cluster-api-autoscaler-node-group-min-size"
	ClusterAutoscalerMaxSizeAnnotation = "cluster.x-k8s.io/cluster-api-autoscaler-node-group-max-size"
)

// MachineDeployment renders one worker pool. When pool.Autoscaling is set,
// the rendered object carries cluster-autoscaler's own min/max-size
// annotations - replicas is still set (it's this pool's initial size at
// creation), but applyMachineDeploymentPool stops reconciling it against
// the live object's .spec.replicas from then on, since cluster-autoscaler
// owns that field once these annotations are present.
func MachineDeployment(wc *benchv1alpha1.WorkloadCluster, pool benchv1alpha1.WorkerPoolSpec) *clusterv1.MachineDeployment {
	replicas := pool.Replicas
	labels := machineDeploymentSelectorLabels(wc, pool)
	var annotations map[string]string
	if pool.Autoscaling != nil {
		annotations = map[string]string{
			ClusterAutoscalerMinSizeAnnotation: fmt.Sprintf("%d", pool.Autoscaling.MinReplicas),
			ClusterAutoscalerMaxSizeAnnotation: fmt.Sprintf("%d", pool.Autoscaling.MaxReplicas),
		}
	}
	return &clusterv1.MachineDeployment{
		Name:        MachineDeploymentName(wc, pool),
		Namespace:   wc.Namespace,
		Labels:      labels,
		Annotations: annotations,
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
							Name:     workerKubeadmConfigTemplateName(wc, pool),
						},
					},
					InfrastructureRef: clusterv1.ContractVersionedObjectReference{
						APIGroup: infrastructureAPIGroup,
						Kind:     infrastructureMachineTemplateKind(wc),
						Name:     workerMachineTemplateName(wc, pool),
					},
				},
			},
		},
	}
}

// WorkerKubeadmConfigTemplate renders the join-only bootstrap config workers
// use (no InitConfiguration - only the first control-plane machine inits the
// cluster, everything else joins it).
func WorkerKubeadmConfigTemplate(wc *benchv1alpha1.WorkloadCluster, pool benchv1alpha1.WorkerPoolSpec) *bootstrapv1.KubeadmConfigTemplate {
	nodeReg := bootstrapv1.NodeRegistrationOptions{
		KubeletExtraArgs: workerNodeRegistrationArgs(pool),
		Taints:           poolTaints(pool),
	}
	return &bootstrapv1.KubeadmConfigTemplate{
		Name:      workerKubeadmConfigTemplateName(wc, pool),
		Namespace: wc.Namespace,
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
		Name:      wc.Name,
		Namespace: wc.Namespace,
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
				APIGroup: infrastructureAPIGroup,
				Kind:     infrastructureClusterKind(wc),
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
