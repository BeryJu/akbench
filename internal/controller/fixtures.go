package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// fixturesSecretName is the Secret every k6 workload mounts users.json from.
// Created once per namespace by `make fixtures` (see
// fixtures/gen-blueprint.py) on the management cluster; ensureFixturesSecret
// below mirrors it onto whichever cluster a TestCase/TestRun's k6 pod
// actually runs on.
const fixturesSecretName = "bench-fixtures-users"

// ensureFixturesSecret mirrors the management cluster's fixturesSecretName
// Secret onto rc, the same create-or-update pattern
// TestCaseReconciler.ensureScriptConfigMap already uses for the k6 script
// ConfigMap. Every k6 pod (Burn Deployment and Quick Job alike) mounts this
// Secret by this same fixed name (render.K6WorkloadParams.FixturesSecretName)
// regardless of which cluster it runs on - without a real copy there, every
// k6 pod sits in ContainerCreating forever ("FailedMount: secret ... not
// found"), the exact failure mode this closes. A safe, cheap no-op when rc
// already points at the management cluster (remote==false): it just reads
// and rewrites the same object back to itself.
func ensureFixturesSecret(ctx context.Context, c client.Client, rc client.Client, namespace string) error {
	var source corev1.Secret
	if err := c.Get(ctx, types.NamespacedName{Name: fixturesSecretName, Namespace: namespace}, &source); err != nil {
		return fmt.Errorf("get fixtures secret: %w", err)
	}
	var existing corev1.Secret
	err := rc.Get(ctx, types.NamespacedName{Name: fixturesSecretName, Namespace: namespace}, &existing)
	switch {
	case apierrors.IsNotFound(err):
		mirrored := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: fixturesSecretName, Namespace: namespace},
			Data:       source.Data,
		}
		return rc.Create(ctx, mirrored)
	case err != nil:
		return err
	default:
		existing.Data = source.Data
		return rc.Update(ctx, &existing)
	}
}

// fixturesBlueprintConfigMapName is the ConfigMap `make fixtures` also
// writes (same script, same management-cluster namespace as
// fixturesSecretName) holding the authentik blueprint that creates the
// synthetic users users.json has credentials for. AuthentikDeploymentReconciler
// reads this once a deployment's HelmRelease is Ready, mirrors it onto the
// runner cluster as a ConfigMap the chart mounts into the server pod, and
// execs `ak apply_blueprint` against it before marking the deployment Ready -
// optional (a deployment with no ConfigMap by this name just skips the step),
// since fixtures are a dev/benchmark convenience, not a hard dependency.
const (
	fixturesBlueprintConfigMapName = "bench-fixtures-blueprint"
	fixturesBlueprintKey           = "blueprint.yaml"
)
