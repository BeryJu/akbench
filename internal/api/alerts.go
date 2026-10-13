package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
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
// per-testid resolution (prometheusURLsForTestID) this mirrors the reasoning
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

// Meta-alerts kube-prometheus-stack ships that fire by design rather than
// reporting a problem: Watchdog always fires to prove the pipeline is alive,
// InfoInhibitor fires whenever an info-severity alert exists so Alertmanager
// can mute it. Neither is shown as an alert; a missing Watchdog is reported
// via listAlerts' unhealthy map instead.
const (
	watchdogAlert      = "Watchdog"
	infoInhibitorAlert = "InfoInhibitor"
)

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
// skipped rather than failing the whole request: one cluster having a bad day
// shouldn't blank out every other cluster's alerts. Instead it, and any
// cluster whose Watchdog isn't firing, lands in the "unhealthy" map
// (cluster -> reason) so the UI can say its alerts can't be trusted.
func (s *Server) listAlerts(w http.ResponseWriter, r *http.Request) {
	var clusters benchv1alpha1.WorkloadClusterList
	if err := s.client.List(r.Context(), &clusters, client.InNamespace(s.cfg.Namespace)); err != nil {
		writeAPIError(w, r, err)
		return
	}

	alerts := []Alert{}
	unhealthy := map[string]string{}
	for _, wc := range clusters.Items {
		if wc.Status.AlertmanagerURL == "" {
			continue
		}
		fetched, err := s.fetchAlerts(r.Context(), wc.Name, wc.Status.AlertmanagerURL)
		if err != nil {
			unhealthy[wc.Name] = "Alertmanager unreachable: " + err.Error()
			continue
		}
		watchdog := false
		for _, a := range fetched {
			switch a.Labels["alertname"] {
			case watchdogAlert:
				watchdog = true
			case infoInhibitorAlert:
			default:
				alerts = append(alerts, a)
			}
		}
		if !watchdog {
			unhealthy[wc.Name] = "Watchdog alert not firing, the alerting pipeline may be broken"
		}
	}
	// Newest first - an alert that just started firing is the one worth
	// seeing without scrolling.
	slices.SortFunc(alerts, func(a, b Alert) int { return strings.Compare(b.StartsAt, a.StartsAt) })
	writeJSON(w, r, http.StatusOK, struct {
		Items     []Alert           `json:"items"`
		Unhealthy map[string]string `json:"unhealthy"`
	}{alerts, unhealthy})
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
	defer func() { _ = resp.Body.Close() }()
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
