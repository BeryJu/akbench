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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
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
