package controller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
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
// +kubebuilder:rbac:groups=bench.goauthentik.io,resources=testcasetemplates,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create

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

	// One-time migration of the deprecated singular spec.deploymentRef into
	// spec.deploymentRefs, so every object converges on the one field the UI,
	// printcolumns and samples use. Lossless: Deployments() already treated
	// the two identically, this only makes that explicit in the stored spec.
	if tc.Spec.MigrateDeploymentRef() {
		if err := r.Update(ctx, &tc); err != nil {
			return ctrl.Result{}, err
		}
		log.Info("Migrated TestCase deploymentRef to deploymentRefs", "deployment", tc.Spec.DeploymentRefs[0].Name)
	}

	refs := tc.Deployments()
	if len(refs) == 0 {
		setCondition(r.Recorder, &tc, &tc.Status.Conditions, tc.Generation, benchv1alpha1.ConditionDeploymentReady, metav1.ConditionFalse,
			benchv1alpha1.ReasonDependencyNotReady, "neither deploymentRef nor deploymentRefs is set")
		setCondition(r.Recorder, &tc, &tc.Status.Conditions, tc.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
			benchv1alpha1.ReasonDependencyNotReady, "waiting for a deployment to be named")
		tc.Status.Phase = benchv1alpha1.PhaseFor(tc.Status.Conditions)
		return ctrl.Result{}, r.Status().Update(ctx, &tc)
	}

	var tmpl *benchv1alpha1.TestCaseTemplate
	usingTemplate := tc.Spec.Inline == "" && tc.Spec.ScriptConfigMapRef == nil
	if usingTemplate {
		var err error
		tmpl, err = resolveTemplate(ctx, r.Client, tc.Namespace, tc.Spec.Template)
		if err != nil {
			if apierrors.IsNotFound(err) {
				setCondition(r.Recorder, &tc, &tc.Status.Conditions, tc.Generation, benchv1alpha1.ConditionScriptReady, metav1.ConditionFalse,
					benchv1alpha1.ReasonDependencyNotReady, fmt.Sprintf("TestCaseTemplate %q not found", tc.Spec.Template))
				setCondition(r.Recorder, &tc, &tc.Status.Conditions, tc.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
					benchv1alpha1.ReasonDependencyNotReady, "waiting for template")
				tc.Status.Phase = benchv1alpha1.PhaseFor(tc.Status.Conditions)
				return ctrl.Result{}, r.Status().Update(ctx, &tc)
			}
			return ctrl.Result{}, fmt.Errorf("resolve template: %w", err)
		}
	}

	// One pass per target deployment - everything below (readiness,
	// runner-cluster resolution, script/fixtures mirroring, OAuth
	// provisioning, the k6 workload itself) is independently resolved per
	// target, since two targets can legitimately be on different clusters
	// (resolveRunnerClient only defaults to a *shared* cluster when
	// spec.runnerClusterRef pins one explicitly for the whole TestCase).
	var statuses []benchv1alpha1.TestCaseDeploymentStatus
	var notReadyMessages []string
	allReady := true
	needsRequeue := false
	keepBurnNames := desiredBurnNames(&tc, refs)
	// Runner clusters visited this pass, keyed by cluster name ("" is the
	// local cluster), so stale Burn Deployments are cleaned up once per
	// cluster after every target has been reconciled.
	runnerClients := map[string]client.Client{}

	for _, ref := range refs {
		status := benchv1alpha1.TestCaseDeploymentStatus{Name: ref.Name}

		topo, targetName, host, port, err := resolveReadyDeployment(ctx, r.Client, tc.Namespace, ref.Name)
		if err != nil {
			allReady = false
			switch {
			case apierrors.IsNotFound(err):
				notReadyMessages = append(notReadyMessages, fmt.Sprintf("%s: AuthentikDeployment not found", ref.Name))
			case errors.Is(err, errDeploymentNotReady):
				notReadyMessages = append(notReadyMessages, fmt.Sprintf("%s: not Ready yet", ref.Name))
			default:
				return ctrl.Result{}, fmt.Errorf("resolve deployment %q: %w", ref.Name, err)
			}
			statuses = append(statuses, status)
			continue
		}

		rc, remote, err := resolveRunnerClient(ctx, r.Tracker, r.Client, tc.Namespace, tc.Spec.RunnerClusterRef, topo.Spec.ClusterRef)
		if err != nil {
			allReady = false
			needsRequeue = true
			notReadyMessages = append(notReadyMessages, fmt.Sprintf("%s: runner cluster unreachable: %v", ref.Name, err))
			statuses = append(statuses, status)
			continue
		}
		runnerClients[runnerClusterName(&tc, topo)] = rc
		if remote {
			if err := ensureNamespace(ctx, rc, tc.Namespace); err != nil {
				return ctrl.Result{}, fmt.Errorf("ensure runner namespace for %q: %w", ref.Name, err)
			}
		}

		scriptCM, err := r.ensureScriptConfigMap(ctx, rc, remote, &tc, tmpl)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("ensure script configmap for %q: %w", ref.Name, err)
		}
		if err := ensureFixturesSecret(ctx, r.Client, rc, tc.Namespace); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensure fixtures secret for %q: %w", ref.Name, err)
		}

		var oauthHost string
		var oauthPort int32
		if tmpl != nil && tmpl.Spec.RequiresOAuthClient {
			oauthHost, oauthPort, err = r.ensureOAuthClient(ctx, rc, remote, &tc, ref.Name, topo, host, port)
			if err != nil {
				allReady = false
				notReadyMessages = append(notReadyMessages, fmt.Sprintf("%s: %v", ref.Name, err))
				statuses = append(statuses, status)
				continue
			}
		}

		params := render.K6WorkloadParams{
			TestCase:            &tc,
			DeploymentName:      ref.Name,
			TargetName:          targetName,
			BenchHost:           host,
			BenchPort:           port,
			RemoteWriteURL:      defaultRemoteWriteURL,
			ScriptConfigMapName: scriptCM,
			FixturesSecretName:  fixturesSecretName,
			OAuthTestClientHost: oauthHost,
			OAuthTestClientPort: oauthPort,
		}

		switch tc.Spec.Profile {
		case benchv1alpha1.ProfileBurn:
			name, err := r.reconcileBurn(ctx, rc, remote, &tc, ref.Name, params)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("reconcile burn deployment for %q: %w", ref.Name, err)
			}
			status.BurnWorkloadRef = &corev1.LocalObjectReference{Name: name}
		case benchv1alpha1.ProfileQuick:
			// Execution is a TestRun's job - nothing to reconcile
			// continuously here; keepBurnNames being empty cleans up any
			// stale Burn Deployment below if the profile was switched from
			// Burn to Quick.
		default:
			setCondition(r.Recorder, &tc, &tc.Status.Conditions, tc.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
				benchv1alpha1.ReasonReconcileError, fmt.Sprintf("unknown profile %q", tc.Spec.Profile))
			tc.Status.Phase = benchv1alpha1.PhaseFor(tc.Status.Conditions)
			return ctrl.Result{}, r.Status().Update(ctx, &tc)
		}

		status.Ready = true
		statuses = append(statuses, status)
	}

	// Cleans up a target that's been removed from DeploymentRefs (or every
	// target, switching Burn->Quick) on each runner cluster visited above.
	// This must run after the loop, with the complete keep set: running it
	// per target would delete every later target's Burn Deployment on a
	// shared runner cluster, only for the next iteration to recreate it.
	// Known limitation: if a target is removed entirely and no remaining
	// target shares its runner cluster, that cluster is never visited, so its
	// now-orphaned Burn Deployment is never cleaned up - the same class of
	// gap as this project's other "day-2 removal" boundaries (e.g.
	// WorkloadCluster worker pool resize), not something silently assumed
	// away.
	for cluster, rc := range runnerClients {
		if err := r.cleanupStaleBurnDeployments(ctx, rc, &tc, keepBurnNames); err != nil {
			return ctrl.Result{}, fmt.Errorf("clean up stale burn deployments on runner cluster %q: %w", cluster, err)
		}
	}

	tc.Status.Deployments = statuses
	if !allReady {
		reason := benchv1alpha1.ReasonDependencyNotReady
		setCondition(r.Recorder, &tc, &tc.Status.Conditions, tc.Generation, benchv1alpha1.ConditionDeploymentReady, metav1.ConditionFalse,
			reason, strings.Join(notReadyMessages, "; "))
		setCondition(r.Recorder, &tc, &tc.Status.Conditions, tc.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
			reason, strings.Join(notReadyMessages, "; "))
		tc.Status.Phase = benchv1alpha1.PhaseFor(tc.Status.Conditions)
		if err := r.Status().Update(ctx, &tc); err != nil {
			return ctrl.Result{}, err
		}
		if needsRequeue {
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
		return ctrl.Result{}, nil
	}
	setCondition(r.Recorder, &tc, &tc.Status.Conditions, tc.Generation, benchv1alpha1.ConditionDeploymentReady, metav1.ConditionTrue,
		benchv1alpha1.ReasonReady, "every deployment is Ready")
	setCondition(r.Recorder, &tc, &tc.Status.Conditions, tc.Generation, benchv1alpha1.ConditionScriptReady, metav1.ConditionTrue,
		benchv1alpha1.ReasonReady, "script resolved")

	tc.Status.ObservedGeneration = tc.Generation
	setCondition(r.Recorder, &tc, &tc.Status.Conditions, tc.Generation, benchv1alpha1.ConditionReady, metav1.ConditionTrue,
		benchv1alpha1.ReasonReady, "test case reconciled")
	tc.Status.Phase = benchv1alpha1.PhaseFor(tc.Status.Conditions)
	if err := r.Status().Update(ctx, &tc); err != nil {
		return ctrl.Result{}, err
	}
	log.Info("reconciled TestCase", "profile", tc.Spec.Profile, "deployments", len(refs))
	return ctrl.Result{}, nil
}

func (r *TestCaseReconciler) reconcileDelete(ctx context.Context, tc *benchv1alpha1.TestCase) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(tc, testCaseFinalizer) {
		return ctrl.Result{}, nil
	}

	// Resolve every target's runner cluster the same way Reconcile does, so
	// a Burn Deployment on a remote cluster gets cleaned up there too. If a
	// deployment or its cluster is already gone, skip remote cleanup for
	// that one rather than block finalizer removal forever on a cluster
	// that may never come back.
	for _, ref := range tc.Deployments() {
		var topo benchv1alpha1.AuthentikDeployment
		if err := r.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: tc.Namespace}, &topo); err != nil {
			continue
		}
		rc, _, err := resolveRunnerClient(ctx, r.Tracker, r.Client, tc.Namespace, tc.Spec.RunnerClusterRef, topo.Spec.ClusterRef)
		if err != nil {
			logf.FromContext(ctx).Info("runner cluster unreachable during delete, skipping remote cleanup", "deployment", ref.Name, "error", err.Error())
			continue
		}
		if err := r.cleanupStaleBurnDeployments(ctx, rc, tc, nil); err != nil {
			return ctrl.Result{}, err
		}
	}
	controllerutil.RemoveFinalizer(tc, testCaseFinalizer)
	return ctrl.Result{}, r.Update(ctx, tc)
}

// cleanupStaleBurnDeployments deletes every Burn Deployment this TestCase
// owns on rc whose name isn't in keep - a target removed from
// DeploymentRefs, or every target when switching Burn->Quick (keep == nil).
// Relies on BurnDeployment's own object-level labels (LabelTestCase, set by
// render.BurnDeployment) to find them all without needing to already know
// their exact names, plus deleteLegacyBurnDeployment for the one Deployment
// that predates those labels.
func (r *TestCaseReconciler) cleanupStaleBurnDeployments(ctx context.Context, rc client.Client, tc *benchv1alpha1.TestCase, keep map[string]bool) error {
	if err := deleteLegacyBurnDeployment(ctx, rc, tc); err != nil {
		return err
	}
	var list appsv1.DeploymentList
	if err := rc.List(ctx, &list, client.InNamespace(tc.Namespace), client.MatchingLabels{benchv1alpha1.LabelTestCase: tc.Name}); err != nil {
		return err
	}
	for i := range list.Items {
		dep := &list.Items[i]
		if keep[dep.Name] {
			continue
		}
		if err := rc.Delete(ctx, dep); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// desiredBurnNames is every target's Burn Deployment name, including
// targets that aren't Ready this pass - a transiently unready target
// shouldn't lose its load generator just because another target shares its
// runner cluster. Empty for Quick, so a Burn->Quick switch cleans up
// everything.
func desiredBurnNames(tc *benchv1alpha1.TestCase, refs []corev1.LocalObjectReference) map[string]bool {
	keep := map[string]bool{}
	if tc.Spec.Profile != benchv1alpha1.ProfileBurn {
		return keep
	}
	for _, ref := range refs {
		keep[render.BurnDeploymentName(tc.Name, ref.Name)] = true
	}
	return keep
}

// runnerClusterName is the cluster resolveRunnerClient picks for one
// target, by name ("" is the local cluster).
func runnerClusterName(tc *benchv1alpha1.TestCase, topo *benchv1alpha1.AuthentikDeployment) string {
	if tc.Spec.RunnerClusterRef != nil && tc.Spec.RunnerClusterRef.Name != "" {
		return tc.Spec.RunnerClusterRef.Name
	}
	return topo.Spec.ClusterRef.Name
}

// deleteLegacyBurnDeployment removes the "k6-<testcase>" Burn Deployment
// written before a TestCase could drive more than one deployment. It had no
// object-level labels, so cleanupStaleBurnDeployments' label List never
// finds it, and left alone it would keep generating load against the
// original target alongside its renamed render.BurnDeploymentName
// successor. Ownership is checked via its pod template label and its
// selector lacking LabelDeployment, since the bare name alone can collide
// with a current-scheme name ("k6-<a>-<b>" is both TestCase "a-b"'s legacy
// name and TestCase "a"'s name for deployment "b").
func deleteLegacyBurnDeployment(ctx context.Context, rc client.Client, tc *benchv1alpha1.TestCase) error {
	var dep appsv1.Deployment
	if err := rc.Get(ctx, types.NamespacedName{Name: "k6-" + tc.Name, Namespace: tc.Namespace}, &dep); err != nil {
		return client.IgnoreNotFound(err)
	}
	if dep.Spec.Template.Labels[benchv1alpha1.LabelTestCase] != tc.Name {
		return nil
	}
	if dep.Spec.Selector != nil {
		if _, ok := dep.Spec.Selector.MatchLabels[benchv1alpha1.LabelDeployment]; ok {
			return nil
		}
	}
	logf.FromContext(ctx).Info("Deleting legacy Burn Deployment", "name", dep.Name)
	if err := rc.Delete(ctx, &dep); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// reconcileBurn applies one target's k6 burn Deployment via rc, returning
// its name. An ownerReference is only set when the Deployment lives on the
// local cluster (remote==false): a cross-cluster ownerReference would carry
// a UID the remote cluster's garbage collector can never recognize, so
// remote cleanup instead goes through cleanupStaleBurnDeployments above.
func (r *TestCaseReconciler) reconcileBurn(ctx context.Context, rc client.Client, remote bool, tc *benchv1alpha1.TestCase, deploymentName string, params render.K6WorkloadParams) (string, error) {
	name := render.BurnDeploymentName(tc.Name, deploymentName)
	dep := render.BurnDeployment(name, tc.Namespace, params)
	if !remote {
		if err := controllerutil.SetControllerReference(tc, dep, r.Scheme); err != nil {
			return "", err
		}
	}
	existing := &appsv1.Deployment{}
	err := rc.Get(ctx, types.NamespacedName{Name: name, Namespace: tc.Namespace}, existing)
	switch {
	case apierrors.IsNotFound(err):
		if err := rc.Create(ctx, dep); err != nil {
			return "", err
		}
	case err != nil:
		return "", err
	default:
		existing.Spec = dep.Spec
		if err := rc.Update(ctx, existing); err != nil {
			return "", err
		}
	}
	return name, nil
}

// ensureScriptConfigMap mirrors the resolved script content - a
// user-supplied scriptConfigMapRef (BYO, used directly), inline content
// stored on the TestCase itself, or the resolved TestCaseTemplate's own
// script, in that precedence order (matching render.scriptFileName's own) -
// into the ConfigMap the k6 pod mounts on rc, returning its name. tmpl is
// nil when scriptConfigMapRef or inline already won upstream (see Reconcile's
// usingTemplate).
func (r *TestCaseReconciler) ensureScriptConfigMap(ctx context.Context, rc client.Client, remote bool, tc *benchv1alpha1.TestCase, tmpl *benchv1alpha1.TestCaseTemplate) (string, error) {
	if tc.Spec.ScriptConfigMapRef != nil {
		// BYO script: used directly, nothing to mirror.
		return tc.Spec.ScriptConfigMapRef.Name, nil
	}

	var content string
	if tc.Spec.Inline != "" {
		content = tc.Spec.Inline
	} else {
		if tmpl.Spec.Script == "" {
			return "", fmt.Errorf("TestCaseTemplate %q has an empty script", tmpl.Name)
		}
		content = tmpl.Spec.Script
	}
	const key = "script.ts"

	name := "k6-script-" + tc.Name
	cm := &corev1.ConfigMap{
		Name: name, Namespace: tc.Namespace,
		Data: map[string]string{key: content},
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
		if slices.ContainsFunc(tc.Deployments(), func(ref corev1.LocalObjectReference) bool { return ref.Name == topo.GetName() }) {
			reqs = append(reqs, reconcile.Request{Name: tc.Name, Namespace: tc.Namespace})
		}
	}
	return reqs
}
