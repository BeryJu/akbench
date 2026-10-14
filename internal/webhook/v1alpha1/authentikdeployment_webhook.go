package v1alpha1

import (
	"context"
	"fmt"
	"net/http"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
	"github.com/goauthentik/authentik-bench/internal/registry"
)

// defaultAuthentikImage mirrors AuthentikSpec.Image's +kubebuilder:default,
// for an object the API server hasn't defaulted (e.g. in tests).
const defaultAuthentikImage = "ghcr.io/goauthentik/server"

// registryTimeout bounds the registry lookup, well inside the API server's
// 10s webhook timeout.
const registryTimeout = 5 * time.Second

// TagChecker answers whether an image tag exists (registry.Checker).
type TagChecker interface {
	TagExists(ctx context.Context, image, tag string) (bool, error)
}

// SetupAuthentikDeploymentWebhookWithManager registers the webhook for AuthentikDeployment in the manager.
func SetupAuthentikDeploymentWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &benchv1alpha1.AuthentikDeployment{}).
		WithValidator(&AuthentikDeploymentValidator{
			Images: &registry.Checker{HTTP: &http.Client{Timeout: registryTimeout}},
		}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-bench-goauthentik-io-v1alpha1-authentikdeployment,mutating=false,failurePolicy=fail,sideEffects=None,groups=bench.goauthentik.io,resources=authentikdeployments,verbs=create;update,versions=v1alpha1,name=vauthentikdeployment-v1alpha1.kb.io,admissionReviewVersions=v1

// AuthentikDeploymentValidator rejects an AuthentikDeployment whose
// authentik image tag is malformed or doesn't exist in its registry, so a
// typo'd or not-yet-published tag fails at apply time instead of as an
// ImagePullBackOff on the workload cluster.
//
// +kubebuilder:object:generate=false
type AuthentikDeploymentValidator struct {
	Images TagChecker
}

// ValidateCreate implements admission.Validator so a webhook will be registered for the type AuthentikDeployment.
func (v *AuthentikDeploymentValidator) ValidateCreate(ctx context.Context, obj *benchv1alpha1.AuthentikDeployment) (admission.Warnings, error) {
	return v.validate(ctx, nil, obj)
}

// ValidateUpdate implements admission.Validator so a webhook will be registered for the type AuthentikDeployment.
func (v *AuthentikDeploymentValidator) ValidateUpdate(ctx context.Context, oldObj, newObj *benchv1alpha1.AuthentikDeployment) (admission.Warnings, error) {
	// Finalizer removal on a deleting object must never be blocked.
	if !newObj.DeletionTimestamp.IsZero() {
		return nil, nil
	}
	return v.validate(ctx, oldObj, newObj)
}

// ValidateDelete implements admission.Validator so a webhook will be registered for the type AuthentikDeployment.
func (v *AuthentikDeploymentValidator) ValidateDelete(_ context.Context, _ *benchv1alpha1.AuthentikDeployment) (admission.Warnings, error) {
	return nil, nil
}

func (v *AuthentikDeploymentValidator) validate(ctx context.Context, oldObj, obj *benchv1alpha1.AuthentikDeployment) (admission.Warnings, error) {
	warnings, errs := v.validateImage(ctx, oldObj, obj)
	if len(errs) == 0 {
		return warnings, nil
	}
	return warnings, apierrors.NewInvalid(
		schema.GroupKind{Group: benchv1alpha1.GroupVersion.Group, Kind: "AuthentikDeployment"}, obj.Name, errs)
}

// validateImage checks spec.authentik.{image,tag}. An unset tag means the
// chart's own default and isn't looked up. Only a definite "not found"
// rejects the object: an unreachable registry yields a warning, so a
// registry outage can't block every deployment change.
func (v *AuthentikDeploymentValidator) validateImage(ctx context.Context, oldObj, obj *benchv1alpha1.AuthentikDeployment) (admission.Warnings, field.ErrorList) {
	imagePath := field.NewPath("spec", "authentik", "image")
	tagPath := field.NewPath("spec", "authentik", "tag")

	image, tag := obj.Spec.Authentik.Image, obj.Spec.Authentik.Tag
	if image == "" {
		image = defaultAuthentikImage
	}
	// Leave an unchanged image alone, so a tag that has since been deleted
	// upstream doesn't block unrelated edits (or the operator's own updates).
	if oldObj != nil && oldObj.Spec.Authentik.Image == obj.Spec.Authentik.Image && oldObj.Spec.Authentik.Tag == tag {
		return nil, nil
	}

	var errs field.ErrorList
	if _, _, err := registry.ParseImage(image); err != nil {
		errs = append(errs, field.Invalid(imagePath, obj.Spec.Authentik.Image, err.Error()))
	}
	if tag != "" && !registry.TagPattern.MatchString(tag) {
		errs = append(errs, field.Invalid(tagPath, tag, "must be a valid image tag: letters, digits, '_', '.' and '-', at most 128 characters, not starting with '.' or '-'"))
	}
	if len(errs) > 0 || tag == "" || v.Images == nil {
		return nil, errs
	}

	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()
	exists, err := v.Images.TagExists(ctx, image, tag)
	if err != nil {
		logf.FromContext(ctx).Info("Failed to look up image tag", "image", image, "tag", tag, "error", err.Error())
		return admission.Warnings{fmt.Sprintf("could not verify that %s:%s exists: %v", image, tag, err)}, nil
	}
	if !exists {
		return nil, field.ErrorList{field.NotFound(tagPath, fmt.Sprintf("%s:%s", image, tag))}
	}
	return nil, nil
}
