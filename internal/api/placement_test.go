package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	benchv1alpha1 "github.com/authentik-labs/akbench/api/v1alpha1"
)

func TestListPlacementReadsNodeFromKubePodInfo(t *testing.T) {
	var gotQuery string
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("query")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{"resultType": "vector", "result": []any{
				map[string]any{"metric": map[string]string{"node": "n2", "pod": "topo-worker-0"}, "value": []any{1700000000, "1"}},
				map[string]any{"metric": map[string]string{"node": "n1", "pod": "topo-server-0"}, "value": []any{1700000000, "1"}},
			}},
		})
	}))
	t.Cleanup(prom.Close)

	wc := &benchv1alpha1.WorkloadCluster{}
	wc.Name = "wc1"
	wc.Namespace = testNamespace
	wc.Status.PrometheusURL = prom.URL
	deployment := sampleDeployment("topo")
	deployment.Spec.ClusterRef.Name = "wc1"
	// A deployment whose cluster has no Prometheus yet is skipped, not fatal.
	pending := sampleDeployment("pending")
	pending.Spec.ClusterRef.Name = "missing"

	s, _ := newServer(t, Config{}, wc, deployment, pending)
	rec := do(t, s, http.MethodGet, "/api/v1/placement", nil)
	requireStatus(t, rec, http.StatusOK)

	body := decodeBody[struct {
		Items []PodPlacement `json:"items"`
	}](t, rec)
	want := []PodPlacement{
		{Deployment: "topo", Cluster: "wc1", Node: "n1", Pod: "topo-server-0"},
		{Deployment: "topo", Cluster: "wc1", Node: "n2", Pod: "topo-worker-0"},
	}
	if len(body.Items) != len(want) {
		t.Fatalf("items = %+v, want %+v", body.Items, want)
	}
	for i := range want {
		if body.Items[i] != want[i] {
			t.Errorf("items[%d] = %+v, want %+v", i, body.Items[i], want[i])
		}
	}
	if !strings.Contains(gotQuery, `namespace="ak-topo"`) || !strings.Contains(gotQuery, `owner_kind!="Job"`) {
		t.Errorf("query = %q, want it scoped to ak-topo without Job pods", gotQuery)
	}
}
