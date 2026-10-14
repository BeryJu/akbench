package api

import (
	"context"
	"net/http"
	"testing"

	"k8s.io/apimachinery/pkg/types"

	benchv1alpha1 "github.com/authentik-labs/akbench/api/v1alpha1"
)

func TestResetDeployment(t *testing.T) {
	external := sampleDeployment("external")
	external.Spec.Postgres = benchv1alpha1.PostgresSpec{Mode: benchv1alpha1.PostgresExternal}
	s, c := newServer(t, Config{}, sampleDeployment("reset-me"), external)

	t.Run("sets the reset annotation", func(t *testing.T) {
		rec := do(t, s, http.MethodPost, "/api/v1/deployments/reset-me/reset", nil)
		requireStatus(t, rec, http.StatusOK)

		var updated benchv1alpha1.AuthentikDeployment
		if err := c.Get(context.Background(), types.NamespacedName{Name: "reset-me", Namespace: testNamespace}, &updated); err != nil {
			t.Fatalf("get after reset: %v", err)
		}
		if updated.Annotations[benchv1alpha1.AnnotationResetRequested] == "" {
			t.Fatalf("annotation %q was not set", benchv1alpha1.AnnotationResetRequested)
		}
	})

	t.Run("refuses an external postgres", func(t *testing.T) {
		rec := do(t, s, http.MethodPost, "/api/v1/deployments/external/reset", nil)
		requireStatus(t, rec, http.StatusBadRequest)
	})

	t.Run("404s an unknown deployment", func(t *testing.T) {
		rec := do(t, s, http.MethodPost, "/api/v1/deployments/nope/reset", nil)
		requireStatus(t, rec, http.StatusNotFound)
	})
}
