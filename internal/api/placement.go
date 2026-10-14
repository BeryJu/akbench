package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	benchv1alpha1 "github.com/authentik-labs/akbench/api/v1alpha1"
)

// PodPlacement is one running pod of an AuthentikDeployment and the node it
// was scheduled onto.
type PodPlacement struct {
	Deployment string `json:"deployment"`
	Cluster    string `json:"cluster"`
	Node       string `json:"node"`
	Pod        string `json:"pod"`
}

// placementQuery reads pod->node from kube-state-metrics' kube_pod_info
// rather than the pods themselves - the REST API only talks to the
// management cluster (see server.go), but every cluster's Prometheus scrapes
// its own kube-state-metrics. Job pods (blueprint import, postgres role fix)
// are excluded, same as resourceMetrics.
const placementQuery = `kube_pod_info{namespace=%[1]q, node!=""} and on (pod) kube_pod_owner{namespace=%[1]q, owner_kind!="Job"}`

// listPlacement answers GET /placement: where every deployment's pods
// landed. A deployment whose cluster's Prometheus is unresolved or
// unreachable is skipped, same as listAlerts - one cluster having a bad day
// shouldn't blank the rest.
func (s *Server) listPlacement(w http.ResponseWriter, r *http.Request) {
	var deployments benchv1alpha1.AuthentikDeploymentList
	if err := s.client.List(r.Context(), &deployments, client.InNamespace(s.cfg.Namespace)); err != nil {
		writeAPIError(w, r, err)
		return
	}
	pods := []PodPlacement{}
	for _, d := range deployments.Items {
		cluster := d.Spec.ClusterRef.Name
		promURL := s.prometheusURLForCluster(r.Context(), cluster)
		if cluster == "" {
			cluster = ManagementClusterName
		}
		if promURL == "" {
			continue
		}
		ctx, cancel := context.WithTimeout(r.Context(), alertmanagerFetchTimeout)
		samples, err := s.promInstantMulti(ctx, promURL, fmt.Sprintf(placementQuery, d.Spec.Namespace), "")
		cancel()
		if err != nil {
			continue
		}
		for _, sample := range samples {
			pods = append(pods, PodPlacement{Deployment: d.Name, Cluster: cluster, Node: sample.Labels["node"], Pod: sample.Labels["pod"]})
		}
	}
	slices.SortFunc(pods, func(a, b PodPlacement) int {
		return strings.Compare(a.Cluster+"/"+a.Node+"/"+a.Pod, b.Cluster+"/"+b.Node+"/"+b.Pod)
	})
	writeItems(w, r, pods)
}

// prometheusURLForCluster is the Prometheus scraping a deployment's cluster:
// the central one for the management cluster (""), else the
// WorkloadCluster's own - "" if it hasn't reconciled one yet.
func (s *Server) prometheusURLForCluster(ctx context.Context, cluster string) string {
	if cluster == "" {
		return s.cfg.PrometheusURL
	}
	var wc benchv1alpha1.WorkloadCluster
	if err := s.client.Get(ctx, types.NamespacedName{Name: cluster, Namespace: s.cfg.Namespace}, &wc); err != nil {
		return ""
	}
	return wc.Status.PrometheusURL
}
