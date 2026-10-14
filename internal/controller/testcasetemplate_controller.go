package controller

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	benchv1alpha1 "github.com/authentik-labs/akbench/api/v1alpha1"
)

// TestCaseTemplateReconciler reconciles a TestCaseTemplate object.
//
// A template carries no remote or owned resources - a TestCase resolves one
// directly via a plain Get (see testcase_controller.go's resolveTemplate),
// the same way it already resolves an AuthentikDeployment. This reconciler
// only exists to give a template a Ready condition/phase (validating
// spec.script isn't empty) so a broken template - e.g. accidentally
// emptied out via kubectl edit - is visible the same
// way every other CRD's status already is, not because anything here needs
// watching or reacting to.
type TestCaseTemplateReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=bench.goauthentik.io,resources=testcasetemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bench.goauthentik.io,resources=testcasetemplates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=bench.goauthentik.io,resources=testcasetemplates/finalizers,verbs=update

func (r *TestCaseTemplateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var tmpl benchv1alpha1.TestCaseTemplate
	if err := r.Get(ctx, req.NamespacedName, &tmpl); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if tmpl.Spec.Script == "" {
		setCondition(r.Recorder, &tmpl, &tmpl.Status.Conditions, tmpl.Generation, benchv1alpha1.ConditionReady, metav1.ConditionFalse,
			benchv1alpha1.ReasonReconcileError, "spec.script is empty")
	} else {
		setCondition(r.Recorder, &tmpl, &tmpl.Status.Conditions, tmpl.Generation, benchv1alpha1.ConditionReady, metav1.ConditionTrue,
			benchv1alpha1.ReasonReady, "template is usable")
	}
	tmpl.Status.ObservedGeneration = tmpl.Generation
	tmpl.Status.Phase = benchv1alpha1.PhaseFor(tmpl.Status.Conditions)
	return ctrl.Result{}, r.Status().Update(ctx, &tmpl)
}

// SetupWithManager sets up the controller with the Manager.
func (r *TestCaseTemplateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&benchv1alpha1.TestCaseTemplate{}).
		Named("testcasetemplate").
		Complete(r)
}
