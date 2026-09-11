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
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
	"github.com/goauthentik/authentik-bench/internal/grafana"
	"github.com/goauthentik/authentik-bench/internal/multicluster"
	"github.com/goauthentik/authentik-bench/internal/render"
)

const testRunFinalizer = "bench.goauthentik.io/remote-cleanup"

// TestRunReconciler executes one bounded (Quick) run of a TestCase.
//
// The Job runs on the same cluster TestCaseReconciler resolves for its
// TestCase (spec.runnerClusterRef, else the topology's own clusterRef, else
// local), resolved fresh via Tracker. Grafana annotations are posted only
// when internal/grafana.FromEnv reports Enabled().
type TestRunReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Grafana  *grafana.Client
	Tracker  *multicluster.Tracker
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=bench.goauthentik.io,resources=testruns,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bench.goauthentik.io,resources=testruns/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=bench.goauthentik.io,resources=testruns/finalizers,verbs=update
// +kubebuilder:rbac:groups=bench.goauthentik.io,resources=testcases,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete

func (r *TestRunReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var run benchv1alpha1.TestRun
	if err := r.Get(ctx, req.NamespacedName, &run); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !run.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &run)
	}
	// The finalizer and the test-case label are both metadata the run needs
	// before anything else happens, so they go on in one update: the label is
	// what makes `kubectl get testruns -l bench.goauthentik.io/testcase=<name>`
	// a run history, and a TestRun created by hand would otherwise not have it.
	needsFinalizer := !controllerutil.ContainsFinalizer(&run, testRunFinalizer)
	needsLabel := run.Labels[benchv1alpha1.LabelTestCase] != run.Spec.TestCaseRef.Name
	if needsFinalizer || needsLabel {
		controllerutil.AddFinalizer(&run, testRunFinalizer)
		if run.Labels == nil {
			run.Labels = map[string]string{}
		}
		run.Labels[benchv1alpha1.LabelTestCase] = run.Spec.TestCaseRef.Name
		if err := r.Update(ctx, &run); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Terminal phases are immutable audit records - re-reconciling a
	// Succeeded/Failed TestRun would re-annotate Grafana and re-check a Job
	// that has already been accounted for.
	if run.Status.Phase == benchv1alpha1.TestRunSucceeded || run.Status.Phase == benchv1alpha1.TestRunFailed {
		return ctrl.Result{}, nil
	}

	if run.Status.JobRef == nil {
		return r.start(ctx, &run)
	}
	return r.pollJob(ctx, &run)
}

func (r *TestRunReconciler) start(ctx context.Context, run *benchv1alpha1.TestRun) (ctrl.Result, error) {
	var tc benchv1alpha1.TestCase
	if err := r.Get(ctx, types.NamespacedName{Name: run.Spec.TestCaseRef.Name, Namespace: run.Namespace}, &tc); err != nil {
		if apierrors.IsNotFound(err) {
			setCondition(r.Recorder, run, &run.Status.Conditions, run.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
				benchv1alpha1.ReasonDependencyNotReady, fmt.Sprintf("TestCase %q not found", run.Spec.TestCaseRef.Name))
			return ctrl.Result{}, r.Status().Update(ctx, run)
		}
		return ctrl.Result{}, err
	}
	if tc.Spec.Profile != benchv1alpha1.ProfileQuick {
		setCondition(r.Recorder, run, &run.Status.Conditions, run.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
			benchv1alpha1.ReasonReconcileError, "TestRuns only execute Quick TestCases; Burn TestCases run continuously via their own Deployment")
		run.Status.Phase = benchv1alpha1.TestRunFailed
		return ctrl.Result{}, r.Status().Update(ctx, run)
	}

	resolved := tc.Spec.DeepCopy()
	applyOverrides(resolved, run.Spec.Overrides)
	resolvedTC := tc.DeepCopy()
	resolvedTC.Spec = *resolved

	topo, targetName, host, port, err := resolveReadyTopology(ctx, r.Client, tc.Namespace, tc.Spec.TopologyRef.Name)
	if err != nil {
		if errors.Is(err, errTopologyNotReady) {
			setCondition(r.Recorder, run, &run.Status.Conditions, run.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
				benchv1alpha1.ReasonDependencyNotReady, "AuthentikTopology is not Ready yet")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, r.Status().Update(ctx, run)
		}
		return ctrl.Result{}, fmt.Errorf("resolve topology: %w", err)
	}

	rc, remote, err := resolveRunnerClient(ctx, r.Tracker, r.Client, run.Namespace, tc.Spec.RunnerClusterRef, topo.Spec.ClusterRef)
	if err != nil {
		setCondition(r.Recorder, run, &run.Status.Conditions, run.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
			benchv1alpha1.ReasonDependencyNotReady, fmt.Sprintf("waiting for runner cluster to be reachable: %v", err))
		return ctrl.Result{RequeueAfter: 10 * time.Second}, r.Status().Update(ctx, run)
	}
	if remote {
		if err := ensureNamespace(ctx, rc, run.Namespace); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensure runner namespace: %w", err)
		}
	}

	scriptCMName := "k6-script-" + tc.Name
	if tc.Spec.ScriptConfigMapRef != nil {
		scriptCMName = tc.Spec.ScriptConfigMapRef.Name
	}

	params := render.K6WorkloadParams{
		TestCase:            resolvedTC,
		TargetName:          targetName,
		BenchHost:           host,
		BenchPort:           port,
		RemoteWriteURL:      defaultRemoteWriteURL,
		ScriptConfigMapName: scriptCMName,
		FixturesSecretName:  fixturesSecretName,
	}
	jobName := "testrun-" + run.Name
	job := render.QuickJob(jobName, run.Namespace, params)
	// A cross-cluster ownerReference would carry a UID the remote cluster's
	// garbage collector can never recognize, so remote cleanup instead goes
	// through the finalizer-driven reconcileDelete below.
	if !remote {
		if err := controllerutil.SetControllerReference(run, job, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
	}
	if err := rc.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) {
		return ctrl.Result{}, fmt.Errorf("create quick job: %w", err)
	}

	now := metav1.Now()
	run.Status.JobRef = &corev1.LocalObjectReference{Name: jobName}
	run.Status.StartTime = &now
	run.Status.Phase = benchv1alpha1.TestRunRunning
	run.Status.ResolvedSpec = resolved
	setCondition(r.Recorder, run, &run.Status.Conditions, run.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
		benchv1alpha1.ReasonReconciling, "quick job started")

	if r.Grafana.Enabled() {
		text := fmt.Sprintf("quick test %s against %s: %d VUs x %d iterations", tc.Name, targetName, resolved.VUs, resolved.Iterations)
		id, err := r.Grafana.Annotate(ctx, now.Time, time.Time{}, []string{"k6", "quick", "test:" + tc.Name, "target:" + targetName}, text)
		if err != nil {
			logf.FromContext(ctx).Error(err, "failed to post Grafana start annotation")
		} else if id != 0 {
			run.Status.GrafanaAnnotationID = &id
		}
	}

	return ctrl.Result{RequeueAfter: 2 * time.Second}, r.Status().Update(ctx, run)
}

// resolveRunRunnerClient re-derives the cluster a TestRun's Job runs on from
// its own status.resolvedSpec (captured at start time), so pollJob/
// reconcileDelete don't need to re-fetch the live TestCase, which may have
// since changed or been deleted.
func (r *TestRunReconciler) resolveRunRunnerClient(ctx context.Context, run *benchv1alpha1.TestRun) (client.Client, error) {
	if run.Status.ResolvedSpec == nil {
		return r.Client, nil
	}
	var topo benchv1alpha1.AuthentikTopology
	var topoClusterRef corev1.LocalObjectReference
	if err := r.Get(ctx, types.NamespacedName{Name: run.Status.ResolvedSpec.TopologyRef.Name, Namespace: run.Namespace}, &topo); err == nil {
		topoClusterRef = topo.Spec.ClusterRef
	}
	rc, _, err := resolveRunnerClient(ctx, r.Tracker, r.Client, run.Namespace, run.Status.ResolvedSpec.RunnerClusterRef, topoClusterRef)
	return rc, err
}

func (r *TestRunReconciler) pollJob(ctx context.Context, run *benchv1alpha1.TestRun) (ctrl.Result, error) {
	rc, err := r.resolveRunRunnerClient(ctx, run)
	if err != nil {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	var job batchv1.Job
	if err := rc.Get(ctx, types.NamespacedName{Name: run.Status.JobRef.Name, Namespace: run.Namespace}, &job); err != nil {
		if apierrors.IsNotFound(err) {
			run.Status.Phase = benchv1alpha1.TestRunFailed
			setCondition(r.Recorder, run, &run.Status.Conditions, run.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
				benchv1alpha1.ReasonReconcileError, "job disappeared before completing")
			return ctrl.Result{}, r.Status().Update(ctx, run)
		}
		return ctrl.Result{}, err
	}

	switch {
	case job.Status.Succeeded > 0:
		return ctrl.Result{}, r.finish(ctx, run, benchv1alpha1.TestRunSucceeded, "quick job completed")
	case job.Status.Failed > 0:
		return ctrl.Result{}, r.finish(ctx, run, benchv1alpha1.TestRunFailed, "quick job failed")
	default:
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}
}

func (r *TestRunReconciler) finish(ctx context.Context, run *benchv1alpha1.TestRun, phase benchv1alpha1.TestRunPhase, message string) error {
	now := metav1.Now()
	run.Status.Phase = phase
	run.Status.CompletionTime = &now
	ready := metav1.ConditionTrue
	if phase == benchv1alpha1.TestRunFailed {
		ready = metav1.ConditionFalse
	}
	setCondition(r.Recorder, run, &run.Status.Conditions, run.Generation, benchv1alpha1.ConditionReady, ready, benchv1alpha1.ReasonReady, message)

	if r.Grafana.Enabled() && run.Status.StartTime != nil {
		targetName := ""
		testName := run.Spec.TestCaseRef.Name
		if run.Status.ResolvedSpec != nil {
			targetName = run.Status.ResolvedSpec.TopologyRef.Name
		}
		if _, err := r.Grafana.Annotate(ctx, run.Status.StartTime.Time, now.Time,
			[]string{"k6", "quick", "test:" + testName, "target:" + targetName}, message); err != nil {
			logf.FromContext(ctx).Error(err, "failed to post Grafana completion annotation")
		}
	}
	return r.Status().Update(ctx, run)
}

func (r *TestRunReconciler) reconcileDelete(ctx context.Context, run *benchv1alpha1.TestRun) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(run, testRunFinalizer) {
		return ctrl.Result{}, nil
	}
	if run.Status.JobRef != nil {
		rc, err := r.resolveRunRunnerClient(ctx, run)
		if err != nil {
			logf.FromContext(ctx).Info("runner cluster unreachable during delete, skipping remote job cleanup", "error", err.Error())
		} else {
			job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: run.Status.JobRef.Name, Namespace: run.Namespace}}
			propagation := client.PropagationPolicy(metav1.DeletePropagationBackground)
			if err := rc.Delete(ctx, job, propagation); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		}
	}
	controllerutil.RemoveFinalizer(run, testRunFinalizer)
	return ctrl.Result{}, r.Update(ctx, run)
}

// applyOverrides merges a TestRun's ad hoc overrides onto a copy of its
// TestCase's spec, without mutating the TestCase itself.
func applyOverrides(spec *benchv1alpha1.TestCaseSpec, overrides *benchv1alpha1.TestRunOverrides) {
	if overrides == nil {
		return
	}
	if overrides.VUs != nil {
		spec.VUs = *overrides.VUs
	}
	if overrides.Iterations != nil {
		spec.Iterations = *overrides.Iterations
	}
	if overrides.Flow != nil {
		spec.Flow = *overrides.Flow
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *TestRunReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Grafana == nil {
		r.Grafana = grafana.FromEnv()
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&benchv1alpha1.TestRun{}).
		Owns(&batchv1.Job{}).
		Named("testrun").
		Complete(r)
}
