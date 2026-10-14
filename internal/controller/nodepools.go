package controller

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	benchv1alpha1 "github.com/authentik-labs/akbench/api/v1alpha1"
)

// resolveWorkloadCluster fetches the named WorkloadCluster from the
// management cluster (c) for node-pool placement (render.AuthentikPoolPlacement/
// render.K6PoolPlacement) - nil for an empty name (the management cluster
// itself, which this node-pool scheme doesn't apply to) or if the object
// can't be found. Best-effort: every caller only uses the result for a
// scheduling nicety, never something worth failing a reconcile over, so a
// lookup error here is logged, not returned.
func resolveWorkloadCluster(ctx context.Context, c client.Client, namespace, name string) *benchv1alpha1.WorkloadCluster {
	if name == "" {
		return nil
	}
	var wc benchv1alpha1.WorkloadCluster
	if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &wc); err != nil {
		if !apierrors.IsNotFound(err) {
			logf.FromContext(ctx).Error(err, "failed to resolve WorkloadCluster for node-pool placement", "name", name)
		}
		return nil
	}
	return &wc
}
