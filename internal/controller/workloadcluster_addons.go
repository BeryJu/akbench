package controller

import (
	"context"
	"fmt"
	"strings"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	benchv1alpha1 "github.com/authentik-labs/akbench/api/v1alpha1"
	"github.com/authentik-labs/akbench/internal/render"
)

// addonState works out an add-on's status after its ensure func ran: the
// ensure error decides it if there was one, otherwise the add-on's
// HelmRelease (when it installs through one) does, and a directly-applied
// add-on is Ready once applied.
func (r *WorkloadClusterReconciler) addonState(ctx context.Context, wc *benchv1alpha1.WorkloadCluster, helmRelease string, ensureErr error) (benchv1alpha1.AddonState, string) {
	if ensureErr != nil {
		if waiting, msg := transientAddonError(ensureErr); waiting {
			return benchv1alpha1.AddonWaiting, msg
		}
		return benchv1alpha1.AddonFailed, ensureErr.Error()
	}
	if helmRelease == "" {
		return benchv1alpha1.AddonReady, ""
	}

	var hr helmv2.HelmRelease
	err := r.Get(ctx, types.NamespacedName{Namespace: wc.Namespace, Name: helmRelease}, &hr)
	switch {
	case apierrors.IsNotFound(err):
		// The add-on decided there's nothing to install (e.g.
		// cluster-autoscaler without any autoscaled pool).
		return benchv1alpha1.AddonReady, "not needed"
	case err != nil:
		return benchv1alpha1.AddonFailed, fmt.Sprintf("get HelmRelease %s: %v", helmRelease, err)
	case !metav1.IsControlledBy(&hr, wc):
		return benchv1alpha1.AddonFailed, fmt.Sprintf("HelmRelease %s/%s belongs to another object", hr.Namespace, hr.Name)
	}
	ready := meta.FindStatusCondition(hr.Status.Conditions, "Ready")
	switch {
	case ready == nil || hr.Status.ObservedGeneration != hr.Generation:
		return benchv1alpha1.AddonProgressing, "waiting for helm-controller"
	case ready.Status == metav1.ConditionTrue:
		return benchv1alpha1.AddonReady, ""
	case ready.Status == metav1.ConditionFalse && isHelmFailureReason(ready.Reason):
		return benchv1alpha1.AddonFailed, ready.Message
	default:
		return benchv1alpha1.AddonProgressing, ready.Message
	}
}

// transientAddonError recognises ensure errors that resolve by themselves
// on a later reconcile, so they're reported as Waiting rather than Failed:
// a kind whose CRD another add-on hasn't installed yet (ServiceMonitor and
// PrometheusRule arrive with kube-prometheus-stack, which every
// observability add-on races on a fresh cluster), and optimistic-locking
// conflicts with helm-controller writing the same HelmRelease's status.
func transientAddonError(err error) (bool, string) {
	msg := err.Error()
	switch {
	case meta.IsNoMatchError(err) || strings.Contains(msg, "no matches for kind") ||
		strings.Contains(msg, "unable to retrieve the complete list of server APIs"):
		return true, "waiting for a CRD it needs to be installed on the cluster (" + msg + ")"
	case apierrors.IsConflict(err) || strings.Contains(msg, "the object has been modified"):
		return true, "retrying after a concurrent update"
	}
	return false, ""
}

// isHelmFailureReason reports a helm-controller Ready=False reason that
// means the release failed rather than is still in progress.
func isHelmFailureReason(reason string) bool {
	return strings.HasSuffix(reason, "Failed") || reason == "RetriesExceeded" || reason == "ArtifactFailed"
}

// setAddonStatus records one add-on's state, keeping lastTransitionTime when
// the state didn't change.
func setAddonStatus(wc *benchv1alpha1.WorkloadCluster, name string, state benchv1alpha1.AddonState, message string) {
	for i := range wc.Status.Addons {
		a := &wc.Status.Addons[i]
		if a.Name != name {
			continue
		}
		if a.State != state {
			a.LastTransitionTime = metav1.Now()
		}
		a.State, a.Message = state, message
		return
	}
	wc.Status.Addons = append(wc.Status.Addons, benchv1alpha1.AddonStatus{
		Name: name, State: state, Message: message, LastTransitionTime: metav1.Now(),
	})
}

// migrateLegacyAddonHelmRelease removes the HelmRelease an older operator
// created for one of wc's add-ons under a fixed name (e.g. "flannel"), now
// that each add-on's HelmRelease is named per cluster
// (render.AddonHelmReleaseName) - without uninstalling the Helm release it
// manages. The per-cluster HelmRelease keeps the same spec.releaseName and
// storage namespace, so helm-controller finds the existing release in
// storage, treats it as unmanaged and upgrades it in place rather than
// installing it again. The old object is suspended first: helm-controller
// only uninstalls a deleted HelmRelease's release when it isn't suspended.
// A fixed-name HelmRelease controlled by anything else (another cluster in
// the same namespace, from before the rename) is left alone.
func (r *WorkloadClusterReconciler) migrateLegacyAddonHelmRelease(ctx context.Context, wc *benchv1alpha1.WorkloadCluster, name string) error {
	if name == "" || name == render.AddonHelmReleaseName(wc, name) {
		return nil
	}
	var hr helmv2.HelmRelease
	err := r.Get(ctx, types.NamespacedName{Namespace: wc.Namespace, Name: name}, &hr)
	if apierrors.IsNotFound(err) || (err == nil && !metav1.IsControlledBy(&hr, wc)) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get legacy HelmRelease %s: %w", name, err)
	}
	if !hr.Spec.Suspend {
		patch := client.MergeFrom(hr.DeepCopy())
		hr.Spec.Suspend = true
		if err := r.Patch(ctx, &hr, patch); err != nil {
			return fmt.Errorf("suspend legacy HelmRelease %s: %w", name, err)
		}
	}
	if hr.DeletionTimestamp.IsZero() {
		if err := r.Delete(ctx, &hr); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete legacy HelmRelease %s: %w", name, err)
		}
		logf.FromContext(ctx).Info("Replaced legacy add-on HelmRelease with a per-cluster one, keeping its Helm release",
			"legacy", name, "new", render.AddonHelmReleaseName(wc, name))
	}
	return nil
}
