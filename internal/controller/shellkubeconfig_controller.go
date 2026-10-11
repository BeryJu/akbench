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
	"bytes"
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	capikubeconfig "sigs.k8s.io/cluster-api/util/kubeconfig"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
	"github.com/goauthentik/authentik-bench/internal/render"
)

// shellKubeconfigDataKey is the Secret key cmd/apiserver's
// --shell-kubeconfig-secret reads the kubeconfig from.
const shellKubeconfigDataKey = "config"

// ShellKubeconfigReconciler maintains one Secret (Namespace/SecretName)
// holding a kubeconfig that merges the management cluster's own context
// with every Ready WorkloadCluster's - see render.ShellKubeconfig. The
// in-browser kubectl shell (internal/api/shell.go) reads it fresh for every
// session, so a cluster that just became Ready is usable at once.
//
// This reconciler's own desired state doesn't depend on which specific
// WorkloadCluster triggered it - every reconcile recomputes the full
// membership from a fresh List, so which object req names is irrelevant.
type ShellKubeconfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// Namespace/SecretName name the Secret cmd/apiserver is configured to
	// read (BENCH_SHELL_KUBECONFIG_SECRET).
	Namespace  string
	SecretName string
}

// +kubebuilder:rbac:groups=bench.goauthentik.io,resources=workloadclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update

func (r *ShellKubeconfigReconciler) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var list benchv1alpha1.WorkloadClusterList
	if err := r.List(ctx, &list); err != nil {
		return ctrl.Result{}, fmt.Errorf("list workload clusters: %w", err)
	}

	var workloadClusters []render.WorkloadClusterKubeconfig
	for _, wc := range list.Items {
		ready := findCondition(wc.Status.Conditions, benchv1alpha1.ConditionReady)
		if ready == nil || ready.Status != metav1.ConditionTrue {
			continue
		}
		raw, err := capikubeconfig.FromSecret(ctx, r.Client, client.ObjectKey{Name: wc.Name, Namespace: wc.Namespace})
		if err != nil {
			// Ready but the kubeconfig Secret isn't there/readable yet is a
			// transient state (CAPI writes it moments after control-plane
			// init) - log and skip this one rather than failing the whole
			// merge over one cluster.
			log.Error(err, "get workload cluster kubeconfig, skipping", "workloadCluster", wc.Name)
			continue
		}
		workloadClusters = append(workloadClusters, render.WorkloadClusterKubeconfig{Name: wc.Name, Bytes: raw})
	}

	merged, err := render.ShellKubeconfig(workloadClusters)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("merge kubeconfigs: %w", err)
	}

	changed, err := r.applyKubeconfigSecret(ctx, merged)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("apply shell kubeconfig secret: %w", err)
	}
	log.Info("Reconciled shell kubeconfig Secret", "workloadClusters", len(workloadClusters), "changed", changed)
	return ctrl.Result{}, nil
}

// applyKubeconfigSecret is a plain get-or-create-then-update rather than
// this package's applyOwned/applyUnowned: those compare/copy a resource's
// .Spec (see apply.go's specEqual/copySpec), which Secret has no equivalent
// of, and unlike those, this Secret's Data is *meant* to change on every
// reconcile that observes different cluster membership. Returns whether the
// content actually changed.
func (r *ShellKubeconfigReconciler) applyKubeconfigSecret(ctx context.Context, kubeconfig []byte) (bool, error) {
	desired := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: r.SecretName, Namespace: r.Namespace},
		Data:       map[string][]byte{shellKubeconfigDataKey: kubeconfig},
	}
	if err := r.Create(ctx, desired); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return false, err
		}
		var existing corev1.Secret
		if err := r.Get(ctx, types.NamespacedName{Name: r.SecretName, Namespace: r.Namespace}, &existing); err != nil {
			return false, err
		}
		if bytes.Equal(existing.Data[shellKubeconfigDataKey], kubeconfig) {
			return false, nil
		}
		existing.Data = desired.Data
		return true, r.Update(ctx, &existing)
	}
	return true, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ShellKubeconfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&benchv1alpha1.WorkloadCluster{}).
		Named("shellkubeconfig").
		Complete(r)
}
