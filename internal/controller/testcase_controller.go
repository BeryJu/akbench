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
	"os"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
	"github.com/goauthentik/authentik-bench/internal/multicluster"
	"github.com/goauthentik/authentik-bench/internal/render"
	k6scripts "github.com/goauthentik/authentik-bench/k6/scripts"
)

const testCaseFinalizer = "bench.goauthentik.io/remote-cleanup"

// defaultRemoteWriteURL is the in-cluster Prometheus every k6 workload
// remote-writes to before M4 introduces the central mTLS receiver. It is
// intentionally a package var, not a const, so tests and later milestones
// can override it without touching call sites.
var defaultRemoteWriteURL = envOrDefault("BENCH_PROMETHEUS_RW_URL", "http://prometheus-server.bench-system.svc.cluster.local/api/v1/write")

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// TestCaseReconciler reconciles a TestCase object.
//
// The k6 workload runs on spec.runnerClusterRef if set, else the deployment's
// own spec.clusterRef, else the management cluster - resolved fresh every
// reconcile via Tracker, since a workload cluster's reachability can change
// over time.
type TestCaseReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Tracker  *multicluster.Tracker
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=bench.goauthentik.io,resources=testcases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bench.goauthentik.io,resources=testcases/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=bench.goauthentik.io,resources=testcases/finalizers,verbs=update
// +kubebuilder:rbac:groups=bench.goauthentik.io,resources=authentikdeployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete

func (r *TestCaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var tc benchv1alpha1.TestCase
	if err := r.Get(ctx, req.NamespacedName, &tc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !tc.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &tc)
	}
	if !controllerutil.ContainsFinalizer(&tc, testCaseFinalizer) {
		controllerutil.AddFinalizer(&tc, testCaseFinalizer)
		if err := r.Update(ctx, &tc); err != nil {
			return ctrl.Result{}, err
		}
	}

	topo, targetName, host, port, err := resolveReadyDeployment(ctx, r.Client, tc.Namespace, tc.Spec.DeploymentRef.Name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			setCondition(r.Recorder, &tc, &tc.Status.Conditions, tc.Generation, benchv1alpha1.ConditionDeploymentReady, metav1.ConditionFalse,
				benchv1alpha1.ReasonDependencyNotReady, fmt.Sprintf("AuthentikDeployment %q not found", tc.Spec.DeploymentRef.Name))
			setCondition(r.Recorder, &tc, &tc.Status.Conditions, tc.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
				benchv1alpha1.ReasonDependencyNotReady, "waiting for deployment")
			tc.Status.Phase = benchv1alpha1.PhaseFor(tc.Status.Conditions)
			return ctrl.Result{}, r.Status().Update(ctx, &tc)
		}
		if errors.Is(err, errDeploymentNotReady) {
			setCondition(r.Recorder, &tc, &tc.Status.Conditions, tc.Generation, benchv1alpha1.ConditionDeploymentReady, metav1.ConditionFalse,
				benchv1alpha1.ReasonDependencyNotReady, "AuthentikDeployment is not Ready yet")
			setCondition(r.Recorder, &tc, &tc.Status.Conditions, tc.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
				benchv1alpha1.ReasonDependencyNotReady, "waiting for deployment")
			tc.Status.Phase = benchv1alpha1.PhaseFor(tc.Status.Conditions)
			return ctrl.Result{}, r.Status().Update(ctx, &tc)
		}
		return ctrl.Result{}, fmt.Errorf("resolve deployment: %w", err)
	}
	setCondition(r.Recorder, &tc, &tc.Status.Conditions, tc.Generation, benchv1alpha1.ConditionDeploymentReady, metav1.ConditionTrue,
		benchv1alpha1.ReasonReady, "deployment is Ready")

	rc, remote, err := resolveRunnerClient(ctx, r.Tracker, r.Client, tc.Namespace, tc.Spec.RunnerClusterRef, topo.Spec.ClusterRef)
	if err != nil {
		setCondition(r.Recorder, &tc, &tc.Status.Conditions, tc.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
			benchv1alpha1.ReasonDependencyNotReady, fmt.Sprintf("waiting for runner cluster to be reachable: %v", err))
		tc.Status.Phase = benchv1alpha1.PhaseFor(tc.Status.Conditions)
		if err := r.Status().Update(ctx, &tc); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	if remote {
		if err := ensureNamespace(ctx, rc, tc.Namespace); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensure runner namespace: %w", err)
		}
	}

	scriptCM, err := r.ensureScriptConfigMap(ctx, rc, remote, &tc)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure script configmap: %w", err)
	}
	setCondition(r.Recorder, &tc, &tc.Status.Conditions, tc.Generation, benchv1alpha1.ConditionScriptReady, metav1.ConditionTrue,
		benchv1alpha1.ReasonReady, "script resolved")

	if err := ensureFixturesSecret(ctx, r.Client, rc, tc.Namespace); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure fixtures secret: %w", err)
	}

	params := render.K6WorkloadParams{
		TestCase:            &tc,
		TargetName:          targetName,
		BenchHost:           host,
		BenchPort:           port,
		RemoteWriteURL:      defaultRemoteWriteURL,
		ScriptConfigMapName: scriptCM,
		FixturesSecretName:  fixturesSecretName,
	}

	switch tc.Spec.Profile {
	case benchv1alpha1.ProfileBurn:
		if err := r.reconcileBurn(ctx, rc, remote, &tc, params); err != nil {
			return ctrl.Result{}, fmt.Errorf("reconcile burn deployment: %w", err)
		}
	case benchv1alpha1.ProfileQuick:
		// Execution is a TestRun's job - nothing to reconcile continuously
		// here besides removing a stale burn Deployment if the profile was
		// switched from Burn to Quick.
		if err := r.deleteBurnDeployment(ctx, rc, &tc); err != nil {
			return ctrl.Result{}, fmt.Errorf("remove stale burn deployment: %w", err)
		}
		tc.Status.BurnWorkloadRef = nil
	default:
		setCondition(r.Recorder, &tc, &tc.Status.Conditions, tc.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
			benchv1alpha1.ReasonReconcileError, fmt.Sprintf("unknown profile %q", tc.Spec.Profile))
		tc.Status.Phase = benchv1alpha1.PhaseFor(tc.Status.Conditions)
		return ctrl.Result{}, r.Status().Update(ctx, &tc)
	}

	tc.Status.ObservedGeneration = tc.Generation
	setCondition(r.Recorder, &tc, &tc.Status.Conditions, tc.Generation, benchv1alpha1.ConditionReady, metav1.ConditionTrue,
		benchv1alpha1.ReasonReady, "test case reconciled")
	tc.Status.Phase = benchv1alpha1.PhaseFor(tc.Status.Conditions)
	if err := r.Status().Update(ctx, &tc); err != nil {
		return ctrl.Result{}, err
	}
	log.Info("reconciled TestCase", "profile", tc.Spec.Profile)
	return ctrl.Result{}, nil
}

func (r *TestCaseReconciler) reconcileDelete(ctx context.Context, tc *benchv1alpha1.TestCase) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(tc, testCaseFinalizer) {
		return ctrl.Result{}, nil
	}

	// Resolve the runner cluster the same way Reconcile does, so a Burn
	// Deployment on a remote cluster gets cleaned up there too. If the
	// deployment or its cluster is already gone, skip remote cleanup rather
	// than block finalizer removal forever on a cluster that may never come
	// back.
	rc := r.Client
	var topo benchv1alpha1.AuthentikDeployment
	if err := r.Get(ctx, types.NamespacedName{Name: tc.Spec.DeploymentRef.Name, Namespace: tc.Namespace}, &topo); err == nil {
		resolved, _, err := resolveRunnerClient(ctx, r.Tracker, r.Client, tc.Namespace, tc.Spec.RunnerClusterRef, topo.Spec.ClusterRef)
		if err != nil {
			logf.FromContext(ctx).Info("runner cluster unreachable during delete, skipping remote cleanup", "error", err.Error())
			rc = nil
		} else {
			rc = resolved
		}
	}
	if rc != nil {
		if err := r.deleteBurnDeployment(ctx, rc, tc); err != nil {
			return ctrl.Result{}, err
		}
	}
	controllerutil.RemoveFinalizer(tc, testCaseFinalizer)
	return ctrl.Result{}, r.Update(ctx, tc)
}

func (r *TestCaseReconciler) burnDeploymentName(tc *benchv1alpha1.TestCase) string {
	return "k6-" + tc.Name
}

func (r *TestCaseReconciler) deleteBurnDeployment(ctx context.Context, rc client.Client, tc *benchv1alpha1.TestCase) error {
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: r.burnDeploymentName(tc), Namespace: tc.Namespace}}
	if err := rc.Delete(ctx, dep); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// reconcileBurn applies the k6 burn Deployment via rc. An ownerReference is
// only set when the Deployment lives on the local cluster (remote==false):
// a cross-cluster ownerReference would carry a UID the remote cluster's
// garbage collector can never recognize, so remote cleanup instead goes
// through the finalizer-driven deleteBurnDeployment above.
func (r *TestCaseReconciler) reconcileBurn(ctx context.Context, rc client.Client, remote bool, tc *benchv1alpha1.TestCase, params render.K6WorkloadParams) error {
	name := r.burnDeploymentName(tc)
	dep := render.BurnDeployment(name, tc.Namespace, params)
	if !remote {
		if err := controllerutil.SetControllerReference(tc, dep, r.Scheme); err != nil {
			return err
		}
	}
	existing := &appsv1.Deployment{}
	err := rc.Get(ctx, types.NamespacedName{Name: name, Namespace: tc.Namespace}, existing)
	switch {
	case apierrors.IsNotFound(err):
		if err := rc.Create(ctx, dep); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		existing.Spec = dep.Spec
		if err := rc.Update(ctx, existing); err != nil {
			return err
		}
	}
	tc.Status.BurnWorkloadRef = &corev1.LocalObjectReference{Name: name}
	return nil
}

// ensureScriptConfigMap mirrors the built-in k6 script (or a user-supplied
// scriptConfigMapRef's content) into the ConfigMap the k6 pod mounts on rc,
// returning its name.
func (r *TestCaseReconciler) ensureScriptConfigMap(ctx context.Context, rc client.Client, remote bool, tc *benchv1alpha1.TestCase) (string, error) {
	if tc.Spec.ScriptConfigMapRef != nil {
		// BYO script: used directly, nothing to mirror.
		return tc.Spec.ScriptConfigMapRef.Name, nil
	}
	script := tc.Spec.Script
	if script == "" {
		script = "login.ts"
	}
	content, ok := k6scripts.Get(script)
	if !ok {
		return "", fmt.Errorf("unknown built-in script %q", script)
	}
	name := "k6-script-" + tc.Name
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: tc.Namespace},
		Data:       map[string]string{script: content},
	}
	if !remote {
		if err := controllerutil.SetControllerReference(tc, cm, r.Scheme); err != nil {
			return "", err
		}
	}
	existing := &corev1.ConfigMap{}
	err := rc.Get(ctx, types.NamespacedName{Name: name, Namespace: tc.Namespace}, existing)
	switch {
	case apierrors.IsNotFound(err):
		if err := rc.Create(ctx, cm); err != nil {
			return "", err
		}
	case err != nil:
		return "", err
	default:
		existing.Data = cm.Data
		if err := rc.Update(ctx, existing); err != nil {
			return "", err
		}
	}
	return name, nil
}

func findCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}

// splitServiceEndpoint parses an AuthentikDeployment's "host:port" status
// field.
func splitServiceEndpoint(endpoint string) (string, int32, error) {
	host, portStr, found := strings.Cut(endpoint, ":")
	if !found {
		return "", 0, fmt.Errorf("expected host:port, got %q", endpoint)
	}
	var port int32
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		return "", 0, fmt.Errorf("invalid port in %q: %w", endpoint, err)
	}
	return host, port, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TestCaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&benchv1alpha1.TestCase{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.ConfigMap{}).
		// Without this, a TestCase left in DependencyNotReady only
		// re-checks its deployment on its own next unrelated reconcile (a
		// spec edit, an owned object changing, or the informer's resync
		// period) - the deployment later becoming Ready doesn't trigger
		// anything on its own, so the stale condition can sit for a long
		// time even though the real dependency is satisfied.
		Watches(&benchv1alpha1.AuthentikDeployment{}, handler.EnqueueRequestsFromMapFunc(r.testCasesForDeployment)).
		Named("testcase").
		Complete(r)
}

// testCasesForDeployment maps an AuthentikDeployment change to every TestCase in
// the same namespace referencing it, so its controller re-checks readiness
// as soon as the deployment's own status changes instead of waiting for an
// unrelated trigger.
func (r *TestCaseReconciler) testCasesForDeployment(ctx context.Context, topo client.Object) []reconcile.Request {
	var list benchv1alpha1.TestCaseList
	if err := r.List(ctx, &list, client.InNamespace(topo.GetNamespace())); err != nil {
		return nil
	}
	var reqs []reconcile.Request
	for _, tc := range list.Items {
		if tc.Spec.DeploymentRef.Name == topo.GetName() {
			reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{Name: tc.Name, Namespace: tc.Namespace}})
		}
	}
	return reqs
}
