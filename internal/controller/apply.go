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
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	bootstrapv1 "sigs.k8s.io/cluster-api/api/bootstrap/kubeadm/v1beta2"
	controlplanev1 "sigs.k8s.io/cluster-api/api/controlplane/kubeadm/v1beta2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
)

// applyOwned creates or updates obj, setting owner as its controller owner so
// deleting the owner garbage-collects it (local-cluster objects only - a
// remote/cross-cluster object needs a finalizer plus explicit cleanup
// instead, since an ownerReference can't reach across clusters).
//
// Skips the Update entirely when the existing object's spec already matches:
// an unconditional Update here bumps resourceVersion on every reconcile even
// with no real change, which re-triggers this controller's own Owns() watch
// *and* restarts the owning system's (Flux's, CAPI's) in-flight reconcile of
// the object before it can finish - verified against a live HelmRelease that
// never completed an install until this became conditional.
//
// When an Update is needed, it's applied to the fetched existing object
// (only its spec overwritten) rather than replacing existing wholesale with
// our freshly rendered obj: a full-object Update using obj would wipe
// metadata.finalizers, since our rendered copy never has one set. Doing so
// raced helm-controller's own finalizer-add-then-requeue step on a live
// HelmRelease - whichever of the two writes landed last won, so the
// finalizer sometimes got stripped right back off moments after
// helm-controller added it, and every subsequent helm-controller reconcile
// just re-added it and requeued forever, never reaching the point where it
// sets any status condition at all.
func applyOwned(ctx context.Context, c client.Client, scheme *runtime.Scheme, owner client.Object, obj client.Object) error {
	if err := controllerutil.SetControllerReference(owner, obj, scheme); err != nil {
		return err
	}
	if err := c.Create(ctx, obj); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
		existing := obj.DeepCopyObject().(client.Object)
		if err := c.Get(ctx, types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}, existing); err != nil {
			return err
		}
		if specEqual(existing, obj) {
			return nil
		}
		// existing already carries a valid owner ref from whoever created it
		// (this function always sets one on obj before the initial Create) -
		// re-asserting it here would error out when existing is a shared
		// object (e.g. one HelmRepository per namespace, reused by every
		// AuthentikTopology in it) already owned by a different owner.
		return updateSpecWithConflictRetry(ctx, c, types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}, existing, obj)
	}
	return nil
}

// updateSpecWithConflictRetry copies obj's spec onto existing and updates it,
// retrying with a fresh Get on 409 Conflict. existing's own status subresource
// is written separately from a plain Update, but its resourceVersion still
// advances on every status write - and a remote controller actively working
// this object (e.g. helm-controller mid-install) can write status often
// enough that the resourceVersion this function's caller fetched existing at
// is already stale by the time Update runs, well before any real conflict in
// what either side is trying to change.
func updateSpecWithConflictRetry(ctx context.Context, c client.Client, key types.NamespacedName, existing, obj client.Object) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := copySpec(existing, obj); err != nil {
			return err
		}
		err := c.Update(ctx, existing)
		if apierrors.IsConflict(err) {
			if getErr := c.Get(ctx, key, existing); getErr != nil {
				return getErr
			}
		}
		return err
	})
}

// applyCreateOnly creates obj (setting owner as its controller owner) and
// does nothing if it already exists - never updates it. Cluster API's own
// webhooks/defaulting mutate a Cluster/KubeadmControlPlane/MachineDeployment/
// LXCCluster's spec after creation (rollout strategy defaults, etc.) in ways
// this operator's freshly-rendered "desired" object never has, so comparing
// specs the way applyOwned does for our own HelmRelease/HelmRepository kinds
// always finds a difference - verified live: that made
// WorkloadClusterReconciler update every reconcile, each Update re-triggering
// its own Owns() watch, in an infinite loop racing CAPI's own controllers for
// the same objects. Provisioning is create-once for now; day-2 resize/
// upgrade of an existing WorkloadCluster is not yet supported.
func applyCreateOnly(ctx context.Context, c client.Client, scheme *runtime.Scheme, owner client.Object, obj client.Object) error {
	if err := controllerutil.SetControllerReference(owner, obj, scheme); err != nil {
		return err
	}
	if err := c.Create(ctx, obj); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// applyMachineDeploymentPool creates a worker pool's MachineDeployment if it
// doesn't exist yet, or - if it does - patches only spec.replicas and
// spec.template.spec.infrastructureRef.name (the two fields a
// WorkerPoolSpec edit can actually change) rather than comparing/replacing
// the whole spec the way applyOwned does. A full-spec comparison would hit
// the same infinite-reconcile bug applyCreateOnly's doc comment describes -
// CAPI mutates other spec fields (rollout strategy defaults, etc.) after
// creation - so this only ever touches the two fields this operator itself
// owns, leaving everything CAPI has defaulted alone.
func applyMachineDeploymentPool(ctx context.Context, c client.Client, scheme *runtime.Scheme, owner client.Object, desired *clusterv1.MachineDeployment) error {
	if err := controllerutil.SetControllerReference(owner, desired, scheme); err != nil {
		return err
	}
	var existing clusterv1.MachineDeployment
	err := c.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, &existing)
	if apierrors.IsNotFound(err) {
		return c.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	changed := false
	if existing.Spec.Replicas == nil || desired.Spec.Replicas == nil || *existing.Spec.Replicas != *desired.Spec.Replicas {
		existing.Spec.Replicas = desired.Spec.Replicas
		changed = true
	}
	if existing.Spec.Template.Spec.InfrastructureRef.Name != desired.Spec.Template.Spec.InfrastructureRef.Name {
		existing.Spec.Template.Spec.InfrastructureRef.Name = desired.Spec.Template.Spec.InfrastructureRef.Name
		changed = true
	}
	if !changed {
		return nil
	}
	return c.Update(ctx, &existing)
}

// applyUnowned is applyOwned without the ownerReference: for objects applied
// to a client that may be a different cluster than the owner lives on,
// where an ownerReference would carry a UID the remote cluster's garbage
// collector can never recognize. Callers are responsible for their own
// cleanup (typically: delete the namespace this object lives in).
//
// Like applyOwned, an Update is applied to the fetched existing object
// rather than replacing it wholesale with obj, so metadata a remote
// controller added (e.g. CNPG's own finalizer on the Cluster it manages)
// survives instead of being raced off on every spec-changing reconcile.
func applyUnowned(ctx context.Context, c client.Client, obj client.Object) error {
	if err := c.Create(ctx, obj); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
		existing := obj.DeepCopyObject().(client.Object)
		if err := c.Get(ctx, types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}, existing); err != nil {
			return err
		}
		if specEqual(existing, obj) {
			return nil
		}
		return updateSpecWithConflictRetry(ctx, c, types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}, existing, obj)
	}
	return nil
}

// specEqual compares the parts of existing/desired that applyOwned actually
// renders, for every kind any controller in this package owns. Unknown types
// return false (always update) rather than risk silently dropping a real
// change.
func specEqual(existing, desired client.Object) bool {
	switch d := desired.(type) {
	case *helmv2.HelmRelease:
		e, ok := existing.(*helmv2.HelmRelease)
		return ok && apiequality.Semantic.DeepEqual(e.Spec, d.Spec)
	case *sourcev1.HelmRepository:
		e, ok := existing.(*sourcev1.HelmRepository)
		return ok && apiequality.Semantic.DeepEqual(e.Spec, d.Spec)
	case *clusterv1.Cluster:
		e, ok := existing.(*clusterv1.Cluster)
		return ok && apiequality.Semantic.DeepEqual(e.Spec, d.Spec)
	case *clusterv1.MachineDeployment:
		e, ok := existing.(*clusterv1.MachineDeployment)
		return ok && apiequality.Semantic.DeepEqual(e.Spec, d.Spec)
	case *controlplanev1.KubeadmControlPlane:
		e, ok := existing.(*controlplanev1.KubeadmControlPlane)
		return ok && apiequality.Semantic.DeepEqual(e.Spec, d.Spec)
	case *bootstrapv1.KubeadmConfigTemplate:
		e, ok := existing.(*bootstrapv1.KubeadmConfigTemplate)
		return ok && apiequality.Semantic.DeepEqual(e.Spec, d.Spec)
	case *unstructured.Unstructured:
		e, ok := existing.(*unstructured.Unstructured)
		return ok && apiequality.Semantic.DeepEqual(e.Object["spec"], d.Object["spec"])
	default:
		return false
	}
}

// copySpec overwrites existing's spec with desired's, leaving the rest of
// existing (most importantly metadata.finalizers, which desired never has
// set) untouched.
func copySpec(existing, desired client.Object) error {
	switch d := desired.(type) {
	case *helmv2.HelmRelease:
		e, ok := existing.(*helmv2.HelmRelease)
		if !ok {
			return fmt.Errorf("copySpec: existing is %T, want *helmv2.HelmRelease", existing)
		}
		e.Spec = d.Spec
	case *sourcev1.HelmRepository:
		e, ok := existing.(*sourcev1.HelmRepository)
		if !ok {
			return fmt.Errorf("copySpec: existing is %T, want *sourcev1.HelmRepository", existing)
		}
		e.Spec = d.Spec
	case *clusterv1.Cluster:
		e, ok := existing.(*clusterv1.Cluster)
		if !ok {
			return fmt.Errorf("copySpec: existing is %T, want *clusterv1.Cluster", existing)
		}
		e.Spec = d.Spec
	case *clusterv1.MachineDeployment:
		e, ok := existing.(*clusterv1.MachineDeployment)
		if !ok {
			return fmt.Errorf("copySpec: existing is %T, want *clusterv1.MachineDeployment", existing)
		}
		e.Spec = d.Spec
	case *controlplanev1.KubeadmControlPlane:
		e, ok := existing.(*controlplanev1.KubeadmControlPlane)
		if !ok {
			return fmt.Errorf("copySpec: existing is %T, want *controlplanev1.KubeadmControlPlane", existing)
		}
		e.Spec = d.Spec
	case *bootstrapv1.KubeadmConfigTemplate:
		e, ok := existing.(*bootstrapv1.KubeadmConfigTemplate)
		if !ok {
			return fmt.Errorf("copySpec: existing is %T, want *bootstrapv1.KubeadmConfigTemplate", existing)
		}
		e.Spec = d.Spec
	case *unstructured.Unstructured:
		e, ok := existing.(*unstructured.Unstructured)
		if !ok {
			return fmt.Errorf("copySpec: existing is %T, want *unstructured.Unstructured", existing)
		}
		e.Object["spec"] = d.Object["spec"]
	default:
		return fmt.Errorf("copySpec: unsupported type %T", desired)
	}
	return nil
}
