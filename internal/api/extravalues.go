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
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// extraValuesConfigMapName is deterministic from the topology name, so the
// UI never has to invent or remember one.
func extraValuesConfigMapName(topologyName string) string {
	return topologyName + "-extra-values"
}

// extraValuesBody is both the request and response shape: a single raw YAML
// document merged over the generated Helm values last (see
// internal/render.MergeExtraValues) - the escape hatch for authentik/chart
// settings the CRD doesn't model as a dedicated field.
type extraValuesBody struct {
	ValuesYAML string `json:"valuesYAML"`
}

// getExtraValues returns the topology's current extra-values YAML, or an
// empty string if none is set - not a 404, since "no extra values" is a
// normal, common state rather than a missing resource.
func (s *Server) getExtraValues(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var cm corev1.ConfigMap
	key := types.NamespacedName{Name: extraValuesConfigMapName(name), Namespace: s.cfg.Namespace}
	if err := s.client.Get(r.Context(), key, &cm); err != nil {
		if apierrors.IsNotFound(err) {
			writeJSON(w, r, http.StatusOK, extraValuesBody{})
			return
		}
		writeAPIError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, extraValuesBody{ValuesYAML: cm.Data["values.yaml"]})
}

// putExtraValues upserts the topology's extra-values ConfigMap and points
// spec.authentik.extraValuesRef at it, creating the reference the first time
// this is called. It always writes the AuthentikTopology itself (touching an
// annotation even when extraValuesRef.name doesn't change) so
// AuthentikTopologyReconciler - which does not separately watch this
// ConfigMap - picks up the new content: editing the ConfigMap alone would
// otherwise never re-trigger a reconcile.
func (s *Server) putExtraValues(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	var body extraValuesBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&body); err != nil {
		writeError(w, r, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
		return
	}
	if body.ValuesYAML != "" {
		var probe map[string]any
		if err := yaml.Unmarshal([]byte(body.ValuesYAML), &probe); err != nil {
			writeError(w, r, http.StatusBadRequest, fmt.Sprintf("invalid YAML: %v", err))
			return
		}
	}

	var topo benchv1alpha1.AuthentikTopology
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: name, Namespace: s.cfg.Namespace}, &topo); err != nil {
		writeAPIError(w, r, err)
		return
	}

	cmName := extraValuesConfigMapName(name)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: s.cfg.Namespace},
		Data:       map[string]string{"values.yaml": body.ValuesYAML},
	}
	var existing corev1.ConfigMap
	err := s.client.Get(r.Context(), types.NamespacedName{Name: cmName, Namespace: s.cfg.Namespace}, &existing)
	switch {
	case apierrors.IsNotFound(err):
		if err := s.client.Create(r.Context(), cm); err != nil {
			writeAPIError(w, r, err)
			return
		}
	case err != nil:
		writeAPIError(w, r, err)
		return
	default:
		existing.Data = cm.Data
		if err := s.client.Update(r.Context(), &existing); err != nil {
			writeAPIError(w, r, err)
			return
		}
	}

	if topo.Spec.Authentik.ExtraValuesRef == nil || topo.Spec.Authentik.ExtraValuesRef.Name != cmName {
		topo.Spec.Authentik.ExtraValuesRef = &corev1.LocalObjectReference{Name: cmName}
	}
	if topo.Annotations == nil {
		topo.Annotations = map[string]string{}
	}
	// Forces a spec-unrelated write every call, so the controller's watch on
	// AuthentikTopology fires even when extraValuesRef.name is unchanged -
	// this ConfigMap's content is the only thing that changed.
	topo.Annotations["bench.goauthentik.io/extra-values-updated-at"] = time.Now().UTC().Format(time.RFC3339Nano)
	if err := s.client.Update(r.Context(), &topo); err != nil {
		writeAPIError(w, r, err)
		return
	}

	writeJSON(w, r, http.StatusOK, body)
}
