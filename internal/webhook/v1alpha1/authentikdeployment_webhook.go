package v1alpha1

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// nolint:unused
// log is for logging in this package.
var authentikdeploymentlog = logf.Log.WithName("authentikdeployment-resource")

// SetupAuthentikDeploymentWebhookWithManager registers the webhook for AuthentikDeployment in the manager.
func SetupAuthentikDeploymentWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &benchv1alpha1.AuthentikDeployment{}).
		WithValidator(&AuthentikDeploymentValidator{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: If you want to customise the 'path', use the flags '--defaulting-path' or '--validation-path'.
// +kubebuilder:webhook:path=/validate-bench-goauthentik-io-v1alpha1-authentikdeployment,mutating=false,failurePolicy=fail,sideEffects=None,groups=bench.goauthentik.io,resources=authentikdeployments,verbs=create;update,versions=v1alpha1,name=vauthentikdeployment-v1alpha1.kb.io,admissionReviewVersions=v1

// AuthentikDeploymentValidator struct is responsible for validating the AuthentikDeployment resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type AuthentikDeploymentValidator struct {
	// TODO(user): Add more fields as needed for validation
}

// ValidateCreate implements admission.Validator so a webhook will be registered for the type AuthentikDeployment.
func (v *AuthentikDeploymentValidator) ValidateCreate(_ context.Context, obj *benchv1alpha1.AuthentikDeployment) (admission.Warnings, error) {
	authentikdeploymentlog.Info("Validation for AuthentikDeployment upon creation", "name", obj.GetName())

	// TODO(user): fill in your validation logic upon object creation.

	return nil, nil
}

// ValidateUpdate implements admission.Validator so a webhook will be registered for the type AuthentikDeployment.
func (v *AuthentikDeploymentValidator) ValidateUpdate(_ context.Context, oldObj, newObj *benchv1alpha1.AuthentikDeployment) (admission.Warnings, error) {
	authentikdeploymentlog.Info("Validation for AuthentikDeployment upon update", "name", newObj.GetName())

	// TODO(user): fill in your validation logic upon object update.

	return nil, nil
}

// ValidateDelete implements admission.Validator so a webhook will be registered for the type AuthentikDeployment.
func (v *AuthentikDeploymentValidator) ValidateDelete(_ context.Context, obj *benchv1alpha1.AuthentikDeployment) (admission.Warnings, error) {
	authentikdeploymentlog.Info("Validation for AuthentikDeployment upon deletion", "name", obj.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
