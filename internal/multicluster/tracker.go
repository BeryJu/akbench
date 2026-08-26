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

// Package multicluster resolves a bench.goauthentik.io clusterRef/
// runnerClusterRef (a cluster.x-k8s.io Cluster name) to a live client for
// that workload cluster, so AuthentikTopology/TestCase/TestRun can deploy
// onto any Cluster API-provisioned cluster and not just the management
// cluster the operator itself runs on.
package multicluster

import (
	"context"

	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	"sigs.k8s.io/cluster-api/controllers/clustercache"
)

// ErrClusterNotConnected means the named Cluster exists but the operator
// doesn't have a live connection to it yet (still provisioning, control
// plane unreachable, kubeconfig Secret not written yet...). Callers should
// treat this as "requeue and check back later", not a fatal error.
var ErrClusterNotConnected = clustercache.ErrClusterNotConnected

// Tracker wraps Cluster API's ClusterCache: it watches every cluster.x-k8s.io
// Cluster cluster-wide and, for each one, maintains (creating, health
// checking, and tearing down as needed) a client built from the
// "<cluster-name>-kubeconfig" Secret convention CAPI itself defines - the
// same Secret WorkloadClusterReconciler's provisioning produces once a
// Cluster's control plane is initialized.
type Tracker struct {
	cache clustercache.ClusterCache
}

// SetupWithManager creates the underlying ClusterCache and registers its
// controller with mgr. Must be called before any reconciler that uses the
// returned Tracker starts running.
func SetupWithManager(ctx context.Context, mgr ctrl.Manager) (*Tracker, error) {
	cc, err := clustercache.SetupWithManager(ctx, mgr, clustercache.Options{
		// Ideally this would be a client scoped to just kubeconfig Secrets
		// (see the Options docstring) - deferred until this actually needs
		// to scale beyond a handful of workload clusters.
		SecretClient: mgr.GetClient(),
		Client:       clustercache.ClientOptions{UserAgent: "authentik-bench"},
	}, controller.Options{MaxConcurrentReconciles: 5})
	if err != nil {
		return nil, err
	}
	return &Tracker{cache: cc}, nil
}

// GetClient returns a live client for the named Cluster (namespace/name of
// the cluster.x-k8s.io Cluster object, which by convention is the same
// namespace as the bench.goauthentik.io object naming it via clusterRef).
func (t *Tracker) GetClient(ctx context.Context, cluster client.ObjectKey) (client.Client, error) {
	return t.cache.GetClient(ctx, cluster)
}

// GetRESTConfig returns a raw REST config for the named Cluster - for
// callers that need something a controller-runtime client.Client can't do
// (pod exec, in this operator's case), not for general reads/writes.
func (t *Tracker) GetRESTConfig(ctx context.Context, cluster client.ObjectKey) (*rest.Config, error) {
	return t.cache.GetRESTConfig(ctx, cluster)
}
