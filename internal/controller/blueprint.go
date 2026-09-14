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
	"bytes"
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
	"github.com/goauthentik/authentik-bench/internal/render"
)

// serverPodForExec finds the newest Running server pod for this deployment's
// release on rc - the target for ApplyBlueprint's exec. Readiness probes
// passing isn't required (the blueprint write+apply command runs
// standalone), but newest is: a rolling update can leave an old and a new
// pod briefly Running at once, and the old one, from before whatever spec
// change is in flight, may not have the fixtures volume mounted at all.
func serverPodForExec(ctx context.Context, rc client.Client, topo *benchv1alpha1.AuthentikDeployment) (*corev1.Pod, error) {
	var pods corev1.PodList
	if err := rc.List(ctx, &pods, client.InNamespace(topo.Spec.Namespace), client.MatchingLabels{
		"app.kubernetes.io/instance":  render.HelmReleaseName(topo),
		"app.kubernetes.io/component": "server",
	}); err != nil {
		return nil, fmt.Errorf("list server pods: %w", err)
	}
	var newest *corev1.Pod
	for i, p := range pods.Items {
		if p.Status.Phase != corev1.PodRunning || !p.DeletionTimestamp.IsZero() {
			continue
		}
		if newest == nil || p.CreationTimestamp.After(newest.CreationTimestamp.Time) {
			newest = &pods.Items[i]
		}
	}
	if newest == nil {
		return nil, errors.NewNotFound(corev1.Resource("pods"), "server")
	}
	return newest, nil
}

// blueprintApplyTimeout bounds ApplyBlueprint's exec: ak apply_blueprint
// cold-starts a full Django app before doing anything (observed several
// minutes on a resource-starved dev cluster running multiple authentik
// instances at once) - without a bound, a slow or wedged attempt blocks
// this reconcile forever, and since AuthentikDeploymentReconciler only runs
// one reconcile at a time by default, that starves every *other*
// deployment's reconciles too (verified live via a goroutine dump: an exec's
// stdin/stdout/stderr copy goroutines parked indefinitely on a stream read,
// and no other AuthentikDeployment reconciled again as long as it sat there).
const blueprintApplyTimeout = 5 * time.Minute

// applyBlueprint writes blueprintYAML into the fixtures PVC HelmValues
// mounted at render.BlueprintMountPath (see withBlueprintVolume) and applies
// it with `ak apply_blueprint`, via a single exec so the write and the apply
// see the same content even if the caller's own copy changes between
// reconciles. restConfig is whichever cluster topo actually runs on - always
// the same cluster rc itself talks to. A free function (not a method)
// because both AuthentikDeploymentReconciler (the shared fixtures blueprint)
// and TestCaseReconciler (the oauth-oidc template's own OAuth2
// provider/application blueprint, scoped to one TestCase) need it.
func applyBlueprint(ctx context.Context, restConfig *rest.Config, rc client.Client, topo *benchv1alpha1.AuthentikDeployment, blueprintYAML []byte) error {
	ctx, cancel := context.WithTimeout(ctx, blueprintApplyTimeout)
	defer cancel()

	pod, err := serverPodForExec(ctx, rc, topo)
	if err != nil {
		return fmt.Errorf("find server pod: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("build clientset: %w", err)
	}

	targetPath := render.BlueprintMountPath + "/" + render.BlueprintFileName
	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: "server",
			Command:   []string{"sh", "-c", "cat > " + targetPath + " && ak apply_blueprint " + targetPath},
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("build executor: %w", err)
	}

	var stdout, stderr bytes.Buffer
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  bytes.NewReader(blueprintYAML),
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		return fmt.Errorf("apply_blueprint failed: %w (stderr: %s)", err, stderr.String())
	}
	return nil
}
