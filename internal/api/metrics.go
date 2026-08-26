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
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// CompareMetric is one column of the test-comparison table. The queries are
// written against the metric names k6's experimental-prometheus-rw output
// actually produces: counters get a _total suffix, rates a _rate suffix,
// gauges none, and trends - which the k6 workloads run with
// K6_PROMETHEUS_RW_TREND_AS_NATIVE_HISTOGRAM=true - become native histograms
// named after their base unit, hence k6_http_req_duration_seconds and values
// in seconds rather than k6's native milliseconds.
type CompareMetric struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Unit  string `json:"unit"`
	// Query is a template with two verbs: the label selector, then the range
	// window.
	Query string `json:"-"`
	// Better records which direction is an improvement, so the UI can
	// highlight a winner without hardcoding per-metric knowledge.
	Better string `json:"better"`
}

// betterHigher/betterLower are CompareMetric.Better's two directions.
const (
	betterHigher = "higher"
	betterLower  = "lower"
)

// metricKeyIterations/metricKeyVUs also name TestCaseSpec's json fields of
// the same values - not the same string by coincidence, but two independent
// literals goconst can't tell apart, so kept named here for its sake.
const (
	metricKeyHTTPReqs   = "http_reqs"
	metricKeyIterations = "iterations"
	metricKeyVUs        = "vus"
)

// compareMetrics is the fixed summary every test comparison reports. Kept
// small on purpose: anything deeper belongs in the Grafana dashboards the
// compare view embeds alongside this table.
var compareMetrics = []CompareMetric{
	{Key: metricKeyHTTPReqs, Label: "HTTP requests/s", Unit: "req/s", Better: betterHigher,
		Query: `sum(rate(k6_http_reqs_total{%s}[%s]))`},
	{Key: metricKeyIterations, Label: "Logins/s", Unit: "logins/s", Better: betterHigher,
		Query: `sum(rate(k6_iterations_total{%s}[%s]))`},
	{Key: "http_req_duration_p95", Label: "Request duration p95", Unit: "s", Better: betterLower,
		Query: `histogram_quantile(0.95, sum(rate(k6_http_req_duration_seconds{%s}[%s])))`},
	{Key: "http_req_duration_p99", Label: "Request duration p99", Unit: "s", Better: betterLower,
		Query: `histogram_quantile(0.99, sum(rate(k6_http_req_duration_seconds{%s}[%s])))`},
	{Key: "iteration_duration_p95", Label: "Login duration p95", Unit: "s", Better: betterLower,
		Query: `histogram_quantile(0.95, sum(rate(k6_iteration_duration_seconds{%s}[%s])))`},
	{Key: "http_req_failed", Label: "Failed requests", Unit: "ratio", Better: betterLower,
		Query: `avg(k6_http_req_failed_rate{%s})`},
	{Key: "checks", Label: "Passing checks", Unit: "ratio", Better: betterHigher,
		Query: `avg(k6_checks_rate{%s})`},
	{Key: metricKeyVUs, Label: "Peak VUs", Unit: metricKeyVUs, Better: betterHigher,
		Query: `max(k6_vus{%s})`},
}

// safeLabelValue matches the Kubernetes object names that become testid
// values. Every value interpolated into PromQL is checked against it, which
// is what keeps the compare endpoint from being a PromQL injection point.
var safeLabelValue = regexp.MustCompile(`^[a-z0-9]([-a-z0-9._]*[a-z0-9])?$`)

// safeWindow matches a Prometheus duration literal.
var safeWindow = regexp.MustCompile(`^[0-9]+(ms|s|m|h|d|w|y)$`)

// safeUnixTime matches a unix timestamp in seconds, the only form
// metricsRange accepts start/end in.
var safeUnixTime = regexp.MustCompile(`^[0-9]{1,10}$`)

// defaultStep is the range-query resolution used when the caller doesn't
// specify one.
const defaultStep = "15s"

// queryParam is the PromQL-query url.Values key every query/query_range
// call passes.
const queryParam = "query"

// promStatusSuccess is Prometheus' own query-response status field value on
// success (the only one these handlers ever act on; anything else falls
// through to its own error message).
const promStatusSuccess = "success"

// proxyQuery forwards a PromQL query to the central Prometheus. Only the
// parameters below are passed through, and only the two query endpoints are
// reachable, so this stays a query proxy rather than a general-purpose hole
// through to Prometheus' admin API.
func (s *Server) proxyQuery(w http.ResponseWriter, r *http.Request) {
	if s.cfg.PrometheusURL == "" {
		writeError(w, r, http.StatusServiceUnavailable, "no Prometheus configured")
		return
	}
	q := r.URL.Query()
	query := q.Get(queryParam)
	if query == "" {
		writeError(w, r, http.StatusBadRequest, "query is required")
		return
	}

	params := url.Values{queryParam: {query}}
	path := "/api/v1/query"
	if start, end := q.Get("start"), q.Get("end"); start != "" && end != "" {
		path = "/api/v1/query_range"
		params.Set("start", start)
		params.Set("end", end)
		step := q.Get("step")
		if step == "" {
			step = defaultStep
		}
		params.Set("step", step)
	} else if ts := q.Get("time"); ts != "" {
		params.Set("time", ts)
	}

	resp, err := s.promGet(r.Context(), path, params)
	if err != nil {
		writeError(w, r, http.StatusBadGateway, err.Error())
		return
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPromResponseBytes))
	if err != nil {
		writeError(w, r, http.StatusBadGateway, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	if _, err := w.Write(body); err != nil {
		return
	}
}

// maxPromResponseBytes caps a proxied Prometheus response. Range queries over
// a long window can be large, but nothing the UI asks for is near this.
const maxPromResponseBytes = 32 << 20

// CompareResult is one row of the comparison table: a test, and its summary
// values keyed by CompareMetric.Key. A metric with no data is absent rather
// than zero, so "the test never reported this" reads differently from "it
// reported zero".
type CompareResult struct {
	TestID string             `json:"testid"`
	Values map[string]float64 `json:"values"`
}

type compareResponse struct {
	Window  string          `json:"window"`
	Metrics []CompareMetric `json:"metrics"`
	Results []CompareResult `json:"results"`
}

// compare answers GET /metrics/compare?testids=a,b,c[&window=5m][&time=...],
// the numeric half of the compare view (the other half being an embedded
// Grafana dashboard).
func (s *Server) compare(w http.ResponseWriter, r *http.Request) {
	if s.cfg.PrometheusURL == "" {
		writeError(w, r, http.StatusServiceUnavailable, "no Prometheus configured")
		return
	}
	raw := r.URL.Query().Get("testids")
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

	window := r.URL.Query().Get("window")
	if window == "" {
		window = "5m"
	}
	if !safeWindow.MatchString(window) {
		writeError(w, r, http.StatusBadRequest, fmt.Sprintf("invalid window %q", window))
		return
	}
	at := r.URL.Query().Get("time")

	resp := compareResponse{Window: window, Metrics: compareMetrics}
	for _, id := range testids {
		row := CompareResult{TestID: id, Values: map[string]float64{}}
		selector := fmt.Sprintf("testid=%q", id)
		for _, m := range compareMetrics {
			query := m.Query
			if strings.Count(query, "%s") == 2 {
				query = fmt.Sprintf(query, selector, window)
			} else {
				query = fmt.Sprintf(query, selector)
			}
			value, ok, err := s.promInstant(r.Context(), query, at)
			if err != nil {
				writeError(w, r, http.StatusBadGateway, err.Error())
				return
			}
			if ok {
				row.Values[m.Key] = value
			}
		}
		resp.Results = append(resp.Results, row)
	}
	writeJSON(w, r, http.StatusOK, resp)
}

// RangePoint is one (timestamp, value) sample of a metricsRange result.
type RangePoint struct {
	Time  int64   `json:"time"`
	Value float64 `json:"value"`
}

// RangeSeries is one test's time series for a single CompareMetric.
type RangeSeries struct {
	TestID string       `json:"testid"`
	Points []RangePoint `json:"points"`
}

type rangeResponse struct {
	Metric CompareMetric `json:"metric"`
	Series []RangeSeries `json:"series"`
}

// metricsRange answers GET
// /metrics/range?testids=a,b&metric=<key>&start=&end=[&window=][&step=], the
// per-test time series behind the compare view's charts and a TestCase's own
// metrics panel. It is the only place a Burn TestCase's metrics can be
// viewed at all: Burn has no TestRun history the way Quick does, since it
// runs as a self-healing Deployment rather than a one-shot Job, so its only
// record of what happened is whatever Prometheus still retains.
func (s *Server) metricsRange(w http.ResponseWriter, r *http.Request) {
	if s.cfg.PrometheusURL == "" {
		writeError(w, r, http.StatusServiceUnavailable, "no Prometheus configured")
		return
	}
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

	var metric *CompareMetric
	for i := range compareMetrics {
		if compareMetrics[i].Key == q.Get("metric") {
			metric = &compareMetrics[i]
			break
		}
	}
	if metric == nil {
		writeError(w, r, http.StatusBadRequest, fmt.Sprintf("unknown metric %q", q.Get("metric")))
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

	resp := rangeResponse{Metric: *metric}
	for _, id := range testids {
		selector := fmt.Sprintf("testid=%q", id)
		query := metric.Query
		if strings.Count(query, "%s") == 2 {
			query = fmt.Sprintf(query, selector, window)
		} else {
			query = fmt.Sprintf(query, selector)
		}
		points, err := s.promRange(r.Context(), query, start, end, step)
		if err != nil {
			writeError(w, r, http.StatusBadGateway, err.Error())
			return
		}
		resp.Series = append(resp.Series, RangeSeries{TestID: id, Points: points})
	}
	writeJSON(w, r, http.StatusOK, resp)
}

// promMatrixResponse is the query_range shape: one entry per distinct label
// set. Every CompareMetric.Query aggregates down to a single series
// (sum(...)/avg(...)/max(...) with no "by"), so promRange only ever reads
// the first result entry.
type promMatrixResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Values [][2]json.RawMessage `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

// promRange runs a range query and returns its single series, dropping
// non-finite samples the same way promInstant drops a NaN quantile.
func (s *Server) promRange(ctx context.Context, query, start, end, step string) ([]RangePoint, error) {
	params := url.Values{queryParam: {query}, "start": {start}, "end": {end}, "step": {step}}
	resp, err := s.promGet(ctx, "/api/v1/query_range", params)
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
	// []RangePoint{}, not nil: a nil slice marshals to JSON null, and the
	// frontend iterates this array unconditionally (a test with no data in
	// the window is a legitimate, common case - not an error to special-case).
	if len(parsed.Data.Result) == 0 {
		return []RangePoint{}, nil
	}
	raw := parsed.Data.Result[0].Values
	points := make([]RangePoint, 0, len(raw))
	for _, sample := range raw {
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
	return points, nil
}

func (s *Server) promGet(ctx context.Context, path string, params url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimSuffix(s.cfg.PrometheusURL, "/")+path+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	return s.http.Do(req)
}

// promVectorResponse is the slice of Prometheus' response format the compare
// endpoint needs: the first sample of an instant vector.
type promVectorResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Value []json.RawMessage `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

// promInstant runs an instant query and returns its single value, reporting
// ok=false for an empty result (a test that reported nothing in the window).
func (s *Server) promInstant(ctx context.Context, query, at string) (float64, bool, error) {
	params := url.Values{queryParam: {query}}
	if at != "" {
		params.Set("time", at)
	}
	resp, err := s.promGet(ctx, "/api/v1/query", params)
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = resp.Body.Close() }()

	var parsed promVectorResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxPromResponseBytes)).Decode(&parsed); err != nil {
		return 0, false, fmt.Errorf("decode Prometheus response: %w", err)
	}
	if parsed.Status != promStatusSuccess {
		if parsed.Error != "" {
			return 0, false, fmt.Errorf("prometheus: %s", parsed.Error)
		}
		return 0, false, fmt.Errorf("prometheus returned %s", resp.Status)
	}
	if len(parsed.Data.Result) == 0 || len(parsed.Data.Result[0].Value) != 2 {
		return 0, false, nil
	}
	// A vector sample is [timestamp, "value"] - the value is a JSON string.
	var text string
	if err := json.Unmarshal(parsed.Data.Result[0].Value[1], &text); err != nil {
		return 0, false, fmt.Errorf("decode sample value: %w", err)
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, false, fmt.Errorf("parse sample value %q: %w", text, err)
	}
	// histogram_quantile over an empty histogram yields NaN, which is both
	// meaningless here and unrepresentable in JSON - report it as no data.
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false, nil
	}
	return value, true, nil
}
