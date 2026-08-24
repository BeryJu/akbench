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
	"errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// errTopologyNotReady is returned by resolveReadyTopology when the topology
// exists but hasn't reached Ready yet - distinct from a lookup failure so
// callers can requeue/wait rather than treat it as an error.
var errTopologyNotReady = errors.New("topology not ready")

// resolveReadyTopology fetches the AuthentikTopology a TestCase/TestRun
// drives, requiring it to be Ready, and returns its target name and
// in-cluster BENCH_HOST/BENCH_PORT. Both TestCaseReconciler and
// TestRunReconciler share this so a topology's readiness contract is defined
// in exactly one place.
func resolveReadyTopology(ctx context.Context, c client.Client, namespace, name string) (topo *benchv1alpha1.AuthentikTopology, targetName, host string, port int32, err error) {
	topo = &benchv1alpha1.AuthentikTopology{}
	if err = c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, topo); err != nil {
		return nil, "", "", 0, err
	}
	ready := findCondition(topo.Status.Conditions, benchv1alpha1.ConditionReady)
	if ready == nil || ready.Status != metav1.ConditionTrue {
		return topo, "", "", 0, errTopologyNotReady
	}
	targetName = topo.Spec.TargetName
	if targetName == "" {
		targetName = topo.Name
	}
	host, port, err = splitServiceEndpoint(topo.Status.ServiceEndpoint)
	if err != nil {
		return topo, targetName, "", 0, err
	}
	return topo, targetName, host, port, nil
}
