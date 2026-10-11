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
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/types"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// podMetric is one resource usage query this endpoint can run, scoped to a
// deployment's own namespace rather than a testid label the way
// compareMetrics is - cAdvisor/kube-state-metrics never tag container
// metrics with a k6 testid, only namespace/pod/container, matching
// gitops/dashboards/authentik.json's own "CPU by pod"/"Memory by pod" panels
// (the same expressions, verified against this project's live cluster).
type podMetric struct {
	Label string `json:"label"`
	Unit  string `json:"unit"`
	// NeedsWindow marks a Query with a second %s verb (a [range] subquery
	// window) alongside its %q namespace verb - cpu's rate() needs one,
	// memory's plain gauge read doesn't. Explicit rather than counting "%s"
	// occurrences in Query (compareMetrics' own trick): these queries also
	// use %q for the namespace, so a literal-substring count conflates the
	// two verb kinds and silently leaves the window unsubstituted - caught
	// live, confirmed by Prometheus itself rejecting the resulting
	// malformed "[%!s(MISSING)]" range selector.
	NeedsWindow bool
	Query       string `json:"-"`
}

// podMetrics is keyed by the ?resource= query param. container!="",
// container!="POD" excludes cAdvisor's own per-pod aggregate/pause-container
// series, the same filter every panel in authentik.json already uses.
var podMetrics = map[string]podMetric{
	"cpu": {
		Label:       "CPU by pod",
		Unit:        "cores",
		NeedsWindow: true,
		Query:       `sum by (pod) (rate(container_cpu_usage_seconds_total{namespace=%q, container!="", container!="POD"}[%s]))`,
	},
	"memory": {
		Label: "Memory by pod",
		Unit:  "bytes",
		Query: `sum by (pod) (container_memory_working_set_bytes{namespace=%q, container!="", container!="POD"})`,
	},
}

// PodRangeSeries is one pod's time series for a podMetric.
type PodRangeSeries struct {
	TestID string       `json:"testid"`
	Pod    string       `json:"pod"`
	Points []RangePoint `json:"points"`
}

type podRangeResponse struct {
	Resource string           `json:"resource"`
	Label    string           `json:"label"`
	Unit     string           `json:"unit"`
	Series   []PodRangeSeries `json:"series"`
}

// deploymentTargetForTestID resolves where a testid's own AuthentikDeployment
// actually runs - its namespace and the Prometheus that scrapes it -
// deliberately NOT the same resolution prometheusURLForTestID uses: that one
// prefers the k6 *runner's* cluster (TestCase.Spec.RunnerClusterRef), which
// can legitimately differ from the deployment's own cluster, but the
// deployment's server/worker/postgres pods (what this endpoint reports on)
// always live on the deployment's own clusterRef/namespace regardless of
// where the runner executes. Returns ok=false if any part of that chain
// can't be resolved (deleted TestCase/Deployment, or a WorkloadCluster that
// hasn't reconciled a PrometheusURL yet) - the caller skips that testid
// rather than failing the whole request, the same "one bad testid shouldn't
// blank the rest" reasoning listAlerts already uses.
func (s *Server) deploymentTargetForTestID(ctx context.Context, testid string) (prometheusURL, namespace string, ok bool) {
	var tc benchv1alpha1.TestCase
	if err := s.client.Get(ctx, types.NamespacedName{Name: testid, Namespace: s.cfg.Namespace}, &tc); err != nil {
		return "", "", false
	}
	var topo benchv1alpha1.AuthentikDeployment
	key := types.NamespacedName{Name: tc.Spec.DeploymentRef.Name, Namespace: s.cfg.Namespace}
	if err := s.client.Get(ctx, key, &topo); err != nil {
		return "", "", false
	}
	if topo.Spec.ClusterRef.Name == "" {
		return s.cfg.PrometheusURL, topo.Spec.Namespace, true
	}
	var wc benchv1alpha1.WorkloadCluster
	wcKey := types.NamespacedName{Name: topo.Spec.ClusterRef.Name, Namespace: s.cfg.Namespace}
	if err := s.client.Get(ctx, wcKey, &wc); err != nil || wc.Status.PrometheusURL == "" {
		return "", "", false
	}
	return wc.Status.PrometheusURL, topo.Spec.Namespace, true
}

// listPodMetrics answers GET /metrics/pods?testids=a,b&resource=cpu|memory&start=&end=[&window=][&step=] -
// per-pod resource usage for each testid's own deployment, the per-pod
// breakdown a test's own compare/detail charts (which are all per-testid
// aggregates) can't show.
func (s *Server) listPodMetrics(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	raw := q.Get("testids")
	if raw == "" {
		writeError(w, r, http.StatusBadRequest, "testids is required")
		return
	}
	var testids []string
	for id := range strings.SplitSeq(raw, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if !safeLabelValue.MatchString(id) {
			writeError(w, r, http.StatusBadRequest, fmt.Sprintf("invalid testid %q", id))
			return
		}
		testids = append(testids, id)
	}
	if len(testids) == 0 {
		writeError(w, r, http.StatusBadRequest, "testids is required")
		return
	}

	metric, ok := podMetrics[q.Get("resource")]
	if !ok {
		writeError(w, r, http.StatusBadRequest, fmt.Sprintf("unknown resource %q", q.Get("resource")))
		return
	}

	start, end := q.Get("start"), q.Get("end")
	if !safeUnixTime.MatchString(start) || !safeUnixTime.MatchString(end) {
		writeError(w, r, http.StatusBadRequest, "start and end must be unix timestamps")
		return
	}
	window := q.Get("window")
	if window == "" {
		window = "1m"
	}
	if !safeWindow.MatchString(window) {
		writeError(w, r, http.StatusBadRequest, fmt.Sprintf("invalid window %q", window))
		return
	}
	step := q.Get("step")
	if step == "" {
		step = defaultStep
	}
	if !safeWindow.MatchString(step) {
		writeError(w, r, http.StatusBadRequest, fmt.Sprintf("invalid step %q", step))
		return
	}

	resp := podRangeResponse{Resource: q.Get("resource"), Label: metric.Label, Unit: metric.Unit}
	// Dedup by namespace+Prometheus: two testids against the same deployment
	// (e.g. a quick and a burn TestCase on one AuthentikDeployment) would
	// otherwise issue - and chart - the identical query twice.
	seen := map[string]bool{}
	for _, id := range testids {
		baseURL, namespace, ok := s.deploymentTargetForTestID(r.Context(), id)
		if !ok {
			continue
		}
		dedupeKey := baseURL + "/" + namespace
		if seen[dedupeKey] {
			continue
		}
		seen[dedupeKey] = true

		var query string
		if metric.NeedsWindow {
			query = fmt.Sprintf(metric.Query, namespace, window)
		} else {
			query = fmt.Sprintf(metric.Query, namespace)
		}
		series, err := s.promRangeMulti(r.Context(), baseURL, query, start, end, step)
		if err != nil {
			writeError(w, r, http.StatusBadGateway, err.Error())
			return
		}
		for _, s := range series {
			resp.Series = append(resp.Series, PodRangeSeries{TestID: id, Pod: s.labels["pod"], Points: s.points})
		}
	}
	writeJSON(w, r, http.StatusOK, resp)
}

type labeledSeries struct {
	labels map[string]string
	points []RangePoint
}

// promRangeMulti is promRange's multi-series sibling: reads every result
// entry (not just the first) along with its Metric labels, for queries like
// "by (pod)" that return more than one series on purpose.
func (s *Server) promRangeMulti(ctx context.Context, baseURL, query, start, end, step string) ([]labeledSeries, error) {
	params := map[string][]string{queryParam: {query}, "start": {start}, "end": {end}, "step": {step}}
	resp, err := s.promGet(ctx, baseURL, "/api/v1/query_range", params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var parsed promMatrixResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxPromResponseBytes)).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode Prometheus response: %w", err)
	}
	if parsed.Status != promStatusSuccess {
		if parsed.Error != "" {
			return nil, fmt.Errorf("prometheus: %s", parsed.Error)
		}
		return nil, fmt.Errorf("prometheus returned an error status")
	}

	out := make([]labeledSeries, 0, len(parsed.Data.Result))
	for _, entry := range parsed.Data.Result {
		points := make([]RangePoint, 0, len(entry.Values))
		for _, sample := range entry.Values {
			var ts float64
			if err := json.Unmarshal(sample[0], &ts); err != nil {
				return nil, fmt.Errorf("decode sample timestamp: %w", err)
			}
			var text string
			if err := json.Unmarshal(sample[1], &text); err != nil {
				return nil, fmt.Errorf("decode sample value: %w", err)
			}
			value, err := strconv.ParseFloat(text, 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				continue
			}
			points = append(points, RangePoint{Time: int64(ts), Value: value})
		}
		out = append(out, labeledSeries{labels: entry.Metric, points: points})
	}
	return out, nil
}
