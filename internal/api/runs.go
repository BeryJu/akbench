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

package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// runRequest is the body of POST /testcases/{name}/runs. Everything is
// optional: "run now, as defined" is the common case, and the overrides
// mirror TestRunSpec.overrides so the UI can offer a one-off load shape
// without editing the TestCase.
type runRequest struct {
	Overrides *benchv1alpha1.TestRunOverrides `json:"overrides,omitempty"`
}

// createRun is the UI's "Run now" button: it creates a TestRun for a Quick
// TestCase and lets TestRunReconciler do the rest.
func (s *Server) createRun(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	var tc benchv1alpha1.TestCase
	key := types.NamespacedName{Name: name, Namespace: s.cfg.Namespace}
	if err := s.client.Get(r.Context(), key, &tc); err != nil {
		writeAPIError(w, r, err)
		return
	}
	// Burn TestCases run continuously via their own Deployment; a TestRun
	// against one would only be rejected by the reconciler, so say so here
	// where the message can reach the user who pressed the button.
	if tc.Spec.Profile != benchv1alpha1.ProfileQuick {
		writeError(w, r, http.StatusBadRequest,
			"TestRuns execute Quick TestCases only; a Burn TestCase is already running via its own Deployment")
		return
	}

	var body runRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	deployments := tc.Deployments()
	if len(deployments) == 0 {
		writeError(w, r, http.StatusBadRequest, "TestCase has no deployment set (neither deploymentRef nor deploymentRefs)")
		return
	}

	// One TestRun per target deployment, each pinned via Spec.DeploymentRef -
	// matches this project's "immutable, individually-inspectable record"
	// TestRun philosophy (one TestRun = one Job = one target) rather than
	// teaching a single TestRun to fan out N Jobs itself.
	runs := make([]*benchv1alpha1.TestRun, 0, len(deployments))
	for _, dep := range deployments {
		run := &benchv1alpha1.TestRun{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: name + "-",
				Namespace:    s.cfg.Namespace,
				Labels:       map[string]string{benchv1alpha1.LabelTestCase: name},
			},
			Spec: benchv1alpha1.TestRunSpec{
				TestCaseRef:   corev1.LocalObjectReference{Name: name},
				DeploymentRef: &dep,
				Overrides:     body.Overrides,
			},
		}
		if err := s.client.Create(r.Context(), run); err != nil {
			writeAPIError(w, r, err)
			return
		}
		runs = append(runs, run)
	}
	writeJSON(w, r, http.StatusCreated, runs)
}
