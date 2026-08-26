/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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
	Scheme *runtime.Scheme
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
		setCondition(&wc.Status.Conditions, wc.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
			benchv1alpha1.ReasonReconcileError,
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
		setCondition(&wc.Status.Conditions, wc.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
			"Reconciling", "waiting for Cluster to be created")
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
		setCondition(&wc.Status.Conditions, wc.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
			"Reconciling", "waiting for the load balancer's controlPlaneEndpoint before creating the control plane")
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
		// kubeadm doesn't install a CNI itself - without one, kubelet reports
		// "cni plugin not initialized" and every Node stays NotReady forever,
		// so nothing can ever schedule. Best-effort and idempotent (plain
		// Create, ignore AlreadyExists): logged rather than failing the whole
		// reconcile, since a transient failure here (e.g. the API server
		// mid-cert-rotation) shouldn't block updating the rest of this
		// object's status, and the next reconcile just retries it.
		if err := r.ensureCNI(ctx, &wc); err != nil {
			log.Error(err, "failed to ensure CNI on workload cluster")
		}
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
	setCondition(&wc.Status.Conditions, wc.Generation, benchv1alpha1.ConditionReady, ready, "Ready", message)

	if err := r.Status().Update(ctx, &wc); err != nil {
		return ctrl.Result{}, err
	}
	log.Info("reconciled WorkloadCluster", "phase", wc.Status.Phase, "ready", ready)
	return ctrl.Result{}, nil
}

// ensureCNI installs Flannel onto the workload cluster if it isn't there
// already. Uses a one-off client built directly from the "<name>-kubeconfig"
// Secret CAPI writes, rather than internal/multicluster.Tracker: this
// controller only ever needs an occasional apply here, not the long-lived
// cached/watching connection Tracker exists for (that's what
// AuthentikTopology/TestCase/TestRun use once they actually deploy onto this
// cluster via clusterRef).
func (r *WorkloadClusterReconciler) ensureCNI(ctx context.Context, wc *benchv1alpha1.WorkloadCluster) error {
	kubeconfigBytes, err := capikubeconfig.FromSecret(ctx, r.Client, client.ObjectKey{Name: wc.Name, Namespace: wc.Namespace})
	if err != nil {
		return fmt.Errorf("get kubeconfig: %w", err)
	}
	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		return fmt.Errorf("build rest config: %w", err)
	}
	remoteClient, err := client.New(restConfig, client.Options{})
	if err != nil {
		return fmt.Errorf("build remote client: %w", err)
	}

	objs, err := render.CNIObjects()
	if err != nil {
		return fmt.Errorf("render CNI manifests: %w", err)
	}
	for _, obj := range objs {
		if err := remoteClient.Create(ctx, obj); err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("create %s %s/%s: %w", obj.GetKind(), obj.GetNamespace(), obj.GetName(), err)
		}
	}
	return nil
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
