package render

import (
	helmv2 "github.com/fluxcd/helm-controller/api/v2"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// TempoServiceName/TempoOTLPHTTPPort must match defaultTracingEndpoint
// (internal/controller/authentikdeployment_controller.go:
// "http://tempo.bench-system.svc.cluster.local:4318") exactly - every
// authentik pod with errorReporting.enabled exports traces there
// regardless of which cluster it runs on, so each workload cluster's own
// Tempo must answer at this exact name. The chart's fullnameOverride keeps
// its Service named this.
const (
	TempoServiceName  = "tempo"
	TempoOTLPHTTPPort = 4318
	// TempoQueryPort is Tempo's own HTTP API - what Grafana's Tempo
	// datasource (and, through it, cmd/apiserver's trace breakdown) queries.
	TempoQueryPort = 3200
)

// TempoHelmRelease installs Tempo from its upstream chart: single binary,
// local trace storage on a persistent volume. The chart's default receivers
// include OTLP over HTTP on TempoOTLPHTTPPort, which is all authentik
// exports. Usage reporting to Grafana Labs is off.
func TempoHelmRelease(wc *benchv1alpha1.WorkloadCluster) (*helmv2.HelmRelease, error) {
	values := map[string]any{
		"fullnameOverride": TempoServiceName,
		"tempo":            map[string]any{"reportingEnabled": false},
		"persistence":      chartPersistence(wc, TempoStorageSize),
		"serviceMonitor":   map[string]any{"enabled": true},
	}
	monitoringPlacement(wc, values)
	return observabilityHelmRelease(wc, TempoHelmReleaseName, "tempo", tempoChartVersion, GrafanaCommunityHelmRepositoryName, values)
}
