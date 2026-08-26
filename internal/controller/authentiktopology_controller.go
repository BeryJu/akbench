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
	"strconv"
	"time"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
	"github.com/goauthentik/authentik-bench/internal/multicluster"
	"github.com/goauthentik/authentik-bench/internal/render"
)

// authentikTopologyFinalizer guards the remote objects (namespace, CNPG
// Cluster, generated Secrets - everything spec.clusterRef puts on a
// different cluster than the AuthentikTopology object itself) that
// Kubernetes garbage collection cannot reach on its own.
const authentikTopologyFinalizer = "bench.goauthentik.io/remote-cleanup"

// defaultPyroscopeHost/defaultTracingEndpoint are the central metrics
// stack's Pyroscope/Tempo, used whenever a topology doesn't set
// spec.authentik.pyroscopeHost/errorReporting.tracingEndpoint itself -
// mirrors defaultRemoteWriteURL's env-overridable-default pattern
// (testcase_controller.go) so every topology gets profiling/tracing wired up
// for free instead of needing it configured per topology.
var (
	defaultPyroscopeHost   = envOrDefault("BENCH_PYROSCOPE_HOST", "http://pyroscope.bench-system.svc.cluster.local:4040")
	defaultTracingEndpoint = envOrDefault("BENCH_TRACING_ENDPOINT", "http://tempo.bench-system.svc.cluster.local:4318")
)

// kubeconfigSecretName is the Secret name convention Cluster API itself
// defines for a Cluster's admin kubeconfig - the same one
// render.KubeconfigSecretName (internal/render/workloadcluster.go) produces,
// duplicated here rather than imported to avoid a dependency from
// render (WorkloadCluster) back onto AuthentikTopology's naming.
func kubeconfigSecretName(clusterName string) string {
	return clusterName + "-kubeconfig"
}

// AuthentikTopologyReconciler reconciles an AuthentikTopology object.
//
// spec.clusterRef (a cluster.x-k8s.io Cluster name in the same namespace)
// deploys onto any cluster Tracker can reach - a Cluster API-managed
// WorkloadCluster or a pre-existing one - via the "<cluster>-kubeconfig"
// Secret convention CAPI defines. Empty deploys onto the management cluster
// the operator itself runs on. The HelmRelease/HelmRepository Flux objects
// always live on the management cluster regardless (Flux's own
// spec.kubeConfig.secretRef is what points helm-controller at the remote
// cluster) - only the namespace, postgres backend, and the Secrets this
// controller generates move with clusterRef.
type AuthentikTopologyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// RESTConfig is the management cluster's own config (mgr.GetConfig()) -
	// resolveRESTConfig's answer when spec.clusterRef is empty, the same
	// split resolveClient makes for the controller-runtime client.
	RESTConfig *rest.Config
	Tracker    *multicluster.Tracker
}

// resolveClient returns the client this reconcile should use for
// everything except the HelmRelease/HelmRepository themselves: r.Client
// when spec.clusterRef is empty, or a live client for the named Cluster via
// Tracker otherwise. A non-nil error here always means "not ready yet,
// requeue" (multicluster.ErrClusterNotConnected) rather than a permanent
// failure.
func (r *AuthentikTopologyReconciler) resolveClient(ctx context.Context, topo *benchv1alpha1.AuthentikTopology) (client.Client, error) {
	if topo.Spec.ClusterRef.Name == "" {
		return r.Client, nil
	}
	if r.Tracker == nil {
		return nil, fmt.Errorf("multicluster tracker not configured")
	}
	return r.Tracker.GetClient(ctx, client.ObjectKey{Namespace: topo.Namespace, Name: topo.Spec.ClusterRef.Name})
}

// resolveRESTConfig is resolveClient's counterpart for ApplyBlueprint, which
// needs a raw REST config (for pod exec) rather than a controller-runtime
// client - always the same cluster resolveClient itself would pick.
func (r *AuthentikTopologyReconciler) resolveRESTConfig(ctx context.Context, topo *benchv1alpha1.AuthentikTopology) (*rest.Config, error) {
	if topo.Spec.ClusterRef.Name == "" {
		return r.RESTConfig, nil
	}
	if r.Tracker == nil {
		return nil, fmt.Errorf("multicluster tracker not configured")
	}
	return r.Tracker.GetRESTConfig(ctx, client.ObjectKey{Namespace: topo.Namespace, Name: topo.Spec.ClusterRef.Name})
}

// resolveFixturesBlueprint looks up the fixtures blueprint ConfigMap, which,
// like extraValuesRef, always lives alongside the AuthentikTopology object
// itself (topo.Namespace on the management cluster) rather than on rc -
// `make fixtures` writes it there, and it's config for the deployment, not
// part of it. Optional: a topology with none configured just skips
// blueprint application entirely (fixtures are a dev/benchmark convenience,
// not required). When one is configured, also applies the PVC the chart's
// server pod will mount it into (see render.HelmValues' withBlueprintVolume)
// before the caller renders values referencing it.
func (r *AuthentikTopologyReconciler) resolveFixturesBlueprint(ctx context.Context, rc client.Client, topo *benchv1alpha1.AuthentikTopology) (bool, corev1.ConfigMap, error) {
	var cm corev1.ConfigMap
	if err := r.Get(ctx, types.NamespacedName{Name: fixturesBlueprintConfigMapName, Namespace: topo.Namespace}, &cm); err != nil {
		if apierrors.IsNotFound(err) {
			return false, cm, nil
		}
		return false, cm, fmt.Errorf("get fixtures blueprint configmap: %w", err)
	}
	if err := applyUnowned(ctx, rc, render.BlueprintPVC(topo)); err != nil {
		return false, cm, fmt.Errorf("apply blueprint PVC: %w", err)
	}
	return true, cm, nil
}

// applyExtraValues merges topo.Spec.Authentik.ExtraValuesRef's ConfigMap
// (which, like the fixtures blueprint ConfigMap, always lives alongside the
// AuthentikTopology object itself rather than on rc) over values, if one is
// referenced. done=true means the caller should return (result, err)
// immediately - either a real error, or this reconcile already updated
// status itself (a not-yet-existing ConfigMap requeues on a timer; invalid
// YAML doesn't, since the extra-values API always touches the topology
// object on save, which re-triggers this reconcile as soon as the user
// corrects it).
func (r *AuthentikTopologyReconciler) applyExtraValues(ctx context.Context, topo *benchv1alpha1.AuthentikTopology, values *apiextensionsv1.JSON) (*apiextensionsv1.JSON, ctrl.Result, bool, error) {
	ref := topo.Spec.Authentik.ExtraValuesRef
	if ref == nil || ref.Name == "" {
		return values, ctrl.Result{}, false, nil
	}
	var cm corev1.ConfigMap
	if err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: topo.Namespace}, &cm); err != nil {
		if apierrors.IsNotFound(err) {
			setCondition(&topo.Status.Conditions, topo.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
				benchv1alpha1.ReasonDependencyNotReady, fmt.Sprintf("extraValuesRef ConfigMap %q not found", ref.Name))
			if err := r.Status().Update(ctx, topo); err != nil {
				return nil, ctrl.Result{}, true, err
			}
			return nil, ctrl.Result{RequeueAfter: 5 * time.Second}, true, nil
		}
		return nil, ctrl.Result{}, true, fmt.Errorf("get extra values configmap: %w", err)
	}
	merged, err := render.MergeExtraValues(values, cm.Data["values.yaml"])
	if err != nil {
		setCondition(&topo.Status.Conditions, topo.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
			benchv1alpha1.ReasonReconcileError, fmt.Sprintf("invalid extraValuesRef YAML: %v", err))
		return nil, ctrl.Result{}, true, r.Status().Update(ctx, topo)
	}
	return merged, ctrl.Result{}, false, nil
}

// helmReleaseReadiness mirrors hr's own Ready condition - but only once
// helm-controller has actually observed the exact spec (generation) this
// reconcile just applied. Without that check, a Ready=True left over from
// the *previous* generation reads as ready immediately after this reconcile
// pushes a new one - fine for the Ready condition alone (self-corrects next
// reconcile), but reconcileBlueprint acts on this signal synchronously via
// pod exec, and raced a rolling update this way: it read a stale Ready=True
// from before a spec change added the fixtures volume mount, and exec'd
// into whichever pod happened to still be running - the old one, without
// the mount ("Directory nonexistent"), not the new one the still-in-flight
// rollout was replacing it with.
func (r *AuthentikTopologyReconciler) helmReleaseReadiness(ctx context.Context, hr *helmv2.HelmRelease) (metav1.ConditionStatus, string, error) {
	var existing helmv2.HelmRelease
	if err := r.Get(ctx, types.NamespacedName{Name: hr.Name, Namespace: hr.Namespace}, &existing); err != nil {
		if apierrors.IsNotFound(err) {
			return metav1.ConditionFalse, "waiting for helm-controller to reconcile", nil
		}
		return metav1.ConditionFalse, "", fmt.Errorf("get HelmRelease: %w", err)
	}
	if existing.Status.ObservedGeneration != existing.Generation {
		return metav1.ConditionFalse, "waiting for helm-controller to observe the latest spec", nil
	}
	cond := meta.FindStatusCondition(existing.Status.Conditions, "Ready")
	if cond == nil {
		return metav1.ConditionFalse, "waiting for helm-controller to reconcile", nil
	}
	if cond.Status == metav1.ConditionTrue {
		return metav1.ConditionTrue, "HelmRelease is Ready", nil
	}
	return metav1.ConditionFalse, cond.Message, nil
}

// reconcileBlueprint applies the fixtures blueprint (if any is configured)
// once the HelmRelease itself is Ready (a server pod actually exists to
// exec into), returning the BlueprintApplied condition to set and whether
// the caller should requeue on a timer to retry a transient failure. A
// topology with no blueprint configured reports Ready with nothing done, so
// it never blocks the overall Ready condition.
func (r *AuthentikTopologyReconciler) reconcileBlueprint(ctx context.Context, rc client.Client, topo *benchv1alpha1.AuthentikTopology,
	hasBlueprint bool, helmReleaseReady metav1.ConditionStatus, blueprintCM corev1.ConfigMap) (metav1.ConditionStatus, string, bool) {
	switch {
	case !hasBlueprint:
		return metav1.ConditionTrue, "no fixtures blueprint configured", false
	case helmReleaseReady != metav1.ConditionTrue:
		return metav1.ConditionFalse, "waiting for the HelmRelease to be Ready before applying the fixtures blueprint", false
	}

	restConfig, err := r.resolveRESTConfig(ctx, topo)
	if err != nil {
		return metav1.ConditionFalse, fmt.Sprintf("waiting for cluster to be reachable: %v", err), true
	}
	if err := r.ApplyBlueprint(ctx, restConfig, rc, topo, []byte(blueprintCM.Data[fixturesBlueprintKey])); err != nil {
		return metav1.ConditionFalse, err.Error(), true
	}
	return metav1.ConditionTrue, "fixtures blueprint applied", false
}

// +kubebuilder:rbac:groups=bench.goauthentik.io,resources=authentiktopologies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bench.goauthentik.io,resources=authentiktopologies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=bench.goauthentik.io,resources=authentiktopologies/finalizers,verbs=update
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,resources=helmreleases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=helmrepositories,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=pods/exec,verbs=create

func (r *AuthentikTopologyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var topo benchv1alpha1.AuthentikTopology
	if err := r.Get(ctx, req.NamespacedName, &topo); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !topo.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &topo)
	}
	if !controllerutil.ContainsFinalizer(&topo, authentikTopologyFinalizer) {
		controllerutil.AddFinalizer(&topo, authentikTopologyFinalizer)
		if err := r.Update(ctx, &topo); err != nil {
			return ctrl.Result{}, err
		}
	}

	rc, err := r.resolveClient(ctx, &topo)
	if err != nil {
		setCondition(&topo.Status.Conditions, topo.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
			benchv1alpha1.ReasonDependencyNotReady, fmt.Sprintf("waiting for cluster %q to be reachable: %v", topo.Spec.ClusterRef.Name, err))
		if err := r.Status().Update(ctx, &topo); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	if err := ensureNamespace(ctx, rc, topo.Spec.Namespace); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure namespace: %w", err)
	}

	pgConn, pgReady, err := r.resolvePostgres(ctx, rc, &topo)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("resolve postgres: %w", err)
	}
	if !pgReady {
		setCondition(&topo.Status.Conditions, topo.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
			benchv1alpha1.ReasonReconciling, "waiting on postgres backend")
		if err := r.Status().Update(ctx, &topo); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	repo := render.HelmRepository(topo.Namespace, topo.Spec.Chart)
	if err := applyOwned(ctx, r.Client, r.Scheme, &topo, repo); err != nil {
		return ctrl.Result{}, fmt.Errorf("apply HelmRepository: %w", err)
	}

	// The chart does not generate AUTHENTIK_SECRET_KEY itself - authentik
	// refuses to start without one - so the operator generates and caches it
	// once, the same way the Ansible harness's credentials/ directory does.
	// Passed as a literal value (authentik.secret_key), not via
	// authentik.existingSecret: existingSecret tells the chart to skip
	// rendering its own authentik.env-derived config Secret entirely, which
	// is also where postgresql.*/log_level/error_reporting get baked into
	// AUTHENTIK_* env vars - existingSecret would silently drop all of those.
	secretKey, err := ensureRandomSecret(ctx, rc,
		types.NamespacedName{Name: topo.Name + "-authentik-config", Namespace: topo.Spec.Namespace},
		"AUTHENTIK_SECRET_KEY", 32)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure authentik secret key: %w", err)
	}

	pyroscopeHost := topo.Spec.Authentik.PyroscopeHost
	if pyroscopeHost == "" {
		pyroscopeHost = defaultPyroscopeHost
	}
	tracingEndpoint := topo.Spec.Authentik.ErrorReporting.TracingEndpoint
	if tracingEndpoint == "" {
		tracingEndpoint = defaultTracingEndpoint
	}

	hasBlueprint, blueprintCM, err := r.resolveFixturesBlueprint(ctx, rc, &topo)
	if err != nil {
		return ctrl.Result{}, err
	}

	values, err := render.HelmValues(&topo, secretKey, pgConn, pyroscopeHost, tracingEndpoint, hasBlueprint)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("build helm values: %w", err)
	}
	merged, result, done, err := r.applyExtraValues(ctx, &topo, values)
	if done {
		return result, err
	}
	values = merged
	kubeConfigSecretName := ""
	if topo.Spec.ClusterRef.Name != "" {
		kubeConfigSecretName = kubeconfigSecretName(topo.Spec.ClusterRef.Name)
	}
	hr := render.HelmRelease(&topo, kubeConfigSecretName, values)
	if err := applyOwned(ctx, r.Client, r.Scheme, &topo, hr); err != nil {
		return ctrl.Result{}, fmt.Errorf("apply HelmRelease: %w", err)
	}

	helmReleaseReady, helmReleaseMessage, err := r.helmReleaseReadiness(ctx, hr)
	if err != nil {
		return ctrl.Result{}, err
	}
	setCondition(&topo.Status.Conditions, topo.Generation, benchv1alpha1.ConditionHelmReleaseReady, helmReleaseReady,
		benchv1alpha1.ReasonReady, helmReleaseMessage)

	topo.Status.HelmReleaseRef = &corev1.LocalObjectReference{Name: hr.Name}
	topo.Status.ServiceEndpoint = render.ServiceEndpoint(&topo)
	topo.Status.ObservedGeneration = topo.Generation

	blueprintReady, blueprintMessage, requeueForBlueprint := r.reconcileBlueprint(ctx, rc, &topo, hasBlueprint, helmReleaseReady, blueprintCM)
	setCondition(&topo.Status.Conditions, topo.Generation, benchv1alpha1.ConditionBlueprintApplied, blueprintReady,
		benchv1alpha1.ReasonReady, blueprintMessage)

	ready := helmReleaseReady == metav1.ConditionTrue && blueprintReady == metav1.ConditionTrue
	if ready {
		setCondition(&topo.Status.Conditions, topo.Generation, benchv1alpha1.ConditionReady, metav1.ConditionTrue,
			benchv1alpha1.ReasonReady, "authentik chart installed and ready")
	} else {
		msg := helmReleaseMessage
		if helmReleaseReady == metav1.ConditionTrue {
			msg = blueprintMessage
		}
		setCondition(&topo.Status.Conditions, topo.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
			benchv1alpha1.ReasonReconciling, msg)
	}

	if err := r.Status().Update(ctx, &topo); err != nil {
		return ctrl.Result{}, err
	}
	log.Info("reconciled AuthentikTopology", "ready", ready)
	if requeueForBlueprint {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

func (r *AuthentikTopologyReconciler) reconcileDelete(ctx context.Context, topo *benchv1alpha1.AuthentikTopology) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	if !controllerutil.ContainsFinalizer(topo, authentikTopologyFinalizer) {
		return ctrl.Result{}, nil
	}

	// Delete the HelmRelease first and wait for helm-controller's own
	// finalizer to finish running `helm uninstall`, instead of deleting
	// spec.namespace immediately. `helm uninstall` is what correctly removes
	// resources the chart renders as cluster-scoped (e.g. authentik's own
	// ClusterRole/ClusterRoleBinding) - those live in neither the
	// HelmRelease's namespace nor spec.namespace, so a namespace delete can
	// never reach them, and an orphaned one then blocks every future install
	// under the same release name with an ownership-conflict error.
	var hr helmv2.HelmRelease
	err := r.Get(ctx, types.NamespacedName{Name: render.HelmReleaseName(topo), Namespace: topo.Namespace}, &hr)
	switch {
	case apierrors.IsNotFound(err):
		// Already gone - fall through to the namespace sweep below.
	case err != nil:
		return ctrl.Result{}, fmt.Errorf("get HelmRelease: %w", err)
	default:
		if hr.DeletionTimestamp.IsZero() {
			if err := r.Delete(ctx, &hr); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, fmt.Errorf("delete HelmRelease: %w", err)
			}
		}
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}

	// Everything else - the AUTHENTIK_SECRET_KEY/postgres-password Secrets
	// ensureRandomSecret created, and (CNPG mode) the Cluster this controller
	// applies - lives in spec.namespace, a *different* namespace an
	// ownerReference can't reach (and, with clusterRef set, a different
	// cluster entirely), so deleting that namespace on whichever cluster it's
	// on is what cleans those up.
	if topo.Spec.Namespace != "" {
		rc, err := r.resolveClient(ctx, topo)
		if err != nil {
			// The remote cluster is unreachable - most likely its
			// WorkloadCluster was already torn down, taking spec.namespace
			// with it. Don't block finalizer removal forever on a cluster
			// that may never come back.
			log.Info("cluster unreachable during delete, skipping remote namespace cleanup", "clusterRef", topo.Spec.ClusterRef.Name, "error", err.Error())
		} else {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: topo.Spec.Namespace}}
			if err := rc.Delete(ctx, ns); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, fmt.Errorf("delete target namespace %q: %w", topo.Spec.Namespace, err)
			}
		}
	}
	controllerutil.RemoveFinalizer(topo, authentikTopologyFinalizer)
	return ctrl.Result{}, r.Update(ctx, topo)
}

// resolvePostgres implements all three Postgres modes, returning the
// connection info HelmValues needs (nil for Embedded once its password
// secret exists - the chart wires itself to its own bundled subchart) and
// whether the backend is ready for the authentik chart to be installed
// against. A false ready with a nil error means "still reconciling, check
// back later" (e.g. CNPG cluster not healthy yet), not a failure.
func (r *AuthentikTopologyReconciler) resolvePostgres(ctx context.Context, rc client.Client, topo *benchv1alpha1.AuthentikTopology) (*render.PostgresConnection, bool, error) {
	switch topo.Spec.Postgres.Mode {
	case benchv1alpha1.PostgresCNPG:
		return r.resolveCNPGPostgres(ctx, rc, topo)
	case benchv1alpha1.PostgresExternal:
		return r.resolveExternalPostgres(ctx, rc, topo)
	default: // PostgresEmbedded
		return r.resolveEmbeddedPostgres(ctx, rc, topo)
	}
}

func (r *AuthentikTopologyReconciler) resolveEmbeddedPostgres(ctx context.Context, rc client.Client, topo *benchv1alpha1.AuthentikTopology) (*render.PostgresConnection, bool, error) {
	// Pin the bundled postgresql subchart's own auth.password to a value we
	// generate, so it matches what we also hand authentik as
	// AUTHENTIK_POSTGRESQL__PASSWORD - the two are otherwise generated
	// independently and would never agree.
	pw, err := ensureRandomSecret(ctx, rc,
		types.NamespacedName{Name: topo.Name + "-authentik-postgres", Namespace: topo.Spec.Namespace},
		"password", 24)
	if err != nil {
		return nil, false, fmt.Errorf("ensure postgres password: %w", err)
	}
	setCondition(&topo.Status.Conditions, topo.Generation, benchv1alpha1.ConditionPostgresReady, metav1.ConditionTrue,
		benchv1alpha1.ReasonReady, "using the authentik chart's bundled postgresql subchart")
	return &render.PostgresConnection{Password: pw}, true, nil
}

// resolveCNPGPostgres applies the CNPG Cluster via rc (the management
// cluster when clusterRef is empty, otherwise a remote client) rather than
// applyOwned: a cross-cluster ownerReference is meaningless (the owning
// AuthentikTopology's UID doesn't exist in the remote cluster's etcd for its
// garbage collector to recognize), and cleanup already goes through
// reconcileDelete's namespace sweep on whichever cluster this is, so no
// finalizer-tracked per-object cleanup is needed either.
func (r *AuthentikTopologyReconciler) resolveCNPGPostgres(ctx context.Context, rc client.Client, topo *benchv1alpha1.AuthentikTopology) (*render.PostgresConnection, bool, error) {
	cluster := render.CNPGCluster(topo)
	if err := applyUnowned(ctx, rc, cluster); err != nil {
		return nil, false, fmt.Errorf("apply CNPG Cluster: %w", err)
	}
	topo.Status.CNPGClusterRef = &corev1.LocalObjectReference{Name: cluster.GetName()}

	existing := &unstructured.Unstructured{}
	existing.SetAPIVersion(cluster.GetAPIVersion())
	existing.SetKind(cluster.GetKind())
	if err := rc.Get(ctx, types.NamespacedName{Name: cluster.GetName(), Namespace: cluster.GetNamespace()}, existing); err != nil {
		return nil, false, fmt.Errorf("get CNPG Cluster: %w", err)
	}
	if !render.CNPGReady(existing) {
		setCondition(&topo.Status.Conditions, topo.Generation, benchv1alpha1.ConditionPostgresReady, metav1.ConditionFalse,
			benchv1alpha1.ReasonReconciling, "waiting for CNPG Cluster to become healthy")
		return nil, false, nil
	}

	secretName := render.CNPGAppSecretName(topo)
	var sec corev1.Secret
	if err := rc.Get(ctx, types.NamespacedName{Name: secretName, Namespace: topo.Spec.Namespace}, &sec); err != nil {
		if apierrors.IsNotFound(err) {
			setCondition(&topo.Status.Conditions, topo.Generation, benchv1alpha1.ConditionPostgresReady, metav1.ConditionFalse,
				benchv1alpha1.ReasonReconciling, "waiting for CNPG's generated app secret")
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("get CNPG app secret: %w", err)
	}
	setCondition(&topo.Status.Conditions, topo.Generation, benchv1alpha1.ConditionPostgresReady, metav1.ConditionTrue,
		benchv1alpha1.ReasonReady, "CNPG Cluster is healthy")
	return connectionFromSecret(sec, "host", "port", "dbname", "username", "password"), true, nil
}

func (r *AuthentikTopologyReconciler) resolveExternalPostgres(ctx context.Context, rc client.Client, topo *benchv1alpha1.AuthentikTopology) (*render.PostgresConnection, bool, error) {
	ext := topo.Spec.Postgres.External
	if ext == nil {
		return nil, false, fmt.Errorf("postgres mode External requires spec.postgres.external")
	}
	var sec corev1.Secret
	if err := rc.Get(ctx, types.NamespacedName{Name: ext.SecretRef.Name, Namespace: topo.Spec.Namespace}, &sec); err != nil {
		if apierrors.IsNotFound(err) {
			setCondition(&topo.Status.Conditions, topo.Generation, benchv1alpha1.ConditionPostgresReady, metav1.ConditionFalse,
				benchv1alpha1.ReasonDependencyNotReady, fmt.Sprintf("external postgres secret %q not found", ext.SecretRef.Name))
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("get external postgres secret: %w", err)
	}
	setCondition(&topo.Status.Conditions, topo.Generation, benchv1alpha1.ConditionPostgresReady, metav1.ConditionTrue,
		benchv1alpha1.ReasonReady, "using the referenced external postgres secret")
	return connectionFromSecret(sec, "host", "port", "database", "username", "password"), true, nil
}

// connectionFromSecret reads a PostgresConnection out of a Secret's data
// fields, keyed by name so callers can match CNPG's ("dbname") vs. the
// External contract's ("database") differing key for the database name.
func connectionFromSecret(sec corev1.Secret, hostKey, portKey, dbKey, userKey, passwordKey string) *render.PostgresConnection {
	port, _ := strconv.Atoi(string(sec.Data[portKey]))
	if port == 0 {
		port = 5432
	}
	return &render.PostgresConnection{
		Host:     string(sec.Data[hostKey]),
		Port:     int32(port),
		Database: string(sec.Data[dbKey]),
		User:     string(sec.Data[userKey]),
		Password: string(sec.Data[passwordKey]),
	}
}

func ensureNamespace(ctx context.Context, rc client.Client, name string) error {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	err := rc.Get(ctx, types.NamespacedName{Name: name}, ns)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	if err := rc.Create(ctx, ns); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AuthentikTopologyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&benchv1alpha1.AuthentikTopology{}).
		Owns(&helmv2.HelmRelease{}).
		// Without this, a topology whose extraValuesRef/fixtures blueprint
		// ConfigMap didn't exist yet only notices it was created on its own
		// next unrelated reconcile - the same staleness TestCaseReconciler's
		// AuthentikTopology watch fixes, just one hop earlier: fixturesCM's
		// creation (`make fixtures`, run any time after the topology
		// already exists) doesn't touch the topology at all on its own.
		Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(r.topologiesForConfigMap)).
		Named("authentiktopology").
		Complete(r)
}

// topologiesForConfigMap maps a changed ConfigMap to every AuthentikTopology
// in the same namespace that references it - by name, for the fixtures
// blueprint (a fixed name every topology in a namespace shares), or via
// spec.authentik.extraValuesRef for a topology-specific one.
func (r *AuthentikTopologyReconciler) topologiesForConfigMap(ctx context.Context, cm client.Object) []reconcile.Request {
	var list benchv1alpha1.AuthentikTopologyList
	if err := r.List(ctx, &list, client.InNamespace(cm.GetNamespace())); err != nil {
		return nil
	}
	var reqs []reconcile.Request
	for _, topo := range list.Items {
		ref := topo.Spec.Authentik.ExtraValuesRef
		if cm.GetName() == fixturesBlueprintConfigMapName || (ref != nil && ref.Name == cm.GetName()) {
			reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{Name: topo.Name, Namespace: topo.Namespace}})
		}
	}
	return reqs
}
