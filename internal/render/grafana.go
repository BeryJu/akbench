package render

import (
	_ "embed"
	"fmt"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// Grafana is installed per workload cluster (same as Prometheus/Pyroscope/
// Tempo), reached from the web UI's Grafana page through cmd/apiserver's
// own reverse proxy (internal/api/grafana_proxy.go), not the k8s API
// server's service-proxy subresource - that subresource content-rewrites
// absolute paths in text/html responses, unconditionally prepending its
// own mount prefix, which breaks Grafana's own already-prefixed absolute
// paths (confirmed live). cmd/apiserver's reverse proxy is a single hop to
// Grafana's NodePort (GrafanaNodePort below), sidestepping that entirely. Not the
// separate, unrelated BENCH_GRAFANA_URL mechanism (internal/grafana/annotate.go,
// used only for posting TestRun annotations), which assumes one central,
// directly browser-reachable Grafana and is left alone. The embed-URL
// allowlist this comment used to also reference (internal/api/grafana.go)
// was deleted once this reverse-proxy design replaced it.
const (
	GrafanaServiceName = "grafana"
	GrafanaPort        = 3000

	// GrafanaNodePort exposes Grafana outside its own cluster the same way
	// Prometheus's own NodePort does (PrometheusNodePort) - what
	// cmd/apiserver's reverse proxy (internal/api/grafana_proxy.go)
	// connects to, resolved via WorkloadClusterStatus.GrafanaURL. The
	// chart's Service is a NodePort itself, so there's no separate one.
	GrafanaNodePort = 30910

	// GrafanaAdminUser is the one account Grafana has - see
	// GrafanaHelmRelease's doc comment for why anonymous access was
	// replaced with this.
	GrafanaAdminUser = "admin"

	grafanaDashboardsPath = "/var/lib/grafana/dashboards/benchmarks"
)

// GrafanaAdminSecretName is the *management*-cluster Secret
// (WorkloadClusterReconciler's own namespace, via ensureRandomSecret) holding
// this workload cluster's generated Grafana admin password under key
// "password" - readable by cmd/apiserver (internal/api/grafana_proxy.go,
// traces.go) without it needing a client to the workload cluster itself,
// unlike every other per-cluster credential in this project, which normally
// only the operator ever reads.
func GrafanaAdminSecretName(wc *benchv1alpha1.WorkloadCluster) string {
	return wc.Name + "-grafana-admin"
}

// GrafanaProxyPathPrefix is cmd/apiserver's own fixed mount for this
// workload cluster's Grafana (internal/api/grafana_proxy.go's
// /grafana-proxy/{cluster}/ route) - deterministic from wc.Name alone, so
// the operator can bake it into Grafana's own GF_SERVER_ROOT_URL at render
// time without any out-of-band coordination. A single proxy hop, unlike
// the abandoned k8s-API-proxy design this replaced - see this file's own
// package-level doc comment for why that mattered.
func GrafanaProxyPathPrefix(wc *benchv1alpha1.WorkloadCluster) string {
	return fmt.Sprintf("/grafana-proxy/%s/", wc.Name)
}

// TempoDatasourceUID is the Tempo datasource's fixed uid in
// grafanaDatasources below - cmd/apiserver queries Tempo through Grafana's
// own datasource proxy (/api/datasources/proxy/uid/<uid>/...), since Tempo
// itself is only a ClusterIP Service inside the workload cluster.
const TempoDatasourceUID = "tempo"

// PyroscopeDatasourceUID is the Pyroscope datasource's fixed uid, queried
// through Grafana's datasource proxy the same way as TempoDatasourceUID.
const PyroscopeDatasourceUID = "pyroscope"

// grafanaDatasources is adapted directly from the reference harness's own
// provisioning YAML (goauthentik/benchmark@dev:roles/metrics/files/grafana/datasources,
// templates/datasources/prometheus.yml.j2) - same datasource shape, only the
// hostnames changed to this project's own Service names. No Loki datasource:
// unlike the reference harness, nothing here deploys Loki. Tempo's
// tracesToLogsV2 (which pivoted from a trace to Loki logs) is dropped for
// the same reason.
func grafanaDatasources() map[string]any {
	url := func(service string, port int) string {
		return fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", service, ObservabilityNamespace, port)
	}
	return map[string]any{
		"apiVersion": 1,
		"datasources": []map[string]any{
			{
				"name": "Prometheus", "type": "prometheus", "access": "proxy", "orgId": 1, "uid": "prometheus",
				"url":      url(PrometheusServiceName, PrometheusPort),
				"jsonData": map[string]any{"timeInterval": "15s"},
			},
			{
				"name": "Pyroscope", "type": "grafana-pyroscope-datasource", "access": "proxy", "orgId": 1, "uid": PyroscopeDatasourceUID,
				"url": url(PyroscopeServiceName, PyroscopePort),
			},
			{
				"name": "Tempo", "type": "tempo", "access": "proxy", "orgId": 1, "uid": TempoDatasourceUID,
				"url": url(TempoServiceName, TempoQueryPort),
				"jsonData": map[string]any{
					"serviceMap": map[string]any{"datasourceUid": "prometheus"},
					"nodeGraph":  map[string]any{"enabled": true},
				},
			},
		},
	}
}

// grafanaDashboardProviders matches the reference harness's own
// providers/dashboards.yml: a single file-based provider reading the
// dashboards the chart mounts from its "benchmarks" dashboards ConfigMap.
func grafanaDashboardProviders() map[string]any {
	return map[string]any{
		"apiVersion": 1,
		"providers": []map[string]any{{
			"name": "benchmarks", "orgId": 1, "folder": "", "type": "file",
			"disableDeletion": false, "allowUiUpdates": true, "updateIntervalSeconds": 30,
			"options": map[string]any{"path": grafanaDashboardsPath, "foldersFromFilesStructure": false},
		}},
	}
}

// The 5 dashboards are this project's own, already adapted to what this
// project actually collects - see manifests/grafana-dashboards/README.md for
// why they're hand-authored rather than ported from the reference harness's
// newer dashboard.grafana.app/v2-schema ones. The chart writes each into its
// dashboards ConfigMap verbatim (no Helm templating), so their own
// {{...}} legend formats are safe.
//
//go:embed manifests/grafana-dashboards/compare-tests.json
var grafanaDashboardCompareTests []byte

//go:embed manifests/grafana-dashboards/k6.json
var grafanaDashboardK6 []byte

//go:embed manifests/grafana-dashboards/authentik.json
var grafanaDashboardAuthentik []byte

//go:embed manifests/grafana-dashboards/postgres.json
var grafanaDashboardPostgres []byte

//go:embed manifests/grafana-dashboards/nodes.json
var grafanaDashboardNodes []byte

// GrafanaHelmRelease installs the single-replica Grafana every workload
// cluster gets, from its upstream chart. Its Service is a NodePort
// (GrafanaNodePort) purely so cmd/apiserver's reverse proxy
// (internal/api/grafana_proxy.go), running on a different cluster entirely,
// can reach it at all - which means anyone who can reach a node's IP on
// that port can reach Grafana directly, bypassing cmd/apiserver's own
// session/bearer-token gate. So Grafana requires real credentials: the
// admin password is generated once on the management cluster
// (GrafanaAdminSecretName, by ensureRandomSecret) and handed to the chart
// through valuesFrom rather than written into the HelmRelease. cmd/apiserver
// injects it as HTTP Basic auth on every request it proxies, so the iframe
// still "just works" with no login prompt, but reaching the NodePort by any
// other path hits Grafana's login screen. root_url/serve_from_sub_path are
// the usual Grafana-behind-a-reverse-proxy settings; allow_embedding drops
// the X-Frame-Options header that would otherwise block the UI's iframe.
func GrafanaHelmRelease(wc *benchv1alpha1.WorkloadCluster) (*helmv2.HelmRelease, error) {
	values := map[string]any{
		"fullnameOverride": GrafanaServiceName,
		"adminUser":        GrafanaAdminUser,
		"service": map[string]any{
			"type": "NodePort", "port": GrafanaPort, "nodePort": GrafanaNodePort,
		},
		"persistence":    chartPersistence(wc, GrafanaStorageSize),
		"serviceMonitor": map[string]any{"enabled": true},
		"testFramework":  map[string]any{"enabled": false},
		"grafana.ini": map[string]any{
			"server": map[string]any{
				"root_url":            GrafanaProxyPathPrefix(wc),
				"serve_from_sub_path": true,
			},
			"security":  map[string]any{"allow_embedding": true},
			"analytics": map[string]any{"reporting_enabled": false, "check_for_updates": false},
		},
		"datasources":        map[string]any{"datasources.yaml": grafanaDatasources()},
		"dashboardProviders": map[string]any{"dashboardproviders.yaml": grafanaDashboardProviders()},
		"dashboards": map[string]any{
			"benchmarks": map[string]any{
				"compare-tests": map[string]any{"json": string(grafanaDashboardCompareTests)},
				"k6":            map[string]any{"json": string(grafanaDashboardK6)},
				"authentik":     map[string]any{"json": string(grafanaDashboardAuthentik)},
				"postgres":      map[string]any{"json": string(grafanaDashboardPostgres)},
				"nodes":         map[string]any{"json": string(grafanaDashboardNodes)},
			},
		},
	}
	monitoringPlacement(wc, values)
	hr, err := observabilityHelmRelease(wc, GrafanaHelmReleaseName, "grafana", grafanaChartVersion, GrafanaCommunityHelmRepositoryName, values)
	if err != nil {
		return nil, err
	}
	hr.Spec.ValuesFrom = []helmv2.ValuesReference{{
		Kind: "Secret", Name: GrafanaAdminSecretName(wc), ValuesKey: "password", TargetPath: "adminPassword",
	}}
	return hr, nil
}
