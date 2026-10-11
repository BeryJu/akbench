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
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// alertmanagerFetchTimeout bounds each cluster's own Alertmanager call so one
// unreachable instance (a NodePort that's briefly down, a cluster mid-reconcile)
// can't stall the whole aggregated response - confirmed live this session that
// exactly this can happen (a workload cluster's Prometheus/Alertmanager NodePort
// went briefly unreachable while its node was being recycled).
const alertmanagerFetchTimeout = 5 * time.Second

// Alert is one firing/pending/silenced alert as Alertmanager's own v2 API
// reports it, trimmed to what the UI needs and tagged with which workload
// cluster it came from - Alertmanager is installed per workload cluster (see
// render.KubePrometheusStackHelmValues/WorkloadClusterReconciler.ensurePrometheus),
// there is no single central instance to query, unlike Prometheus' own
// per-testid resolution (prometheusURLForTestID) this mirrors the reasoning
// of but not the mechanism: alerts aren't scoped to one testid, so every
// cluster's own Alertmanager is queried and the results merged.
type Alert struct {
	Cluster      string            `json:"cluster"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	State        string            `json:"state"`
	StartsAt     string            `json:"startsAt"`
	EndsAt       string            `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
}

// alertmanagerAlert is Alertmanager's own GET /api/v2/alerts response shape -
// trimmed to the fields listAlerts actually uses.
type alertmanagerAlert struct {
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     string            `json:"startsAt"`
	EndsAt       string            `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Status       struct {
		State string `json:"state"`
	} `json:"status"`
}

// listAlerts aggregates every workload cluster's own Alertmanager - there is
// no central instance, see Alert's own doc comment. A cluster whose
// Alertmanager hasn't reconciled a URL yet, or isn't reachable right now, is
// silently skipped rather than failing the whole request: one cluster having
// a bad day shouldn't blank out every other cluster's alerts.
func (s *Server) listAlerts(w http.ResponseWriter, r *http.Request) {
	var clusters benchv1alpha1.WorkloadClusterList
	if err := s.client.List(r.Context(), &clusters, client.InNamespace(s.cfg.Namespace)); err != nil {
		writeAPIError(w, r, err)
		return
	}

	alerts := []Alert{}
	for _, wc := range clusters.Items {
		if wc.Status.AlertmanagerURL == "" {
			continue
		}
		fetched, err := s.fetchAlerts(r.Context(), wc.Name, wc.Status.AlertmanagerURL)
		if err != nil {
			continue
		}
		alerts = append(alerts, fetched...)
	}
	// Newest first - an alert that just started firing is the one worth
	// seeing without scrolling.
	sort.Slice(alerts, func(i, j int) bool { return alerts[i].StartsAt > alerts[j].StartsAt })
	writeJSON(w, r, http.StatusOK, map[string]any{"items": alerts})
}

func (s *Server) fetchAlerts(ctx context.Context, cluster, baseURL string) ([]Alert, error) {
	ctx, cancel := context.WithTimeout(ctx, alertmanagerFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(baseURL, "/")+"/api/v2/alerts", nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("alertmanager at %s returned %d", baseURL, resp.StatusCode)
	}
	var raw []alertmanagerAlert
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]Alert, 0, len(raw))
	for _, a := range raw {
		out = append(out, Alert{
			Cluster:      cluster,
			Labels:       a.Labels,
			Annotations:  a.Annotations,
			State:        a.Status.State,
			StartsAt:     a.StartsAt,
			EndsAt:       a.EndsAt,
			GeneratorURL: a.GeneratorURL,
		})
	}
	return out, nil
}
