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

package render

import (
	_ "embed"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// Grafana is installed per workload cluster (same as Prometheus/Pyroscope/
// Tempo), reached from Headlamp's own Grafana tab via the k8s API server's
// service-proxy subresource directly (CLUSTERS_PREFIX in the installed
// Headlamp SDK, confirmed against its own k8s/api/v1/constants.js) - not a
// new cmd/apiserver reverse-proxy, and not the separate, unrelated
// Config.GrafanaURL/BENCH_GRAFANA_URL mechanisms (internal/api/grafana.go,
// internal/grafana/annotate.go), both of which assume one directly
// browser-reachable Grafana and are left alone.
const (
	GrafanaServiceName = "grafana"
	GrafanaPort        = 3000

	grafanaDatasourcesConfigMapName = GrafanaServiceName + "-datasources"
	grafanaDashboardsConfigMapName  = GrafanaServiceName + "-dashboards"
	grafanaProvidersConfigMapName   = GrafanaServiceName + "-providers"
)

func grafanaLabels() map[string]string {
	return map[string]string{"app": GrafanaServiceName}
}

// grafanaProxyPathPrefix is where this workload cluster's Grafana is
// reachable through Headlamp's own per-cluster k8s-API proxy - deterministic
// from wc.Name alone (matching headlampClusterName()'s own
// "<name>-admin@<name>" kubeconfig-context convention in
// web/headlamp-plugin/src/api.ts, confirmed live against a real context
// name), so the operator can bake it into Grafana's own GF_SERVER_ROOT_URL
// at render time without any out-of-band coordination.
func grafanaProxyPathPrefix(wc *benchv1alpha1.WorkloadCluster) string {
	cluster := fmt.Sprintf("%s-admin@%s", wc.Name, wc.Name)
	return fmt.Sprintf("/clusters/%s/api/v1/namespaces/%s/services/%s:%d/proxy/",
		cluster, ObservabilityNamespace, GrafanaServiceName, GrafanaPort)
}

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
// Service (no NodePort - reached through the k8s API proxy chain, not
// directly). Anonymous Admin auth (GF_AUTH_ANONYMOUS_ENABLED/
// GF_AUTH_ANONYMOUS_ORG_ROLE, matching the reference harness's own
// compose.yml) is what makes embedding a dashboard in an iframe work at all
// without a login redirect. GF_SERVER_ROOT_URL/GF_SERVER_SERVE_FROM_SUB_PATH
// are required once Grafana is reached through a proxy subpath - a
// well-known Grafana-behind-a-reverse-proxy requirement, not something
// specific to this proxy mechanism - otherwise its own asset/API links
// break once loaded.
func GrafanaObjects(wc *benchv1alpha1.WorkloadCluster) []client.Object {
	labels := grafanaLabels()
	rootURL := grafanaProxyPathPrefix(wc)
	return []client.Object{
		&corev1.Namespace{
			TypeMeta:   metav1.TypeMeta{Kind: "Namespace", APIVersion: "v1"},
			ObjectMeta: metav1.ObjectMeta{Name: ObservabilityNamespace},
		},
		&corev1.ConfigMap{
			TypeMeta:   metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
			ObjectMeta: metav1.ObjectMeta{Name: grafanaDatasourcesConfigMapName, Namespace: ObservabilityNamespace},
			Data:       map[string]string{"datasources.yaml": grafanaDatasources()},
		},
		&corev1.ConfigMap{
			TypeMeta:   metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
			ObjectMeta: metav1.ObjectMeta{Name: grafanaProvidersConfigMapName, Namespace: ObservabilityNamespace},
			Data:       map[string]string{"dashboards.yaml": grafanaDashboardProvider},
		},
		&corev1.ConfigMap{
			TypeMeta:   metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
			ObjectMeta: metav1.ObjectMeta{Name: grafanaDashboardsConfigMapName, Namespace: ObservabilityNamespace},
			Data: map[string]string{
				"compare-tests.json": string(grafanaDashboardCompareTests),
				"k6.json":            string(grafanaDashboardK6),
				"authentik.json":     string(grafanaDashboardAuthentik),
				"postgres.json":      string(grafanaDashboardPostgres),
				"nodes.json":         string(grafanaDashboardNodes),
			},
		},
		&appsv1.Deployment{
			TypeMeta:   metav1.TypeMeta{Kind: "Deployment", APIVersion: "apps/v1"},
			ObjectMeta: metav1.ObjectMeta{Name: GrafanaServiceName, Namespace: ObservabilityNamespace, Labels: labels},
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To(int32(1)),
				Selector: &metav1.LabelSelector{MatchLabels: labels},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: labels},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "grafana",
								Image: "grafana/grafana:11.4.0",
								Env: []corev1.EnvVar{
									{Name: "GF_AUTH_ANONYMOUS_ENABLED", Value: "true"},
									{Name: "GF_AUTH_ANONYMOUS_ORG_ROLE", Value: "Admin"},
									{Name: "GF_SERVER_ROOT_URL", Value: rootURL},
									// Deliberately NOT "true": per Grafana's own docs,
									// serve_from_sub_path=true means Grafana expects
									// incoming requests to still carry the root_url's
									// path prefix, and 301-redirects to add it back
									// when they don't - confirmed live, this produced
									// an infinite redirect loop. The k8s API server's
									// own service-proxy subresource already strips its
									// mount prefix before forwarding (the same thing
									// Grafana's docs describe as "sub-path stripping...
									// done by the reverse proxy"), so Grafana must be
									// left at its default (false): treat every incoming
									// request as mounted at "/", while root_url alone
									// still makes it emit correct absolute links for
									// the browser.
								},
								Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: GrafanaPort}},
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
								VolumeSource: corev1.VolumeSource{
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{Name: grafanaDatasourcesConfigMapName},
									},
								},
							},
							{
								Name: "providers",
								VolumeSource: corev1.VolumeSource{
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{Name: grafanaProvidersConfigMapName},
									},
								},
							},
							{
								Name: "dashboards",
								VolumeSource: corev1.VolumeSource{
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{Name: grafanaDashboardsConfigMapName},
									},
								},
							},
						},
					},
				},
			},
		},
		&corev1.Service{
			TypeMeta:   metav1.TypeMeta{Kind: "Service", APIVersion: "v1"},
			ObjectMeta: metav1.ObjectMeta{Name: GrafanaServiceName, Namespace: ObservabilityNamespace},
			Spec: corev1.ServiceSpec{
				Selector: labels,
				Ports:    []corev1.ServicePort{{Name: "http", Port: GrafanaPort, TargetPort: intstr.FromInt32(GrafanaPort)}},
			},
		},
	}
}
