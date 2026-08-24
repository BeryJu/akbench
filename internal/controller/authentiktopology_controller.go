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
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
	"github.com/goauthentik/authentik-bench/internal/render"
)

// authentikTopologyFinalizer guards the remote (in later milestones,
// cross-cluster) objects an AuthentikTopology owns that Kubernetes garbage
// collection cannot reach on its own.
const authentikTopologyFinalizer = "bench.goauthentik.io/remote-cleanup"

// AuthentikTopologyReconciler reconciles an AuthentikTopology object.
//
// M1/M2 scope: operates only on the cluster the operator itself runs on -
// spec.clusterRef is rejected with a NotImplemented condition until the
// multicluster tracker lands in M4. All three Postgres modes (Embedded,
// CNPG, External) and both placement modes are implemented.
type AuthentikTopologyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=bench.goauthentik.io,resources=authentiktopologies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bench.goauthentik.io,resources=authentiktopologies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=bench.goauthentik.io,resources=authentiktopologies/finalizers,verbs=update
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,resources=helmreleases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=helmrepositories,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postgresql.cnpg.io,resources=clusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update

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

	if topo.Spec.ClusterRef.Name != "" {
		setCondition(&topo.Status.Conditions, topo.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
			benchv1alpha1.ReasonNotImplemented, "multi-cluster clusterRef is not implemented until M4; leave it unset to deploy on the management cluster")
		return ctrl.Result{}, r.Status().Update(ctx, &topo)
	}

	if err := r.ensureNamespace(ctx, topo.Spec.Namespace); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure namespace: %w", err)
	}

	pgConn, pgReady, err := r.resolvePostgres(ctx, &topo)
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
	if err := r.applyOwned(ctx, &topo, repo); err != nil {
		return ctrl.Result{}, fmt.Errorf("apply HelmRepository: %w", err)
	}

	// The chart does not generate AUTHENTIK_SECRET_KEY itself - authentik
	// refuses to start without one - so the operator generates and caches it
	// once, the same way the Ansible harness's credentials/ directory does.
	authentikSecretName := topo.Name + "-authentik-config"
	if _, err := ensureRandomSecret(ctx, r.Client,
		types.NamespacedName{Name: authentikSecretName, Namespace: topo.Spec.Namespace},
		"AUTHENTIK_SECRET_KEY", 32); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure authentik secret key: %w", err)
	}

	values, err := render.HelmValues(&topo, authentikSecretName, pgConn)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("build helm values: %w", err)
	}
	hr := render.HelmRelease(&topo, "", values)
	if err := r.applyOwned(ctx, &topo, hr); err != nil {
		return ctrl.Result{}, fmt.Errorf("apply HelmRelease: %w", err)
	}

	var existing helmv2.HelmRelease
	helmReleaseReady := metav1.ConditionFalse
	helmReleaseMessage := "waiting for helm-controller to reconcile"
	if err := r.Get(ctx, types.NamespacedName{Name: hr.Name, Namespace: hr.Namespace}, &existing); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("get HelmRelease: %w", err)
		}
	} else if cond := meta.FindStatusCondition(existing.Status.Conditions, "Ready"); cond != nil {
		if cond.Status == metav1.ConditionTrue {
			helmReleaseReady = metav1.ConditionTrue
			helmReleaseMessage = "HelmRelease is Ready"
		} else {
			helmReleaseMessage = cond.Message
		}
	}
	setCondition(&topo.Status.Conditions, topo.Generation, benchv1alpha1.ConditionHelmReleaseReady, helmReleaseReady,
		benchv1alpha1.ReasonReady, helmReleaseMessage)

	topo.Status.HelmReleaseRef = &corev1.LocalObjectReference{Name: hr.Name}
	topo.Status.ServiceEndpoint = render.ServiceEndpoint(&topo)
	topo.Status.ObservedGeneration = topo.Generation

	if helmReleaseReady == metav1.ConditionTrue {
		setCondition(&topo.Status.Conditions, topo.Generation, benchv1alpha1.ConditionReady, metav1.ConditionTrue,
			benchv1alpha1.ReasonReady, "authentik chart installed and ready")
	} else {
		setCondition(&topo.Status.Conditions, topo.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
			benchv1alpha1.ReasonReconciling, helmReleaseMessage)
	}

	if err := r.Status().Update(ctx, &topo); err != nil {
		return ctrl.Result{}, err
	}
	log.Info("reconciled AuthentikTopology", "ready", helmReleaseReady)
	return ctrl.Result{}, nil
}

func (r *AuthentikTopologyReconciler) reconcileDelete(ctx context.Context, topo *benchv1alpha1.AuthentikTopology) (ctrl.Result, error) {
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
	// owns - lives in spec.namespace, a *different* namespace an
	// ownerReference can't reach, so deleting that namespace is what cleans
	// those up. From M4 on, a remote topology's namespace needs the same
	// explicit treatment via the multicluster tracker.
	if topo.Spec.Namespace != "" {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: topo.Spec.Namespace}}
		if err := r.Delete(ctx, ns); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("delete target namespace %q: %w", topo.Spec.Namespace, err)
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
func (r *AuthentikTopologyReconciler) resolvePostgres(ctx context.Context, topo *benchv1alpha1.AuthentikTopology) (*render.PostgresConnection, bool, error) {
	switch topo.Spec.Postgres.Mode {
	case benchv1alpha1.PostgresCNPG:
		return r.resolveCNPGPostgres(ctx, topo)
	case benchv1alpha1.PostgresExternal:
		return r.resolveExternalPostgres(ctx, topo)
	default: // PostgresEmbedded
		return r.resolveEmbeddedPostgres(ctx, topo)
	}
}

func (r *AuthentikTopologyReconciler) resolveEmbeddedPostgres(ctx context.Context, topo *benchv1alpha1.AuthentikTopology) (*render.PostgresConnection, bool, error) {
	// Pin the bundled postgresql subchart's own auth.password to a value we
	// generate, so it matches what we also hand authentik as
	// AUTHENTIK_POSTGRESQL__PASSWORD - the two are otherwise generated
	// independently and would never agree.
	pw, err := ensureRandomSecret(ctx, r.Client,
		types.NamespacedName{Name: topo.Name + "-authentik-postgres", Namespace: topo.Spec.Namespace},
		"password", 24)
	if err != nil {
		return nil, false, fmt.Errorf("ensure postgres password: %w", err)
	}
	setCondition(&topo.Status.Conditions, topo.Generation, benchv1alpha1.ConditionPostgresReady, metav1.ConditionTrue,
		benchv1alpha1.ReasonReady, "using the authentik chart's bundled postgresql subchart")
	return &render.PostgresConnection{Password: pw}, true, nil
}

func (r *AuthentikTopologyReconciler) resolveCNPGPostgres(ctx context.Context, topo *benchv1alpha1.AuthentikTopology) (*render.PostgresConnection, bool, error) {
	cluster := render.CNPGCluster(topo)
	if err := r.applyOwned(ctx, topo, cluster); err != nil {
		return nil, false, fmt.Errorf("apply CNPG Cluster: %w", err)
	}
	topo.Status.CNPGClusterRef = &corev1.LocalObjectReference{Name: cluster.GetName()}

	existing := &unstructured.Unstructured{}
	existing.SetAPIVersion(cluster.GetAPIVersion())
	existing.SetKind(cluster.GetKind())
	if err := r.Get(ctx, types.NamespacedName{Name: cluster.GetName(), Namespace: cluster.GetNamespace()}, existing); err != nil {
		return nil, false, fmt.Errorf("get CNPG Cluster: %w", err)
	}
	if !render.CNPGReady(existing) {
		setCondition(&topo.Status.Conditions, topo.Generation, benchv1alpha1.ConditionPostgresReady, metav1.ConditionFalse,
			benchv1alpha1.ReasonReconciling, "waiting for CNPG Cluster to become healthy")
		return nil, false, nil
	}

	secretName := render.CNPGAppSecretName(topo)
	var sec corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: topo.Spec.Namespace}, &sec); err != nil {
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

func (r *AuthentikTopologyReconciler) resolveExternalPostgres(ctx context.Context, topo *benchv1alpha1.AuthentikTopology) (*render.PostgresConnection, bool, error) {
	ext := topo.Spec.Postgres.External
	if ext == nil {
		return nil, false, fmt.Errorf("postgres mode External requires spec.postgres.external")
	}
	var sec corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Name: ext.SecretRef.Name, Namespace: topo.Spec.Namespace}, &sec); err != nil {
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

func (r *AuthentikTopologyReconciler) ensureNamespace(ctx context.Context, name string) error {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	err := r.Get(ctx, types.NamespacedName{Name: name}, ns)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	if err := r.Create(ctx, ns); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// applyOwned creates or updates obj, setting topo as its controller owner so
// deleting the AuthentikTopology garbage-collects it (local-cluster objects
// only - see the finalizer note above for remote objects).
//
// Skips the Update entirely when the existing object's spec already matches:
// an unconditional Update here bumps resourceVersion on every reconcile even
// with no real change, which re-triggers this controller's own Owns() watch
// *and* restarts Flux's in-flight reconcile of the object before it can
// finish - the two controllers end up perpetually interrupting each other
// and the HelmRelease never completes an install.
func (r *AuthentikTopologyReconciler) applyOwned(ctx context.Context, topo *benchv1alpha1.AuthentikTopology, obj client.Object) error {
	if err := controllerutil.SetControllerReference(topo, obj, r.Scheme); err != nil {
		return err
	}
	if err := r.Create(ctx, obj); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
		existing := obj.DeepCopyObject().(client.Object)
		if err := r.Get(ctx, types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}, existing); err != nil {
			return err
		}
		if specEqual(existing, obj) {
			return nil
		}
		obj.SetResourceVersion(existing.GetResourceVersion())
		return r.Update(ctx, obj)
	}
	return nil
}

// specEqual compares the parts of existing/desired that applyOwned actually
// renders, for the kinds this controller owns. Unknown types return false
// (always update) rather than risk silently dropping a real change.
func specEqual(existing, desired client.Object) bool {
	switch d := desired.(type) {
	case *helmv2.HelmRelease:
		e, ok := existing.(*helmv2.HelmRelease)
		return ok && apiequality.Semantic.DeepEqual(e.Spec, d.Spec)
	case *sourcev1.HelmRepository:
		e, ok := existing.(*sourcev1.HelmRepository)
		return ok && apiequality.Semantic.DeepEqual(e.Spec, d.Spec)
	case *unstructured.Unstructured:
		e, ok := existing.(*unstructured.Unstructured)
		return ok && apiequality.Semantic.DeepEqual(e.Object["spec"], d.Object["spec"])
	default:
		return false
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *AuthentikTopologyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&benchv1alpha1.AuthentikTopology{}).
		Owns(&helmv2.HelmRelease{}).
		Named("authentiktopology").
		Complete(r)
}
