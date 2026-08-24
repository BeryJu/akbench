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

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
// M1 scope: operates only on the cluster the operator itself runs on
// (spec.clusterRef is rejected with a NotImplemented condition until the
// multicluster tracker lands in M4) and only supports Postgres mode
// Embedded (CNPG/External are rejected the same way until M2).
type AuthentikTopologyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=bench.goauthentik.io,resources=authentiktopologies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bench.goauthentik.io,resources=authentiktopologies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=bench.goauthentik.io,resources=authentiktopologies/finalizers,verbs=update
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,resources=helmreleases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=helmrepositories,verbs=get;list;watch;create;update;patch;delete
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

	if topo.Spec.Postgres.Mode != benchv1alpha1.PostgresEmbedded {
		setCondition(&topo.Status.Conditions, topo.Generation, benchv1alpha1.ConditionPostgresReady, metav1.ConditionFalse,
			benchv1alpha1.ReasonNotImplemented, fmt.Sprintf("postgres mode %s lands in M2; use Embedded for now", topo.Spec.Postgres.Mode))
		setCondition(&topo.Status.Conditions, topo.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
			benchv1alpha1.ReasonNotImplemented, "waiting on postgres mode support")
		return ctrl.Result{}, r.Status().Update(ctx, &topo)
	}
	setCondition(&topo.Status.Conditions, topo.Generation, benchv1alpha1.ConditionPostgresReady, metav1.ConditionTrue,
		benchv1alpha1.ReasonReady, "using the authentik chart's bundled postgresql subchart")

	if err := r.ensureNamespace(ctx, topo.Spec.Namespace); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure namespace: %w", err)
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

	var pgConn *render.PostgresConnection
	if topo.Spec.Postgres.Mode == benchv1alpha1.PostgresEmbedded {
		// Pin the bundled postgresql subchart's own auth.password to a value
		// we generate, so it matches what we also hand authentik as
		// AUTHENTIK_POSTGRESQL__PASSWORD - the two are otherwise generated
		// independently and would never agree.
		pw, err := ensureRandomSecret(ctx, r.Client,
			types.NamespacedName{Name: topo.Name + "-authentik-postgres", Namespace: topo.Spec.Namespace},
			"password", 24)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("ensure postgres password: %w", err)
		}
		pgConn = &render.PostgresConnection{Password: pw}
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
	// The HelmRelease/HelmRepository this controller owns live beside the
	// AuthentikTopology CR and are garbage-collected via ownerReferences
	// (applyOwned). Everything else - the chart's rendered workloads, and the
	// AUTHENTIK_SECRET_KEY/postgres-password Secrets ensureRandomSecret
	// created - lives in spec.namespace, a *different* namespace an
	// ownerReference can't reach, so deleting that namespace is what actually
	// cleans it up. From M2/M4 on, a remote CNPG Cluster on another cluster
	// needs the same explicit treatment via the multicluster tracker.
	if topo.Spec.Namespace != "" {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: topo.Spec.Namespace}}
		if err := r.Delete(ctx, ns); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("delete target namespace %q: %w", topo.Spec.Namespace, err)
		}
	}
	controllerutil.RemoveFinalizer(topo, authentikTopologyFinalizer)
	return ctrl.Result{}, r.Update(ctx, topo)
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
// renders, for the two Flux kinds this controller owns. Unknown types return
// false (always update) rather than risk silently dropping a real change.
func specEqual(existing, desired client.Object) bool {
	switch d := desired.(type) {
	case *helmv2.HelmRelease:
		e, ok := existing.(*helmv2.HelmRelease)
		return ok && apiequality.Semantic.DeepEqual(e.Spec, d.Spec)
	case *sourcev1.HelmRepository:
		e, ok := existing.(*sourcev1.HelmRepository)
		return ok && apiequality.Semantic.DeepEqual(e.Spec, d.Spec)
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
