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
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// fakePodPrometheus serves a query_range response with one series per pod -
// unlike fakePrometheusRange (suite_test.go), which only ever returns a
// single series with empty labels, this is what a "by (pod)" query actually
// looks like and is what listPodMetrics has to read the Metric labels from.
// Also records the real query string it received: a fake that answers
// "success" unconditionally regardless of what PromQL it was sent would
// never have caught the real bug this file guards against (a malformed
// "[%!s(MISSING)]" range selector from an unsubstituted format verb) - a
// real Prometheus rejects that with a parse error, so the fake must at
// least let a test see what was actually sent.
func fakePodPrometheus(t *testing.T, pods map[string]float64) (*httptest.Server, *string) {
	t.Helper()
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("query")
		result := make([]any, 0, len(pods))
		for pod, value := range pods {
			result = append(result, map[string]any{
				"metric": map[string]string{"pod": pod},
				"values": []any{[]any{1700000000, strconv.FormatFloat(value, 'f', -1, 64)}},
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data":   map[string]any{"resultType": "matrix", "result": result},
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &gotQuery
}

func TestListPodMetricsResolvesDeploymentNamespace(t *testing.T) {
	prom, gotQuery := fakePodPrometheus(t, map[string]float64{
		"server-0": 1.5,
		"worker-0": 0.25,
	})

	wc := &benchv1alpha1.WorkloadCluster{}
	wc.Name = "wc1"
	wc.Namespace = testNamespace
	wc.Status.PrometheusURL = prom.URL

	deployment := sampleDeployment("topo")
	deployment.Spec.ClusterRef.Name = "wc1"
	deployment.Spec.Namespace = "ak-topo"
	tc := sampleTestCase("login-topo", "topo", benchv1alpha1.ProfileBurn)

	s, _ := newServer(t, Config{}, wc, deployment, tc)
	rec := do(t, s, http.MethodGet, "/api/v1/metrics/pods?testids=login-topo&resource=cpu&start=1700000000&end=1700003600", nil)
	requireStatus(t, rec, http.StatusOK)

	body := decodeBody[struct {
		Resource string           `json:"resource"`
		Series   []PodRangeSeries `json:"series"`
	}](t, rec)
	if body.Resource != "cpu" {
		t.Errorf("resource = %q, want cpu", body.Resource)
	}
	if len(body.Series) != 2 {
		t.Fatalf("got %d series, want 2 (one per pod): %+v", len(body.Series), body.Series)
	}
	byPod := map[string]PodRangeSeries{}
	for _, series := range body.Series {
		byPod[series.Pod] = series
		if series.TestID != "login-topo" {
			t.Errorf("series for pod %q has testid %q, want login-topo", series.Pod, series.TestID)
		}
	}
	if _, ok := byPod["server-0"]; !ok {
		t.Errorf("missing series for pod server-0: %+v", body.Series)
	}
	if _, ok := byPod["worker-0"]; !ok {
		t.Errorf("missing series for pod worker-0: %+v", body.Series)
	}

	if strings.Contains(*gotQuery, "%!") || strings.Contains(*gotQuery, "MISSING") {
		t.Fatalf("query has an unsubstituted format verb: %q", *gotQuery)
	}
	if !strings.Contains(*gotQuery, `namespace="ak-topo"`) {
		t.Errorf("query doesn't scope to the deployment's own namespace: %q", *gotQuery)
	}
	if !strings.Contains(*gotQuery, "[1m]") {
		t.Errorf("cpu query missing its rate() window: %q", *gotQuery)
	}
	if !strings.Contains(*gotQuery, `kube_pod_owner{namespace="ak-topo", owner_kind!="Job"}`) {
		t.Errorf("query doesn't exclude one-shot Job pods (blueprint/postgres-role-fix): %q", *gotQuery)
	}
}

func TestListPodMetricsRejectsUnknownResource(t *testing.T) {
	s, _ := newServer(t, Config{})
	rec := do(t, s, http.MethodGet, "/api/v1/metrics/pods?testids=a&resource=disk&start=1700000000&end=1700003600", nil)
	requireStatus(t, rec, http.StatusBadRequest)
}

// TestListPodMetricsMemoryQueryHasNoWindow guards the memory resource
// specifically: it's a plain gauge read with only one format verb
// (namespace), not two like cpu's rate() - the exact asymmetry that broke
// when the original code counted literal "%s" substrings instead of
// checking an explicit NeedsWindow flag.
func TestListPodMetricsMemoryQueryHasNoWindow(t *testing.T) {
	prom, gotQuery := fakePodPrometheus(t, map[string]float64{"server-0": 536870912})

	wc := &benchv1alpha1.WorkloadCluster{}
	wc.Name = "wc1"
	wc.Namespace = testNamespace
	wc.Status.PrometheusURL = prom.URL

	deployment := sampleDeployment("topo2")
	deployment.Spec.ClusterRef.Name = "wc1"
	deployment.Spec.Namespace = "ak-topo2"
	tc := sampleTestCase("login-topo2", "topo2", benchv1alpha1.ProfileBurn)

	s, _ := newServer(t, Config{}, wc, deployment, tc)
	rec := do(t, s, http.MethodGet, "/api/v1/metrics/pods?testids=login-topo2&resource=memory&start=1700000000&end=1700003600", nil)
	requireStatus(t, rec, http.StatusOK)

	if strings.Contains(*gotQuery, "%!") || strings.Contains(*gotQuery, "MISSING") {
		t.Fatalf("query has an unsubstituted format verb: %q", *gotQuery)
	}
	if !strings.Contains(*gotQuery, `namespace="ak-topo2"`) {
		t.Errorf("query doesn't scope to the deployment's own namespace: %q", *gotQuery)
	}
}
