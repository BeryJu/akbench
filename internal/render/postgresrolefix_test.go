package render

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// TestPostgresRoleFixJobTargetsAuthentikPool guards the same live-cluster bug
// class as BlueprintJob: this Job connects to the embedded postgres Service
// in the deployment's own namespace, so it needs the authentik pool's
// nodeSelector/toleration just like server/worker/postgres do.
func TestPostgresRoleFixJobTargetsAuthentikPool(t *testing.T) {
	topo := exampleEmbeddedDeployment()
	wc := &benchv1alpha1.WorkloadCluster{Spec: benchv1alpha1.WorkloadClusterSpec{
		WorkerPools: []benchv1alpha1.WorkerPoolSpec{{Name: "ak", Role: benchv1alpha1.WorkerPoolRoleAuthentik}},
	}}
	job := PostgresRoleFixJob(topo, wc)
	if job.Spec.Template.Spec.NodeSelector[NodePoolLabel] != "authentik" {
		t.Errorf("nodeSelector[%s] = %v, want authentik", NodePoolLabel, job.Spec.Template.Spec.NodeSelector[NodePoolLabel])
	}
	if len(job.Spec.Template.Spec.Tolerations) != 1 {
		t.Errorf("tolerations = %v, want exactly one", job.Spec.Template.Spec.Tolerations)
	}
}

func exampleEmbeddedDeployment() *benchv1alpha1.AuthentikDeployment {
	return &benchv1alpha1.AuthentikDeployment{
		Name: "authentik-1",
		Spec: benchv1alpha1.AuthentikDeploymentSpec{
			Namespace: "authentik-1",
			Postgres:  benchv1alpha1.PostgresSpec{Mode: benchv1alpha1.PostgresEmbedded},
		},
	}
}

// TestPostgresRoleFixJob guards the fix for a real, permanent gap: the
// authentik chart's bundled postgresql subchart uses the plain
// docker.io/library/postgres image rather than Bitnami's own, so no
// "postgres" role - the one the bundled postgres_exporter is hardcoded to
// authenticate as - ever gets created, on any embedded-postgres deployment,
// confirmed live even on a genuinely fresh install.
func TestPostgresRoleFixJob(t *testing.T) {
	topo := exampleEmbeddedDeployment()
	job := PostgresRoleFixJob(topo, nil)

	if got, want := job.Name, "authentik-1-postgres-role-fix"; got != want {
		t.Errorf("name = %q, want %q", got, want)
	}
	if got, want := job.Namespace, "authentik-1"; got != want {
		t.Errorf("namespace = %q, want %q", got, want)
	}
	if job.Spec.Template.Spec.RestartPolicy != corev1.RestartPolicyOnFailure {
		t.Errorf("restartPolicy = %q, want OnFailure", job.Spec.Template.Spec.RestartPolicy)
	}
	if len(job.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("expected exactly one container, got %d", len(job.Spec.Template.Spec.Containers))
	}
	c := job.Spec.Template.Spec.Containers[0]

	env := map[string]*corev1.EnvVarSource{}
	for _, e := range c.Env {
		env[e.Name] = e.ValueFrom
	}
	pgPassword, ok := env["PGPASSWORD"]
	if !ok || pgPassword.SecretKeyRef == nil || pgPassword.SecretKeyRef.Name != "authentik-1-postgresql" || pgPassword.SecretKeyRef.Key != "password" {
		t.Errorf("PGPASSWORD source = %+v, want secretKeyRef authentik-1-postgresql/password", pgPassword)
	}
	newPassword, ok := env["POSTGRES_PASSWORD"]
	if !ok || newPassword.SecretKeyRef == nil || newPassword.SecretKeyRef.Name != "authentik-1-postgresql" || newPassword.SecretKeyRef.Key != "postgres-password" {
		t.Errorf("POSTGRES_PASSWORD source = %+v, want secretKeyRef authentik-1-postgresql/postgres-password", newPassword)
	}

	// The new password must reach SQL via psql's own -v/:'name' variable
	// substitution, not raw string interpolation - guards against
	// reintroducing a quoting bug if this script is ever edited.
	script := c.Command[len(c.Command)-1]
	if !strings.Contains(script, `-v newpass="$POSTGRES_PASSWORD"`) {
		t.Errorf("script does not pass the new password via psql -v: %s", script)
	}
	if !strings.Contains(script, `:'newpass'`) {
		t.Errorf("script does not reference the substituted variable via :'newpass': %s", script)
	}
	if strings.Contains(script, "$POSTGRES_PASSWORD'") || strings.Contains(script, "'$POSTGRES_PASSWORD") {
		t.Errorf("script appears to interpolate POSTGRES_PASSWORD directly into a quoted SQL literal: %s", script)
	}
}
