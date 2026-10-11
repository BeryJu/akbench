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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// fakeAlertmanager serves GET /api/v2/alerts with a fixed, literal response -
// Alertmanager's own v2 API shape, trimmed to the fields listAlerts reads.
func fakeAlertmanager(t *testing.T, alertnames ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/alerts" {
			http.NotFound(w, r)
			return
		}
		items := make([]map[string]any, 0, len(alertnames))
		for _, name := range alertnames {
			items = append(items, map[string]any{
				"labels":      map[string]string{"alertname": name, "severity": "warning"},
				"annotations": map[string]string{"summary": name + " is firing"},
				"startsAt":    "2026-10-11T00:00:00Z",
				"status":      map[string]string{"state": "active"},
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(items)
	}))
}

// TestListAlertsAggregatesAcrossClusters guards the one real design decision
// here: there is no central Alertmanager, so every WorkloadCluster with a
// reconciled AlertmanagerURL is queried and the results merged, tagged with
// which cluster produced them.
func TestListAlertsAggregatesAcrossClusters(t *testing.T) {
	am1 := fakeAlertmanager(t, "AuthentikBenchPostgresDown")
	defer am1.Close()
	am2 := fakeAlertmanager(t, "AuthentikBenchServiceScrapeDown", "AuthentikBenchWorkerTaskErrors")
	defer am2.Close()

	wc1 := &benchv1alpha1.WorkloadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "wc1", Namespace: testNamespace},
		Status:     benchv1alpha1.WorkloadClusterStatus{AlertmanagerURL: am1.URL},
	}
	wc2 := &benchv1alpha1.WorkloadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "wc2", Namespace: testNamespace},
		Status:     benchv1alpha1.WorkloadClusterStatus{AlertmanagerURL: am2.URL},
	}
	// Not yet reconciled - must be skipped, not treated as an error.
	wc3 := &benchv1alpha1.WorkloadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "wc3", Namespace: testNamespace},
	}

	s, _ := newServer(t, Config{}, wc1, wc2, wc3)
	rec := do(t, s, http.MethodGet, "/api/v1/alerts", nil)
	requireStatus(t, rec, http.StatusOK)

	body := decodeBody[struct {
		Items []Alert `json:"items"`
	}](t, rec)
	if len(body.Items) != 3 {
		t.Fatalf("got %d alerts, want 3: %+v", len(body.Items), body.Items)
	}
	byCluster := map[string]int{}
	for _, a := range body.Items {
		byCluster[a.Cluster]++
		if a.State != "active" {
			t.Errorf("alert %q state = %q, want active", a.Labels["alertname"], a.State)
		}
	}
	if byCluster["wc1"] != 1 || byCluster["wc2"] != 2 {
		t.Fatalf("got counts %v, want wc1=1 wc2=2", byCluster)
	}
}

// TestListAlertsSkipsUnreachableCluster guards the other real design
// decision: one cluster's Alertmanager being unreachable must not blank out
// every other cluster's alerts.
func TestListAlertsSkipsUnreachableCluster(t *testing.T) {
	am := fakeAlertmanager(t, "AuthentikBenchPostgresDown")
	defer am.Close()

	reachable := &benchv1alpha1.WorkloadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "reachable", Namespace: testNamespace},
		Status:     benchv1alpha1.WorkloadClusterStatus{AlertmanagerURL: am.URL},
	}
	unreachable := &benchv1alpha1.WorkloadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "unreachable", Namespace: testNamespace},
		Status:     benchv1alpha1.WorkloadClusterStatus{AlertmanagerURL: "http://127.0.0.1:1"},
	}

	s, _ := newServer(t, Config{}, reachable, unreachable)
	rec := do(t, s, http.MethodGet, "/api/v1/alerts", nil)
	requireStatus(t, rec, http.StatusOK)

	body := decodeBody[struct {
		Items []Alert `json:"items"`
	}](t, rec)
	if len(body.Items) != 1 {
		t.Fatalf("got %d alerts, want 1 (from the reachable cluster only): %+v", len(body.Items), body.Items)
	}
	if body.Items[0].Cluster != "reachable" {
		t.Fatalf("alert came from cluster %q, want reachable", body.Items[0].Cluster)
	}
}
