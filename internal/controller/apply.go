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

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
		obj.SetResourceVersion(existing.GetResourceVersion())
		return c.Update(ctx, obj)
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
