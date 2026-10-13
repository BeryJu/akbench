package api

import (
	"net/http"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// resetDeployment asks the operator to wipe a deployment's database and
// re-apply its blueprints, by setting benchv1alpha1.AnnotationResetRequested
// (see the operator's reconcileReset for what that does). Like
// kickResource, this only writes the request; the operator does the work and
// removes the annotation when it's done.
func (s *Server) resetDeployment(w http.ResponseWriter, r *http.Request) {
	obj, err := s.fetch(r, resourceKinds["deployments"])
	if err != nil {
		writeAPIError(w, r, err)
		return
	}
	mode, _, _ := unstructured.NestedString(obj.Object, "spec", "postgres", "mode")
	if mode == string(benchv1alpha1.PostgresExternal) {
		writeError(w, r, http.StatusBadRequest, "a deployment using an external postgres can't be reset: the operator doesn't manage its database")
		return
	}
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[benchv1alpha1.AnnotationResetRequested] = time.Now().UTC().Format(time.RFC3339)
	obj.SetAnnotations(annotations)
	if err := s.client.Update(r.Context(), obj); err != nil {
		writeAPIError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, obj)
}
