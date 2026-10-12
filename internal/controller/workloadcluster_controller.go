package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	capikubeconfig "sigs.k8s.io/cluster-api/util/kubeconfig"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
	"github.com/goauthentik/authentik-bench/internal/render"
)

// capiDeleteRequeueInterval is how often reconcileDelete polls while waiting
// for CAPI to finish tearing down a Cluster's machines.
const capiDeleteRequeueInterval = 5 * time.Second

// workloadClusterFinalizer guards deletion so the operator can wait for
// CAPI's own Cluster deletion (which tears down machines one at a time) to
// finish before removing the WorkloadCluster - it does not need to reach any
// object on a different cluster, since everything a WorkloadCluster renders
// (the CAPI Cluster and its infrastructure/bootstrap objects) lives on the
// management cluster.
const workloadClusterFinalizer = "bench.goauthentik.io/workload-cluster-cleanup"

// WorkloadClusterReconciler reconciles a WorkloadCluster object: it renders
// the CAPI Cluster/LXCCluster/KubeadmControlPlane/MachineDeployment set
// described in internal/render/workloadcluster.go via the Incus
// infrastructure provider already installed on the management cluster, and
// mirrors the resulting CAPI Cluster's readiness back onto its own status.
type WorkloadClusterReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=bench.goauthentik.io,resources=workloadclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bench.goauthentik.io,resources=workloadclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=bench.goauthentik.io,resources=workloadclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;machinedeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=kubeadmcontrolplanes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=kubeadmconfigtemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=lxcclusters;lxcmachinetemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch

func (r *WorkloadClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var wc benchv1alpha1.WorkloadCluster
	if err := r.Get(ctx, req.NamespacedName, &wc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !wc.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &wc)
	}
	if !controllerutil.ContainsFinalizer(&wc, workloadClusterFinalizer) {
		controllerutil.AddFinalizer(&wc, workloadClusterFinalizer)
		if err := r.Update(ctx, &wc); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Incus is the only infrastructure provider actually implemented today -
	// spec.provider/spec.incus exist as a discriminated union (like
	// PostgresSpec's mode) so a second provider is a new case here plus new
	// render functions, not a breaking schema change, but there's nothing to
	// render for it yet.
	if (wc.Spec.Provider != benchv1alpha1.InfrastructureIncus && wc.Spec.Provider != "") || wc.Spec.Incus == nil {
		setCondition(r.Recorder, &wc, &wc.Status.Conditions, wc.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
			benchv1alpha1.ReasonNotImplemented,
			fmt.Sprintf("provider %q is not implemented; only %q (with spec.incus set) is", wc.Spec.Provider, benchv1alpha1.InfrastructureIncus))
		return ctrl.Result{}, r.Status().Update(ctx, &wc)
	}

	// applyCreateOnly, not applyOwned: CAPI's own webhooks/defaulting mutate
	// these objects' specs after creation (rollout strategy defaults, etc.)
	// in ways this operator's freshly-rendered object never has, so a
	// spec-equality check against them never matches - see applyCreateOnly's
	// doc comment for what that caused when this used applyOwned instead.
	// LXCMachineTemplates/KubeadmConfigTemplates are safe to create-only even
	// for a spec.workerPools edit: workerMachineTemplateName folds the
	// pool's flavor into the object's own name, so an edited flavor always
	// means a brand new object here, never an update to an existing one.
	for _, obj := range []client.Object{
		render.LXCCluster(&wc),
		render.ControlPlaneLXCMachineTemplate(&wc),
	} {
		if err := applyCreateOnly(ctx, r.Client, r.Scheme, &wc, obj); err != nil {
			return ctrl.Result{}, fmt.Errorf("apply %s: %w", obj.GetObjectKind().GroupVersionKind().Kind, err)
		}
	}
	for _, pool := range wc.Spec.WorkerPools {
		for _, obj := range []client.Object{
			render.WorkerLXCMachineTemplate(&wc, pool),
			render.WorkerKubeadmConfigTemplate(&wc, pool),
		} {
			if err := applyCreateOnly(ctx, r.Client, r.Scheme, &wc, obj); err != nil {
				return ctrl.Result{}, fmt.Errorf("apply %s for pool %q: %w", obj.GetObjectKind().GroupVersionKind().Kind, pool.Name, err)
			}
		}
	}

	cluster := render.Cluster(&wc)
	if err := applyCreateOnly(ctx, r.Client, r.Scheme, &wc, cluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("apply Cluster: %w", err)
	}

	var existing clusterv1.Cluster
	if err := r.Get(ctx, types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}, &existing); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("get Cluster: %w", err)
		}
		setCondition(r.Recorder, &wc, &wc.Status.Conditions, wc.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
			benchv1alpha1.ReasonReconciling, "waiting for Cluster to be created")
		return ctrl.Result{}, r.Status().Update(ctx, &wc)
	}

	// KubeadmControlPlane/MachineDeployment must not be created until the
	// Cluster's controlPlaneEndpoint is populated (copied up from
	// LXCCluster.status once Incus provisions the load balancer): the
	// kubeadm bootstrap provider bakes controlPlaneEndpoint into the very
	// first control-plane machine's ClusterConfiguration, which is also what
	// tells kubeadm to add it as an apiserver certificate SAN. Created
	// before that, the machine boots with a cert that's only valid for its
	// own IP, and every client going through the load balancer's IP/DNS -
	// including this operator's own multicluster tracker - fails TLS
	// verification permanently for that machine's lifetime.
	if existing.Spec.ControlPlaneEndpoint.Host == "" {
		setCondition(r.Recorder, &wc, &wc.Status.Conditions, wc.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
			benchv1alpha1.ReasonReconciling, "waiting for the load balancer's controlPlaneEndpoint before creating the control plane")
		return ctrl.Result{RequeueAfter: 2 * time.Second}, r.Status().Update(ctx, &wc)
	}

	if err := applyCreateOnly(ctx, r.Client, r.Scheme, &wc, render.KubeadmControlPlane(&wc)); err != nil {
		return ctrl.Result{}, fmt.Errorf("apply KubeadmControlPlane: %w", err)
	}
	// MachineDeployments are applied only once the Cluster exists - CAPI's
	// own webhook/validation rejects a MachineDeployment naming a Cluster
	// that isn't there yet. Unlike everything above, a pool's
	// MachineDeployment genuinely is updated in place (see
	// applyMachineDeploymentPool) - replicas/flavor are the one day-2 edit
	// this controller supports.
	desiredPools := make(map[string]bool, len(wc.Spec.WorkerPools))
	for _, pool := range wc.Spec.WorkerPools {
		desiredPools[pool.Name] = true
		if err := applyMachineDeploymentPool(ctx, r.Client, r.Scheme, &wc, render.MachineDeployment(&wc, pool)); err != nil {
			return ctrl.Result{}, fmt.Errorf("apply MachineDeployment for pool %q: %w", pool.Name, err)
		}
	}
	if err := r.deleteRemovedWorkerPools(ctx, &wc, desiredPools); err != nil {
		return ctrl.Result{}, fmt.Errorf("delete removed worker pools: %w", err)
	}

	wc.Status.Phase = existing.Status.Phase
	wc.Status.ControlPlaneReady = existing.Status.Initialization.ControlPlaneInitialized != nil && *existing.Status.Initialization.ControlPlaneInitialized
	wc.Status.InfrastructureReady = existing.Status.Initialization.InfrastructureProvisioned != nil && *existing.Status.Initialization.InfrastructureProvisioned
	wc.Status.ObservedGeneration = wc.Generation

	if wc.Status.ControlPlaneReady {
		wc.Status.KubeconfigSecretRef = &corev1.LocalObjectReference{Name: render.KubeconfigSecretName(&wc)}
		r.ensureAddons(ctx, &wc)
	}

	ready := metav1.ConditionFalse
	message := "waiting for Cluster to become ready"
	if cond := meta.FindStatusCondition(existing.Status.Conditions, clusterv1.ClusterAvailableCondition); cond != nil {
		message = cond.Message
	}
	if wc.Status.ControlPlaneReady && wc.Status.InfrastructureReady {
		ready = metav1.ConditionTrue
		message = "control plane and infrastructure ready"
	}
	setCondition(r.Recorder, &wc, &wc.Status.Conditions, wc.Generation, benchv1alpha1.ConditionReady, ready, benchv1alpha1.ReasonReady, message)

	if err := r.Status().Update(ctx, &wc); err != nil {
		return ctrl.Result{}, err
	}
	log.Info("reconciled WorkloadCluster", "phase", wc.Status.Phase, "ready", ready)
	return ctrl.Result{}, nil
}

// ensureAddons installs every best-effort, idempotent addon a Ready control
// plane needs, once its own ensure* method is implemented the same way
// (plain Create, ignore AlreadyExists). Each addon's failure is logged and
// reported as an Event rather than failing the whole reconcile: a
// transient failure here (e.g. the API server mid-cert-rotation) shouldn't
// block updating the rest of this object's status, and the next reconcile
// just retries it.
//
//   - CNI (ensureCNI): kubeadm doesn't install one itself - without it,
//     kubelet reports "cni plugin not initialized" and every Node stays
//     NotReady forever, so nothing can ever schedule.
//   - csi-incus (ensureCSIIncus): without a default StorageClass, any PVC
//     left without an explicit storageClassName (the authentik chart's
//     bundled postgresql, notably) stays Pending forever with "unbound
//     immediate PersistentVolumeClaims", so any Helm install waiting on it
//     times out. Installs csi-incus (github.com/BeryJu/csi-incus) and makes
//     its StorageClass the cluster default (see CSIIncusStorageClass) - real
//     Incus-backed, resizable, snapshot-capable volumes, the only
//     StorageClass a workload cluster gets now (local-path-provisioner was
//     dropped once this became available). Incus-specific, so a no-op when
//     spec.incus isn't set (guarded inside ensureCSIIncus itself, same as
//     the provider check earlier in Reconcile).
//   - metrics-server (ensureMetricsServer): without it, there's no
//     metrics.k8s.io API on this cluster at all (kubeadm doesn't install it
//     any more than it installs a CNI), so `kubectl top` has nothing to read
//     from.
//   - Prometheus (ensurePrometheus): unlike the addons above, this one also
//     resolves a URL cmd/apiserver (on the management cluster) can reach: a
//     management-cluster pod can reach a workload cluster's node IPs
//     directly (confirmed live - the reverse can't), which is what makes the
//     NodePort ensurePrometheus creates actually useful cross-cluster.
//   - cluster-autoscaler (ensureClusterAutoscaler): a no-op unless at least
//     one worker pool actually sets spec.autoscaling - installing an
//     always-on cluster-autoscaler for every WorkloadCluster regardless of
//     whether anything asked for it would be pure waste, the same reasoning
//     ensureCSIIncus already applies to spec.incus being unset.
//   - Pyroscope/Tempo (ensurePyroscope/ensureTempo): without these,
//     authentik's own AUTHENTIK_PYROSCOPE_HOST/
//     AUTHENTIK_ERROR_REPORTING__OTEL_ENDPOINT env vars
//     (internal/render/helmrelease.go) point at nothing.
//   - Grafana (ensureGrafana): pre-wired with datasources for all three
//     above, so it's useful the moment it comes up.
func (r *WorkloadClusterReconciler) ensureAddons(ctx context.Context, wc *benchv1alpha1.WorkloadCluster) {
	log := logf.FromContext(ctx)
	addons := []struct {
		name   string
		ensure func(context.Context, *benchv1alpha1.WorkloadCluster) error
	}{
		{"CNI", r.ensureCNI},
		{"csi-incus", r.ensureCSIIncus},
		{"metrics-server", r.ensureMetricsServer},
		{"Prometheus", r.ensurePrometheus},
		{"cluster-autoscaler", r.ensureClusterAutoscaler},
		{"Pyroscope", r.ensurePyroscope},
		{"Tempo", r.ensureTempo},
		{"Grafana", r.ensureGrafana},
		{"descheduler", r.ensureDescheduler},
	}
	for _, addon := range addons {
		if err := addon.ensure(ctx, wc); err != nil {
			log.Error(err, "failed to ensure addon on workload cluster", "addon", addon.name)
			r.Recorder.Eventf(wc, corev1.EventTypeWarning, "AddonFailed", "failed to ensure %s: %v", addon.name, err)
		}
	}
}

// remoteAddonClient builds a one-off client directly from the
// "<name>-kubeconfig" Secret CAPI writes, rather than
// internal/multicluster.Tracker: this controller only ever needs occasional
// addon applies (ensurePrometheus, ensurePyroscope, ensureTempo - the
// project-authored addons with no upstream Helm chart worth depending on),
// not the long-lived cached/watching connection Tracker exists for (that's
// what AuthentikDeployment/TestCase/TestRun use once they actually deploy onto
// this cluster via clusterRef). ensureCNI/ensureCSIIncus/ensureMetricsServer
// install via their own official Helm charts instead (Flux HelmRelease
// applied locally with spec.kubeConfig), so they never need this.
func (r *WorkloadClusterReconciler) remoteAddonClient(ctx context.Context, wc *benchv1alpha1.WorkloadCluster) (client.Client, error) {
	kubeconfigBytes, err := capikubeconfig.FromSecret(ctx, r.Client, client.ObjectKey{Name: wc.Name, Namespace: wc.Namespace})
	if err != nil {
		return nil, fmt.Errorf("get kubeconfig: %w", err)
	}
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		return nil, fmt.Errorf("build rest config: %w", err)
	}
	remoteClient, err := client.New(restConfig, client.Options{})
	if err != nil {
		return nil, fmt.Errorf("build remote client: %w", err)
	}
	return remoteClient, nil
}

// ensureCNI installs Flannel onto the workload cluster via its own official
// Helm chart - a Flux HelmRepository + HelmRelease applied here on the
// management cluster with spec.kubeConfig pointing helm-controller at the
// workload cluster, the same mechanism ensureCSIIncus uses.
func (r *WorkloadClusterReconciler) ensureCNI(ctx context.Context, wc *benchv1alpha1.WorkloadCluster) error {
	if err := applyOwned(ctx, r.Client, r.Scheme, wc, render.CNIHelmRepository(wc.Namespace)); err != nil {
		return fmt.Errorf("apply CNI HelmRepository: %w", err)
	}
	hr := render.CNIHelmRelease(wc.Namespace, render.KubeconfigSecretName(wc))
	if err := applyOwned(ctx, r.Client, r.Scheme, wc, hr); err != nil {
		return fmt.Errorf("apply CNI HelmRelease: %w", err)
	}
	return nil
}

// ensureCSIIncus installs csi-incus (github.com/BeryJu/csi-incus) onto the
// workload cluster via its own published Helm chart - a Flux
// HelmRepository + HelmRelease applied here on the management cluster with
// spec.kubeConfig pointing helm-controller at the workload cluster,
// mirroring exactly how AuthentikDeploymentReconciler installs the authentik
// chart remotely (internal/render/helmrelease.go). Chart values are built
// by remapping this WorkloadCluster's own Incus/LXC connection Secret
// (spec.incus.lxcSecretRef) into the shape the chart expects (see
// render.CSIIncusHelmValues) - reusing the existing CAPI provisioning
// credentials rather than requiring a second certificate. This is the only
// StorageClass a workload cluster gets - no more local-path-provisioner
// fallback. A no-op when spec.incus isn't set: csi-incus is Incus-specific,
// unlike ensureCNI/ensureMetricsServer.
func (r *WorkloadClusterReconciler) ensureCSIIncus(ctx context.Context, wc *benchv1alpha1.WorkloadCluster) error {
	if wc.Spec.Incus == nil {
		return nil
	}
	var source corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Name: wc.Spec.Incus.LXCSecretRef.Name, Namespace: wc.Namespace}, &source); err != nil {
		return fmt.Errorf("get incus credentials secret: %w", err)
	}
	values, err := render.CSIIncusHelmValues(&source, wc)
	if err != nil {
		return err
	}
	if err := applyOwned(ctx, r.Client, r.Scheme, wc, render.CSIIncusHelmRepository(wc.Namespace)); err != nil {
		return fmt.Errorf("apply csi-incus HelmRepository: %w", err)
	}
	hr := render.CSIIncusHelmRelease(wc.Namespace, render.KubeconfigSecretName(wc), values)
	if err := applyOwned(ctx, r.Client, r.Scheme, wc, hr); err != nil {
		return fmt.Errorf("apply csi-incus HelmRelease: %w", err)
	}
	return nil
}

// ensureDescheduler installs kubernetes-sigs/descheduler onto the workload
// cluster via its own official Helm chart - same Flux HelmRepository +
// HelmRelease mechanism as ensureCNI/ensureCSIIncus/ensureMetricsServer. See
// render.DeschedulerHelmValues' own doc comment for why it exists and what
// (little) is overridden from the chart's own defaults.
func (r *WorkloadClusterReconciler) ensureDescheduler(ctx context.Context, wc *benchv1alpha1.WorkloadCluster) error {
	values, err := render.DeschedulerHelmValues(wc)
	if err != nil {
		return err
	}
	if err := applyOwned(ctx, r.Client, r.Scheme, wc, render.DeschedulerHelmRepository(wc.Namespace)); err != nil {
		return fmt.Errorf("apply descheduler HelmRepository: %w", err)
	}
	hr := render.DeschedulerHelmRelease(wc.Namespace, render.KubeconfigSecretName(wc), values)
	if err := applyOwned(ctx, r.Client, r.Scheme, wc, hr); err != nil {
		return fmt.Errorf("apply descheduler HelmRelease: %w", err)
	}
	return nil
}

// ensureMetricsServer installs kubernetes-sigs/metrics-server onto the
// workload cluster via its own official Helm chart - same Flux
// HelmRepository + HelmRelease mechanism as ensureCNI/ensureCSIIncus.
func (r *WorkloadClusterReconciler) ensureMetricsServer(ctx context.Context, wc *benchv1alpha1.WorkloadCluster) error {
	values, err := render.MetricsServerHelmValues(wc)
	if err != nil {
		return err
	}
	if err := applyOwned(ctx, r.Client, r.Scheme, wc, render.MetricsServerHelmRepository(wc.Namespace)); err != nil {
		return fmt.Errorf("apply metrics-server HelmRepository: %w", err)
	}
	hr := render.MetricsServerHelmRelease(wc.Namespace, render.KubeconfigSecretName(wc), values)
	if err := applyOwned(ctx, r.Client, r.Scheme, wc, hr); err != nil {
		return fmt.Errorf("apply metrics-server HelmRelease: %w", err)
	}
	return nil
}

// controlPlaneNodeRoleLabel is kubeadm's own standard label, set on every
// control-plane node regardless of whether its NoSchedule taint is still
// present - a more stable selector for "find a node that will always
// exist" than trying to key off any particular worker pool.
const controlPlaneNodeRoleLabel = "node-role.kubernetes.io/control-plane"

// reachableNodeIP returns an InternalIP a management-cluster pod can use to
// reach this workload cluster directly - confirmed live this session that
// this direction works even though the reverse (workload cluster pod to
// management cluster pod) doesn't. Prefers a control-plane node (long-lived
// by construction; worker pools can be resized/replaced), falling back to
// whatever the first Node in the list reports.
func reachableNodeIP(ctx context.Context, remoteClient client.Client) (string, error) {
	var nodes corev1.NodeList
	if err := remoteClient.List(ctx, &nodes, client.MatchingLabels{controlPlaneNodeRoleLabel: ""}); err != nil {
		return "", fmt.Errorf("list control-plane nodes: %w", err)
	}
	if len(nodes.Items) == 0 {
		if err := remoteClient.List(ctx, &nodes); err != nil {
			return "", fmt.Errorf("list nodes: %w", err)
		}
	}
	for _, node := range nodes.Items {
		for _, addr := range node.Status.Addresses {
			if addr.Type == corev1.NodeInternalIP {
				return addr.Address, nil
			}
		}
	}
	return "", fmt.Errorf("no node with an InternalIP found")
}

// ensurePrometheus installs kube-prometheus-stack (prometheus-operator +
// Prometheus with the remote-write receiver enabled + node-exporter +
// kube-state-metrics + Alertmanager, pre-wired with this project's own
// default alert rules - see render.KubePrometheusStackHelmValues) onto the
// workload cluster via its own official Helm chart - the same Flux
// HelmRepository + HelmRelease mechanism ensureCNI/ensureCSIIncus/
// ensureMetricsServer already use, applied here on the management cluster
// with spec.kubeConfig pointing at the workload cluster, rather than the
// hand-rolled single-Deployment Prometheus this replaces. node-exporter is
// the actual point of the switch: the prior install had no per-node
// CPU/memory/disk/network metrics at all. Alertmanager is bundled into this
// same ensure-step (not a separate one) because prometheus-operator wires
// the Alertmanager CR it creates straight to the Prometheus CR it also
// creates - splitting them would only add a second HelmRelease for no
// benefit. Records management-cluster-reachable URLs for both in this
// object's own status so cmd/apiserver/the web UI can query them directly
// (see internal/api/metrics.go's per-testid Prometheus resolution) -
// Prometheus's own URL is unchanged from before, since the chart-managed
// Prometheus is still reachable at the exact same compat Service
// name/NodePort (see render.PrometheusServiceName's own doc comment for why
// no other consumer needed to change).
func (r *WorkloadClusterReconciler) ensurePrometheus(ctx context.Context, wc *benchv1alpha1.WorkloadCluster) error {
	values, err := render.KubePrometheusStackHelmValues(wc)
	if err != nil {
		return err
	}
	if err := applyOwned(ctx, r.Client, r.Scheme, wc, render.KubePrometheusStackHelmRepository(wc.Namespace)); err != nil {
		return fmt.Errorf("apply kube-prometheus-stack HelmRepository: %w", err)
	}
	hr := render.KubePrometheusStackHelmRelease(wc.Namespace, render.KubeconfigSecretName(wc), values)
	if err := applyOwned(ctx, r.Client, r.Scheme, wc, hr); err != nil {
		return fmt.Errorf("apply kube-prometheus-stack HelmRelease: %w", err)
	}

	remoteClient, err := r.remoteAddonClient(ctx, wc)
	if err != nil {
		return err
	}
	// Applied directly, not through the HelmRelease's own extraManifests -
	// see render.KubePrometheusStackHelmValues' own doc comment on why a
	// PrometheusRule containing "{{ $labels... }}" annotations can't go
	// through that path.
	if err := createAll(ctx, remoteClient, []client.Object{render.DefaultAlertRules()}); err != nil {
		return fmt.Errorf("apply default PrometheusRule: %w", err)
	}
	nodeIP, err := reachableNodeIP(ctx, remoteClient)
	if err != nil {
		return fmt.Errorf("resolve reachable node IP: %w", err)
	}
	wc.Status.PrometheusURL = fmt.Sprintf("http://%s:%d", nodeIP, render.PrometheusNodePort)
	wc.Status.AlertmanagerURL = fmt.Sprintf("http://%s:%d", nodeIP, render.AlertmanagerNodePort)
	return nil
}

// ensureClusterAutoscaler installs cluster-autoscaler (kubernetes.github.io/
// autoscaler, --cloud-provider=clusterapi) for this WorkloadCluster if at
// least one of its worker pools sets spec.autoscaling - a no-op otherwise.
// Unlike every other addon in this file, this applies locally (r.Client, no
// spec.kubeConfig on the HelmRelease) rather than remotely onto the
// workload cluster: cluster-autoscaler must run wherever the Cluster API
// MachineDeployment/Cluster objects it watches actually live, which is the
// management cluster this operator itself runs on (see
// render.ClusterAutoscalerHelmRelease's own doc comment for the verified
// clusterAPIMode reasoning).
func (r *WorkloadClusterReconciler) ensureClusterAutoscaler(ctx context.Context, wc *benchv1alpha1.WorkloadCluster) error {
	if !render.HasAutoscalingPool(wc) {
		// Every pool's autoscaling was removed (or never set) - tear down a
		// previously-installed instance rather than leave it running
		// forever against node groups that no longer carry the
		// min/max-size annotations it was managing. Plain Delete, not a
		// wait for helm-controller's own uninstall to finish (unlike
		// AuthentikDeploymentReconciler's own deletion path) - this has no
		// target-namespace sweep afterward that would race an in-progress
		// uninstall, so there's nothing to block on.
		hr := render.ClusterAutoscalerHelmRelease(wc, nil)
		if err := r.Delete(ctx, hr); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete cluster-autoscaler HelmRelease: %w", err)
		}
		return nil
	}
	values, err := render.ClusterAutoscalerHelmValues(wc)
	if err != nil {
		return err
	}
	if err := applyOwned(ctx, r.Client, r.Scheme, wc, render.ClusterAutoscalerHelmRepository(wc.Namespace)); err != nil {
		return fmt.Errorf("apply cluster-autoscaler HelmRepository: %w", err)
	}
	hr := render.ClusterAutoscalerHelmRelease(wc, values)
	if err := applyOwned(ctx, r.Client, r.Scheme, wc, hr); err != nil {
		return fmt.Errorf("apply cluster-autoscaler HelmRelease: %w", err)
	}
	return nil
}

// ensurePyroscope installs a single-replica Grafana Pyroscope onto the
// workload cluster if it isn't there already. No cross-cluster
// reachability needed - authentik's own AUTHENTIK_PYROSCOPE_HOST already
// points at this exact in-cluster DNS name (defaultPyroscopeHost in
// authentikdeployment_controller.go), and nothing outside this cluster reads
// its data (yet).
func (r *WorkloadClusterReconciler) ensurePyroscope(ctx context.Context, wc *benchv1alpha1.WorkloadCluster) error {
	remoteClient, err := r.remoteAddonClient(ctx, wc)
	if err != nil {
		return err
	}
	return createAll(ctx, remoteClient, render.PyroscopeObjects(wc))
}

// ensureTempo installs a single-replica Grafana Tempo onto the workload
// cluster if it isn't there already - same reasoning as ensurePyroscope,
// for AUTHENTIK_ERROR_REPORTING__OTEL_ENDPOINT (defaultTracingEndpoint).
func (r *WorkloadClusterReconciler) ensureTempo(ctx context.Context, wc *benchv1alpha1.WorkloadCluster) error {
	remoteClient, err := r.remoteAddonClient(ctx, wc)
	if err != nil {
		return err
	}
	return createAll(ctx, remoteClient, render.TempoObjects(wc))
}

// ensureGrafana installs a single-replica Grafana onto the workload cluster
// if it isn't there already, pre-wired with Prometheus/Pyroscope/Tempo
// datasources and this project's own 5 dashboards (see
// render.GrafanaObjects). Unlike Pyroscope/Tempo, this does need a
// cross-cluster URL recorded: cmd/apiserver's own reverse proxy
// (internal/api/grafana_proxy.go) is what the web UI's Grafana page actually
// embeds, and it connects to Grafana via this NodePort URL, mirroring
// ensurePrometheus exactly.
func (r *WorkloadClusterReconciler) ensureGrafana(ctx context.Context, wc *benchv1alpha1.WorkloadCluster) error {
	remoteClient, err := r.remoteAddonClient(ctx, wc)
	if err != nil {
		return err
	}
	// Generated once on the management cluster (not the remote one): both
	// cmd/apiserver (which proxies to Grafana) and this Deployment's own env
	// need the exact same value, and only the management cluster is
	// somewhere cmd/apiserver can already read a Secret from directly - see
	// render.GrafanaAdminSecretName's own doc comment.
	adminPassword, err := ensureRandomSecret(ctx, r.Client,
		types.NamespacedName{Name: render.GrafanaAdminSecretName(wc), Namespace: wc.Namespace},
		"password", 16)
	if err != nil {
		return fmt.Errorf("ensure Grafana admin password: %w", err)
	}
	if err := createAll(ctx, remoteClient, render.GrafanaObjects(wc, adminPassword)); err != nil {
		return err
	}
	nodeIP, err := reachableNodeIP(ctx, remoteClient)
	if err != nil {
		return fmt.Errorf("resolve reachable node IP: %w", err)
	}
	wc.Status.GrafanaURL = fmt.Sprintf("http://%s:%d", nodeIP, render.GrafanaNodePort)
	return nil
}

// createAll applies every object best-effort/idempotently, the same way for
// vendored-YAML addons (decoded to unstructured.Unstructured: CNI, storage,
// metrics-server) and project-authored ones (native typed structs like
// *appsv1.Deployment: Prometheus/Pyroscope/Tempo) - client.Object is the one
// interface both satisfy.
func createAll(ctx context.Context, c client.Client, objs []client.Object) error {
	for _, obj := range objs {
		if err := createIfMissing(ctx, c, obj); err != nil {
			return err
		}
	}
	return nil
}

// createIfMissing is createAll's per-object step, its own function so the
// "does this already exist" recheck below can run twice (once before,
// optionally once after Create) without duplicating the error-wrapping.
//
// A plain "Create, ignore AlreadyExists" (this function's own first version)
// is not enough for a NodePort Service with an explicit spec.ports[].nodePort:
// confirmed live that re-Create-ing one that already exists with the exact
// same requested port fails with a spec-validation error ("provided port is
// already allocated"), not AlreadyExists - the apiserver's port allocator
// runs before the etcd-level existence check, so an object that's already
// there in every way still gets rejected at the allocation step first. Every
// pinned-NodePort addon this file renders (Prometheus, Grafana, Pyroscope)
// hit this on every single reconcile after their first successful creation,
// silently logged as an AddonFailed event forever after - not a fluke this
// session happened to catch once.
func createIfMissing(ctx context.Context, c client.Client, obj client.Object) error {
	gvk := obj.GetObjectKind().GroupVersionKind()
	key := client.ObjectKeyFromObject(obj)
	existing := obj.DeepCopyObject().(client.Object)
	if err := c.Get(ctx, key, existing); err == nil {
		return nil
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get %s %s/%s: %w", gvk.Kind, key.Namespace, key.Name, err)
	}
	err := c.Create(ctx, obj)
	if err == nil || apierrors.IsAlreadyExists(err) {
		return nil
	}
	// Re-check before giving up: a Create that failed for any reason -
	// including the NodePort-allocation case above - might still mean the
	// object is there and fine, just not via a race Get-then-Create alone
	// can fully close.
	if getErr := c.Get(ctx, key, existing); getErr == nil {
		return nil
	}
	return fmt.Errorf("create %s %s/%s: %w", gvk.Kind, key.Namespace, key.Name, err)
}

// deleteRemovedWorkerPools deletes the MachineDeployment (and, via CAPI's own
// garbage collection of a deleted MachineDeployment's owned objects, its
// Machines) for any pool that used to be in spec.workerPools but no longer
// is. Listed by the cluster-name label render.MachineDeployment sets on
// every pool it renders, rather than an owner-reference index, since every
// pool's MachineDeployment already carries that label for CAPI's own
// benefit.
func (r *WorkloadClusterReconciler) deleteRemovedWorkerPools(ctx context.Context, wc *benchv1alpha1.WorkloadCluster, desiredPools map[string]bool) error {
	var list clusterv1.MachineDeploymentList
	if err := r.List(ctx, &list, client.InNamespace(wc.Namespace),
		client.MatchingLabels{"cluster.x-k8s.io/cluster-name": wc.Name}); err != nil {
		return fmt.Errorf("list MachineDeployments: %w", err)
	}
	for i := range list.Items {
		md := &list.Items[i]
		poolName, ok := strings.CutPrefix(md.Name, wc.Name+"-")
		if !ok || desiredPools[poolName] {
			continue
		}
		if err := r.Delete(ctx, md); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete MachineDeployment %s: %w", md.Name, err)
		}
	}
	return nil
}

func (r *WorkloadClusterReconciler) reconcileDelete(ctx context.Context, wc *benchv1alpha1.WorkloadCluster) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(wc, workloadClusterFinalizer) {
		return ctrl.Result{}, nil
	}

	// Deleting the owned Cluster (rather than relying on GC once the
	// WorkloadCluster itself disappears) lets CAPI actually tear down its
	// machines before this finalizer is removed - the LXCMachineTemplates/
	// KubeadmControlPlane/MachineDeployment all get garbage-collected as the
	// Cluster's owned objects once CAPI is done with them.
	var cluster clusterv1.Cluster
	err := r.Get(ctx, types.NamespacedName{Name: wc.Name, Namespace: wc.Namespace}, &cluster)
	switch {
	case apierrors.IsNotFound(err):
		controllerutil.RemoveFinalizer(wc, workloadClusterFinalizer)
		return ctrl.Result{}, r.Update(ctx, wc)
	case err != nil:
		return ctrl.Result{}, fmt.Errorf("get Cluster: %w", err)
	default:
		if cluster.DeletionTimestamp.IsZero() {
			if err := r.Delete(ctx, &cluster); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, fmt.Errorf("delete Cluster: %w", err)
			}
		}
		return ctrl.Result{RequeueAfter: capiDeleteRequeueInterval}, nil
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *WorkloadClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&benchv1alpha1.WorkloadCluster{}).
		Owns(&clusterv1.Cluster{}).
		Named("workloadcluster").
		Complete(r)
}
