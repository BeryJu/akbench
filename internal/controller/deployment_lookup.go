package controller

import (
	"context"
	"errors"
	"fmt"
	"slices"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	benchv1alpha1 "github.com/authentik-labs/akbench/api/v1alpha1"
	"github.com/authentik-labs/akbench/internal/multicluster"
)

// errDeploymentNotReady is returned by resolveReadyDeployment when the deployment
// exists but hasn't reached Ready yet - distinct from a lookup failure so
// callers can requeue/wait rather than treat it as an error.
var errDeploymentNotReady = errors.New("deployment not ready")

// resolveReadyDeployment fetches the AuthentikDeployment a TestCase/TestRun
// drives, requiring it to be Ready, and returns its target name and
// in-cluster BENCH_HOST/BENCH_PORT. Both TestCaseReconciler and
// TestRunReconciler share this so a deployment's readiness contract is defined
// in exactly one place.
func resolveReadyDeployment(ctx context.Context, c client.Client, namespace, name string) (topo *benchv1alpha1.AuthentikDeployment, targetName, host string, port int32, err error) {
	topo = &benchv1alpha1.AuthentikDeployment{}
	if err = c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, topo); err != nil {
		return nil, "", "", 0, err
	}
	ready := findCondition(topo.Status.Conditions, benchv1alpha1.ConditionReady)
	if ready == nil || ready.Status != metav1.ConditionTrue {
		return topo, "", "", 0, errDeploymentNotReady
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

// resolveRunnerClient returns the client TestCaseReconciler/TestRunReconciler
// should use for the k6 workload itself: runnerClusterRef if set, else the
// deployment's own clusterRef, else local. The bool result is true when this
// is a different (remote) cluster than local - callers should skip
// ownerReferences (meaningless across clusters) and rely on periodic
// requeues rather than a local-only Owns() watch.
func resolveRunnerClient(ctx context.Context, tracker *multicluster.Tracker, local client.Client, namespace string, runnerClusterRef *corev1.LocalObjectReference, topoClusterRef corev1.LocalObjectReference) (client.Client, bool, error) {
	name := topoClusterRef.Name
	if runnerClusterRef != nil && runnerClusterRef.Name != "" {
		name = runnerClusterRef.Name
	}
	if name == "" {
		return local, false, nil
	}
	if tracker == nil {
		return nil, true, fmt.Errorf("multicluster tracker not configured")
	}
	rc, err := tracker.GetClient(ctx, client.ObjectKey{Namespace: namespace, Name: name})
	return rc, true, err
}

// resolvedRunDeploymentRef picks which of a TestCase's Deployments() a
// particular TestRun targets: runSpec.DeploymentRef if set (every TestRun
// created once a TestCase could drive more than one deployment - see
// internal/api/runs.go's createRun, which always sets it explicitly), else
// the first entry of tcSpec's own resolved deployments (every TestRun
// created before that, back when a TestCase only ever had one). Takes a
// bare *TestCaseSpec rather than a full TestCase so it works equally from a
// live TestCase (tc.Spec) and a TestRun's own audit snapshot
// (run.Status.ResolvedSpec, which has no surrounding TestCase object).
//
// A pinned deploymentRef must be one of the TestCase's own deployments: the
// TestCase only provisions per-target dependencies (e.g. oidc-test-client)
// for those, and a run against anything else wouldn't be that test's data.
func resolvedRunDeploymentRef(runSpec *benchv1alpha1.TestRunSpec, tcSpec *benchv1alpha1.TestCaseSpec) (corev1.LocalObjectReference, error) {
	refs := tcSpec.Deployments()
	if runSpec.DeploymentRef != nil && runSpec.DeploymentRef.Name != "" {
		if !slices.ContainsFunc(refs, func(ref corev1.LocalObjectReference) bool { return ref.Name == runSpec.DeploymentRef.Name }) {
			return corev1.LocalObjectReference{}, fmt.Errorf("deploymentRef %q is not one of the TestCase's deployments", runSpec.DeploymentRef.Name)
		}
		return *runSpec.DeploymentRef, nil
	}
	if len(refs) > 0 {
		return refs[0], nil
	}
	return corev1.LocalObjectReference{}, fmt.Errorf("TestCase has no deployment set")
}

// resolveTemplate fetches the TestCaseTemplate a TestCase's spec.template
// names, defaulting an empty name to DefaultTemplateName (in case this is
// reached before CRD admission defaulting - e.g. a TestCase built by hand in
// a test). Templates live in the same namespace as the TestCase itself (the
// management cluster's own bench namespace, never the runner cluster), so
// this is always a local Get, unlike resolveReadyDeployment/
// resolveRunnerClient which may cross into a workload cluster. Shared by
// TestCaseReconciler and TestRunReconciler so a template's resolution
// contract is defined in exactly one place.
func resolveTemplate(ctx context.Context, c client.Client, namespace, name string) (*benchv1alpha1.TestCaseTemplate, error) {
	if name == "" {
		name = DefaultTemplateName
	}
	var tmpl benchv1alpha1.TestCaseTemplate
	if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &tmpl); err != nil {
		return nil, err
	}
	return &tmpl, nil
}
