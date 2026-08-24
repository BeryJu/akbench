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
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestProxyQueryInstant(t *testing.T) {
	var gotPath, gotQuery string
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.Query().Get("query")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer prom.Close()

	s, _ := newServer(t, Config{PrometheusURL: prom.URL})
	rec := do(t, s, http.MethodGet, "/api/v1/metrics/query?query=up", nil)
	requireStatus(t, rec, http.StatusOK)

	if gotPath != "/api/v1/query" {
		t.Fatalf("forwarded to %q, want the instant query endpoint", gotPath)
	}
	if gotQuery != "up" {
		t.Fatalf("forwarded query %q", gotQuery)
	}
}

// start+end must select query_range, and a missing step must be defaulted -
// Prometheus rejects a range query without one.
func TestProxyQueryRange(t *testing.T) {
	var gotPath, gotStep string
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotStep = r.URL.Path, r.URL.Query().Get("step")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
	}))
	defer prom.Close()

	s, _ := newServer(t, Config{PrometheusURL: prom.URL})
	rec := do(t, s, http.MethodGet,
		"/api/v1/metrics/query?query=k6_vus&start=1700000000&end=1700003600", nil)
	requireStatus(t, rec, http.StatusOK)

	if gotPath != "/api/v1/query_range" {
		t.Fatalf("forwarded to %q, want the range query endpoint", gotPath)
	}
	if gotStep != "15s" {
		t.Fatalf("step defaulted to %q", gotStep)
	}
}

func TestProxyQueryRequiresQuery(t *testing.T) {
	s, _ := newServer(t, Config{PrometheusURL: "http://prometheus.invalid"})
	requireStatus(t, do(t, s, http.MethodGet, "/api/v1/metrics/query", nil), http.StatusBadRequest)
}

func TestProxyQueryWithoutPrometheus(t *testing.T) {
	s, _ := newServer(t, Config{})
	requireStatus(t, do(t, s, http.MethodGet, "/api/v1/metrics/query?query=up", nil), http.StatusServiceUnavailable)
}

func TestCompare(t *testing.T) {
	seen := map[string]bool{}
	prom := fakePrometheus(t, func(query string) (string, bool) {
		seen[query] = true
		switch {
		case strings.Contains(query, "k6_http_reqs_total") && strings.Contains(query, `testid="login-a"`):
			return "120.5", true
		case strings.Contains(query, "k6_http_reqs_total") && strings.Contains(query, `testid="login-b"`):
			return "90", true
		// Everything else reports no data, which must land as an absent key
		// rather than a zero the UI would render as a real measurement.
		default:
			return "", false
		}
	})

	s, _ := newServer(t, Config{PrometheusURL: prom.URL})
	rec := do(t, s, http.MethodGet, "/api/v1/metrics/compare?testids=login-a,login-b", nil)
	requireStatus(t, rec, http.StatusOK)

	body := decodeBody[struct {
		Window  string `json:"window"`
		Results []struct {
			TestID string             `json:"testid"`
			Values map[string]float64 `json:"values"`
		} `json:"results"`
	}](t, rec)

	if body.Window != "5m" {
		t.Fatalf("default window is %q, want 5m", body.Window)
	}
	if len(body.Results) != 2 {
		t.Fatalf("got %d results, want 2", len(body.Results))
	}
	if got := body.Results[0].Values["http_reqs"]; got != 120.5 {
		t.Fatalf("login-a http_reqs is %v, want 120.5", got)
	}
	if _, present := body.Results[0].Values["vus"]; present {
		t.Fatalf("a metric with no data was reported anyway: %v", body.Results[0].Values)
	}
	// The p95 query has to be the native-histogram form: k6 runs with
	// TREND_AS_NATIVE_HISTOGRAM, so there is no le label to quantile over.
	wantP95 := `histogram_quantile(0.95, sum(rate(k6_http_req_duration_seconds{testid="login-a"}[5m])))`
	if !seen[wantP95] {
		t.Fatalf("p95 query was not %q; queries seen: %v", wantP95, seen)
	}
}

// A testid reaches PromQL as a label value, so anything that is not a
// Kubernetes object name has to be refused before it gets interpolated.
func TestCompareRejectsInjection(t *testing.T) {
	s, _ := newServer(t, Config{PrometheusURL: "http://prometheus.invalid"})
	for _, id := range []string{`a"} or up{`, "UPPER", "with space", "-leading"} {
		rec := do(t, s, http.MethodGet, "/api/v1/metrics/compare?testids="+url.QueryEscape(id), nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("testid %q got status %d, want 400", id, rec.Code)
		}
	}
}

func TestCompareRejectsBadWindow(t *testing.T) {
	s, _ := newServer(t, Config{PrometheusURL: "http://prometheus.invalid"})
	rec := do(t, s, http.MethodGet, "/api/v1/metrics/compare?testids=login-a&window=5m])+or+up", nil)
	requireStatus(t, rec, http.StatusBadRequest)
}

// NaN is what an empty native histogram quantiles to, and it cannot be
// encoded as JSON - it has to be dropped, not returned.
func TestCompareDropsNaN(t *testing.T) {
	prom := fakePrometheus(t, func(query string) (string, bool) {
		if strings.Contains(query, "histogram_quantile") {
			return "NaN", true
		}
		return "1", true
	})

	s, _ := newServer(t, Config{PrometheusURL: prom.URL})
	rec := do(t, s, http.MethodGet, "/api/v1/metrics/compare?testids=login-a", nil)
	requireStatus(t, rec, http.StatusOK)

	body := decodeBody[struct {
		Results []struct {
			Values map[string]float64 `json:"values"`
		} `json:"results"`
	}](t, rec)
	if _, present := body.Results[0].Values["http_req_duration_p95"]; present {
		t.Fatalf("NaN was reported as a value: %v", body.Results[0].Values)
	}
	if body.Results[0].Values["http_reqs"] != 1 {
		t.Fatalf("real values were dropped too: %v", body.Results[0].Values)
	}
}
