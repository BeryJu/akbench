package render

import (
	_ "embed"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

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

	// GrafanaNodePortServiceName/GrafanaNodePort expose Grafana outside its
	// own cluster the same way Prometheus's own NodePort does
	// (PrometheusNodePortServiceName/PrometheusNodePort) - what
	// cmd/apiserver's reverse proxy (internal/api/grafana_proxy.go)
	// connects to, resolved via WorkloadClusterStatus.GrafanaURL.
	GrafanaNodePortServiceName = "grafana-nodeport"
	GrafanaNodePort            = 30910

	grafanaDatasourcesConfigMapName = GrafanaServiceName + "-datasources"
	grafanaDashboardsConfigMapName  = GrafanaServiceName + "-dashboards"
	grafanaProvidersConfigMapName   = GrafanaServiceName + "-providers"

	// GrafanaAdminUser is the one account Grafana has - see GrafanaObjects'
	// own doc comment for why anonymous access was replaced with this.
	GrafanaAdminUser = "admin"
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

func grafanaLabels() map[string]string {
	return map[string]string{labelKeyApp: GrafanaServiceName}
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

// grafanaDatasources is adapted directly from the reference harness's own
// provisioning YAML (goauthentik/benchmark@dev:roles/metrics/files/grafana/datasources,
// templates/datasources/prometheus.yml.j2) - same datasource shape, only the
// hostnames changed to this project's own Service names. No Loki datasource:
// unlike the reference harness, nothing here deploys Loki. Tempo's
// tracesToLogsV2 (which pivoted from a trace to Loki logs) is dropped for
// the same reason.
func grafanaDatasources() string {
	return fmt.Sprintf(`apiVersion: 1
datasources:
  - name: Prometheus
    type: prometheus
    access: proxy
    orgId: 1
    uid: prometheus
    url: http://%s.%s.svc.cluster.local:%d
    jsonData:
      timeInterval: 15s
  - name: Pyroscope
    type: grafana-pyroscope-datasource
    access: proxy
    orgId: 1
    uid: pyroscope
    url: http://%s.%s.svc.cluster.local:%d
  - name: Tempo
    type: tempo
    access: proxy
    orgId: 1
    uid: tempo
    url: http://%s.%s.svc.cluster.local:%d
    jsonData:
      serviceMap:
        datasourceUid: prometheus
      nodeGraph:
        enabled: true
`,
		PrometheusServiceName, ObservabilityNamespace, PrometheusPort,
		PyroscopeServiceName, ObservabilityNamespace, PyroscopePort,
		TempoServiceName, ObservabilityNamespace, TempoQueryPort,
	)
}

// grafanaDashboardProvider matches the reference harness's own
// providers/dashboards.yml: a single file-based provider reading whatever's
// mounted at /var/lib/grafana/dashboards.
const grafanaDashboardProvider = `apiVersion: 1
providers:
  - name: benchmarks
    orgId: 1
    type: file
    disableDeletion: false
    allowUiUpdates: true
    updateIntervalSeconds: 30
    options:
      path: /var/lib/grafana/dashboards
      foldersFromFilesStructure: false
`

// The 5 dashboards are this project's own (gitops/dashboards/*.json,
// already adapted to what this project actually collects - see that
// directory's own README for why they're hand-authored rather than ported
// from the reference harness's newer dashboard.grafana.app/v2-schema ones),
// vendored here verbatim since go:embed can't reach outside its own
// package's directory tree. Keep these two copies in sync if either changes.
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

// GrafanaObjects returns the single-replica, no-HA Grafana
// WorkloadClusterReconciler installs onto every workload cluster: datasource/
// dashboard-provider/dashboard ConfigMaps, a Deployment, and a ClusterIP
// Service. Also has a NodePort Service (GrafanaNodePortServiceName) purely so
// cmd/apiserver's reverse proxy (internal/api/grafana_proxy.go), running on a
// different cluster entirely, can reach it at all - which means anyone who
// can reach a node's IP on that port can reach Grafana directly, bypassing
// cmd/apiserver's own session/bearer-token gate entirely. This used to be
// covered by anonymous Admin auth (matching the reference harness's own
// compose.yml, which never needed to consider this - its Grafana was never
// multi-hop), which made that bypass a full unauthenticated admin panel.
// Grafana now requires real credentials (GF_SECURITY_ADMIN_USER/PASSWORD,
// adminPassword generated once by ensureRandomSecret and never sent to a
// browser): cmd/apiserver injects them as HTTP Basic auth on every request it
// proxies (see stripProxyCredentials's caller), so the iframe still "just
// works" with no login prompt, but reaching this NodePort by any other path
// now hits Grafana's own login screen instead of an open admin session.
// GF_SERVER_ROOT_URL/GF_SERVER_SERVE_FROM_SUB_PATH are required once Grafana
// is reached through a proxy subpath - a well-known
// Grafana-behind-a-reverse-proxy requirement, not something specific to this
// proxy mechanism - otherwise its own asset/API links break once loaded.
func GrafanaObjects(wc *benchv1alpha1.WorkloadCluster, adminPassword string) []client.Object {
	labels := grafanaLabels()
	rootURL := GrafanaProxyPathPrefix(wc)
	return []client.Object{
		&corev1.Namespace{
			Kind: kindNamespace, APIVersion: apiVersionV1,
			Name: ObservabilityNamespace,
		},
		&corev1.ConfigMap{
			Kind: kindConfigMap, APIVersion: apiVersionV1,
			Name: grafanaDatasourcesConfigMapName, Namespace: ObservabilityNamespace,
			Data: map[string]string{"datasources.yaml": grafanaDatasources()},
		},
		&corev1.ConfigMap{
			Kind: kindConfigMap, APIVersion: apiVersionV1,
			Name: grafanaProvidersConfigMapName, Namespace: ObservabilityNamespace,
			Data: map[string]string{"dashboards.yaml": grafanaDashboardProvider},
		},
		&corev1.ConfigMap{
			Kind: kindConfigMap, APIVersion: apiVersionV1,
			Name: grafanaDashboardsConfigMapName, Namespace: ObservabilityNamespace,
			Data: map[string]string{
				"compare-tests.json": string(grafanaDashboardCompareTests),
				"k6.json":            string(grafanaDashboardK6),
				"authentik.json":     string(grafanaDashboardAuthentik),
				"postgres.json":      string(grafanaDashboardPostgres),
				"nodes.json":         string(grafanaDashboardNodes),
			},
		},
		&appsv1.Deployment{
			Kind: kindDeployment, APIVersion: apiVersionAppsV1,
			Name: GrafanaServiceName, Namespace: ObservabilityNamespace, Labels: labels,
			Spec: appsv1.DeploymentSpec{
				Replicas: new(int32(1)),
				Selector: &metav1.LabelSelector{MatchLabels: labels},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: labels},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "grafana",
								Image: "grafana/grafana:11.4.0",
								Env: []corev1.EnvVar{
									{Name: "GF_SECURITY_ADMIN_USER", Value: GrafanaAdminUser},
									{Name: "GF_SECURITY_ADMIN_PASSWORD", Value: adminPassword},
									// Grafana sends "X-Frame-Options: deny" on every
									// response by default (confirmed live: curl -I
									// against a stock grafana/grafana:11.4.0 container) -
									// its own clickjacking protection, blocking iframe
									// embedding from any origin whatsoever, not a CORS
									// concern at all (no Access-Control-* header is
									// involved). allow_embedding is the one documented
									// switch that removes it - confirmed live that setting
									// it drops the header entirely.
									{Name: "GF_SECURITY_ALLOW_EMBEDDING", Value: "true"},
									{Name: "GF_SERVER_ROOT_URL", Value: rootURL},
									// "true" is correct here: per Grafana's own docs,
									// serve_from_sub_path=true means the reverse proxy
									// does NOT strip root_url's own path prefix before
									// forwarding to Grafana - exactly what
									// internal/api/grafana_proxy.go's reverse proxy
									// does (it deliberately preserves the full request
									// path). This was "false" in an earlier version of
									// this file when Grafana was reached through the
									// k8s API server's own service-proxy subresource
									// instead, which *does* strip its own mount prefix
									// - a materially different proxy, not just a config
									// tweak; see this file's own package doc comment.
									{Name: "GF_SERVER_SERVE_FROM_SUB_PATH", Value: "true"},
								},
								Ports: []corev1.ContainerPort{{Name: portNameHTTP, ContainerPort: GrafanaPort}},
								VolumeMounts: []corev1.VolumeMount{
									{Name: "datasources", MountPath: "/etc/grafana/provisioning/datasources"},
									{Name: "providers", MountPath: "/etc/grafana/provisioning/dashboards"},
									{Name: "dashboards", MountPath: "/var/lib/grafana/dashboards"},
								},
							},
						},
						Volumes: []corev1.Volume{
							{
								Name: "datasources",
								ConfigMap: &corev1.ConfigMapVolumeSource{
									Name: grafanaDatasourcesConfigMapName,
								},
							},
							{
								Name: "providers",
								ConfigMap: &corev1.ConfigMapVolumeSource{
									Name: grafanaProvidersConfigMapName,
								},
							},
							{
								Name: "dashboards",
								ConfigMap: &corev1.ConfigMapVolumeSource{
									Name: grafanaDashboardsConfigMapName,
								},
							},
						},
					},
				},
			},
		},
		&corev1.Service{
			Kind: kindService, APIVersion: apiVersionV1,
			Name: GrafanaServiceName, Namespace: ObservabilityNamespace, Labels: labels,
			Spec: corev1.ServiceSpec{
				Selector: labels,
				Ports:    []corev1.ServicePort{{Name: portNameHTTP, Port: GrafanaPort, TargetPort: intstr.FromInt32(GrafanaPort)}},
			},
		},
		&corev1.Service{
			Kind: kindService, APIVersion: apiVersionV1,
			Name: GrafanaNodePortServiceName, Namespace: ObservabilityNamespace,
			Spec: corev1.ServiceSpec{
				Type:     corev1.ServiceTypeNodePort,
				Selector: labels,
				Ports: []corev1.ServicePort{
					{Name: portNameHTTP, Port: GrafanaPort, TargetPort: intstr.FromInt32(GrafanaPort), NodePort: int32(GrafanaNodePort)},
				},
			},
		},
		// Grafana serves its own Prometheus-format /metrics on its main
		// "http" port - confirmed live (docker run grafana/grafana:11.4.0,
		// curl :3000/metrics returns real go_*/deprecated_flags_inuse_total
		// series). Selects the plain ClusterIP Service above (it carries
		// these same labels); the NodePort twin isn't labeled, so it's
		// skipped rather than scraped twice.
		newServiceMonitor(GrafanaServiceName, labels, portNameHTTP),
	}
}
