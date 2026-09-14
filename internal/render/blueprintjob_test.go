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

package render

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

func exampleBlueprintDeployment() *benchv1alpha1.AuthentikDeployment {
	return &benchv1alpha1.AuthentikDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: "authentik-1"},
		Spec: benchv1alpha1.AuthentikDeploymentSpec{
			Namespace: "authentik-1",
			Authentik: benchv1alpha1.AuthentikSpec{Image: "ghcr.io/goauthentik/server", Tag: "2026.8.2"},
		},
	}
}

// TestBlueprintJobNameChangesWithContent guards the one property that makes
// re-applying an unchanged blueprint a no-op and a changed one a fresh
// import: the Job name must be a pure function of (deployment, label,
// content).
func TestBlueprintJobNameChangesWithContent(t *testing.T) {
	topo := exampleBlueprintDeployment()
	a := BlueprintJobName(topo, "fixtures", "content-a")
	b := BlueprintJobName(topo, "fixtures", "content-b")
	aAgain := BlueprintJobName(topo, "fixtures", "content-a")
	if a == b {
		t.Errorf("BlueprintJobName gave the same name for different content: %q", a)
	}
	if a != aAgain {
		t.Errorf("BlueprintJobName is not deterministic: %q vs %q", a, aAgain)
	}
	if !strings.HasPrefix(a, "authentik-1-blueprint-fixtures-") {
		t.Errorf("BlueprintJobName = %q, want prefix %q", a, "authentik-1-blueprint-fixtures-")
	}
}

// TestBlueprintJobMountsConfigMapAndPVCNotDeployment guards the actual point
// of this design (see BlueprintJob's own doc comment): the blueprint PVC is
// mounted only by this one-shot Job, never by the server/worker Deployment
// (HelmValues no longer references it at all) - a real RWO-volume
// rolling-update deadlock (old and new server pod both wanting the same
// single-attach volume) motivated this move.
func TestBlueprintJobMountsConfigMapAndPVCNotDeployment(t *testing.T) {
	topo := exampleBlueprintDeployment()
	job := BlueprintJob(topo, "authentik-1-blueprint-fixtures-abcd1234", "bench-fixtures-blueprint", "blueprint.yaml")

	if job.Namespace != "authentik-1" {
		t.Errorf("job namespace = %q, want %q", job.Namespace, "authentik-1")
	}
	if job.Spec.Template.Spec.RestartPolicy != "Never" {
		t.Errorf("restartPolicy = %q, want Never", job.Spec.Template.Spec.RestartPolicy)
	}

	sc := job.Spec.Template.Spec.SecurityContext
	if sc == nil || sc.FSGroup == nil || *sc.FSGroup != 1000 {
		t.Errorf("pod securityContext.fsGroup = %v, want 1000 (csi-incus volumes are root-owned by default)", sc)
	}

	container := job.Spec.Template.Spec.Containers[0]
	if len(container.EnvFrom) != 1 || container.EnvFrom[0].SecretRef == nil || container.EnvFrom[0].SecretRef.Name != "authentik-1-authentik" {
		t.Errorf("container.envFrom = %+v, want the chart's own generated config secret authentik-1-authentik", container.EnvFrom)
	}

	var hasConfigMapVolume, hasPVCVolume bool
	for _, v := range job.Spec.Template.Spec.Volumes {
		if v.ConfigMap != nil && v.ConfigMap.Name == "bench-fixtures-blueprint" {
			hasConfigMapVolume = true
		}
		if v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName == BlueprintPVCName(topo) {
			hasPVCVolume = true
		}
	}
	if !hasConfigMapVolume {
		t.Error("job has no ConfigMap volume for the blueprint source")
	}
	if !hasPVCVolume {
		t.Error("job has no PVC volume for BlueprintMountPath")
	}

	if !strings.Contains(container.Command[len(container.Command)-1], "ak apply_blueprint "+BlueprintMountPath) {
		t.Errorf("container command = %v, want it to run ak apply_blueprint against %s", container.Command, BlueprintMountPath)
	}
}
