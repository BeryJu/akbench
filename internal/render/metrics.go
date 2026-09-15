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
	"encoding/json"
	"fmt"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// ObservabilityNamespace is where every per-workload-cluster
// Prometheus/Pyroscope/Tempo instance lives, matching the namespace this
// project's own dev scripts (hack/dev-prometheus.yaml) already used.
const ObservabilityNamespace = "bench-system"

// PrometheusServiceName matches defaultRemoteWriteURL
// (internal/controller/testcase_controller.go): k6 workloads push to
// "prometheus-server.bench-system.svc.cluster.local" regardless of which
// cluster they run on. Prometheus itself is now installed via the
// kube-prometheus-stack chart (see KubePrometheusStackHelmRelease) rather
// than a hand-rolled Deployment, so this exact Service - along with
// PrometheusNodePortServiceName below - is rendered as a small
// project-authored compatibility object (prometheusCompatServices,
// injected into the same HelmRelease via its own extraManifests value)
// selecting the chart-managed Prometheus pod directly: every other
// consumer of these names (Grafana's datasource, k6's remote-write URL,
// cmd/apiserver's cross-cluster resolution) needed zero changes as a
// result.
const PrometheusServiceName = "prometheus-server"

// PrometheusPort is the port Prometheus itself listens on (the chart's own
// default, unchanged from the prior hand-rolled install), and also the port
// the plain ClusterIP Service exposes as "80" for defaultRemoteWriteURL's
// own http://prometheus-server.../api/v1/write (no :9090 in that URL).
const PrometheusPort = 9090

// PrometheusNodePortServiceName/PrometheusNodePort expose the same
// Prometheus outside its own cluster: a management-cluster pod can reach a
// workload cluster's node IPs directly (confirmed live - the reverse
// direction can't), so cmd/apiserver reaches this NodePort at
// <a node's InternalIP>:PrometheusNodePort rather than the cluster-relative
// DNS name k6 uses, which only resolves from inside that same cluster.
// Pinned rather than left to auto-assignment so WorkloadClusterReconciler
// never has to read the Service back just to learn its own port.
const (
	PrometheusNodePortServiceName = "prometheus-server-nodeport"
	PrometheusNodePort            = 30909
)

// kubePrometheusStack's own release/fullname is pinned to a fixed,
// predictable value (cleanPrometheusOperatorObjectNames below drops the
// chart's default "-prometheus" suffix from the generated Prometheus CR
// name) purely so prometheusPodLabels - the selector the two compatibility
// Services above use to reach the chart-managed Prometheus pod - doesn't
// have to guess at Helm's own name-templating rules.
const (
	kubePrometheusStackHelmRepositoryName = "kube-prometheus-stack"
	kubePrometheusStackChartName          = "kube-prometheus-stack"
	kubePrometheusStackChartRepoURL       = "https://prometheus-community.github.io/helm-charts"

	// kubePrometheusStackChartVersion is pinned - matches the chart version
	// last verified working; bump deliberately.
	kubePrometheusStackChartVersion = "91.4.0"

	KubePrometheusStackHelmReleaseName = "kube-prometheus-stack"
	kubePrometheusStackFullname        = "kube-prometheus-stack"
)

// KubePrometheusStackHelmRepository is the (singleton, per-namespace)
// HelmRepository pointing at the official prometheus-community chart repo.
func KubePrometheusStackHelmRepository(namespace string) *sourcev1.HelmRepository {
	return &sourcev1.HelmRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kubePrometheusStackHelmRepositoryName,
			Namespace: namespace,
		},
		Spec: sourcev1.HelmRepositorySpec{
			URL:      kubePrometheusStackChartRepoURL,
			Interval: metav1.Duration{Duration: helmRepoInterval},
		},
	}
}

// prometheusPodLabels are the labels prometheus-operator itself puts on
// every pod belonging to the Prometheus custom resource this chart renders
// (confirmed directly against the chart's own templates/prometheus/
// service.yaml selector, not assumed) - app.kubernetes.io/instance equals
// the Prometheus CR's own name, which cleanPrometheusOperatorObjectNames
// (set true below) makes equal to kubePrometheusStackFullname exactly,
// rather than fullname + "-prometheus".
func prometheusPodLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":     "prometheus",
		"app.kubernetes.io/instance": kubePrometheusStackFullname,
	}
}

// prometheusCompatServices are the two Services every other consumer in
// this codebase already expects by name (see PrometheusServiceName's own
// doc comment) - not part of the chart itself, injected into the same
// HelmRelease via its own extraManifests value so one install produces
// everything, rather than a second apply step against the remote cluster.
func prometheusCompatServices() []map[string]any {
	labels := prometheusPodLabels()
	clusterIP := &corev1.Service{
		TypeMeta:   metav1.TypeMeta{Kind: "Service", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{Name: PrometheusServiceName, Namespace: ObservabilityNamespace},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 80, TargetPort: intstr.FromInt32(PrometheusPort)},
				{Name: "prom-native", Port: PrometheusPort, TargetPort: intstr.FromInt32(PrometheusPort)},
			},
		},
	}
	nodePort := &corev1.Service{
		TypeMeta:   metav1.TypeMeta{Kind: "Service", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{Name: PrometheusNodePortServiceName, Namespace: ObservabilityNamespace},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeNodePort,
			Selector: labels,
			Ports: []corev1.ServicePort{
				{Name: "http", Port: PrometheusPort, TargetPort: intstr.FromInt32(PrometheusPort), NodePort: PrometheusNodePort},
			},
		},
	}
	out := make([]map[string]any, 0, 2)
	for _, svc := range []*corev1.Service{clusterIP, nodePort} {
		m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(svc)
		if err != nil {
			// svc is a fixed, entirely-literal object with no user input -
			// a conversion failure here would mean this function itself is
			// broken, not a runtime condition any caller could recover from.
			panic(fmt.Sprintf("convert compat service %q to unstructured: %v", svc.Name, err))
		}
		out = append(out, m)
	}
	return out
}

// KubePrometheusStackHelmValues builds spec.values for the
// kube-prometheus-stack chart install:
//   - enableRemoteWriteReceiver, for k6's own experimental-prometheus-rw
//     output. Native-histogram ingestion (K6_PROMETHEUS_RW_TREND_AS_NATIVE_HISTOGRAM=true)
//     needed no matching --enable-feature flag here - confirmed directly
//     against the chart's own pinned Prometheus image (v3.14.0-distroless)
//     that "native-histograms" isn't even a recognized feature name on
//     Prometheus v3.x (it shipped enabled by default there; the flag this
//     chart's own values.yaml exposes is for a different, still-experimental
//     set of features), unlike the hand-rolled v2.55.1 install this
//     replaces, which needed it explicitly.
//   - grafana.enabled/alertmanager.enabled both false: this project already
//     installs its own Grafana (render.GrafanaObjects, pre-wired with
//     Prometheus/Pyroscope/Tempo datasources) and has no alerting need
//     anywhere else, matching the "no HA, dev-scale, only what's actually
//     used" posture the rest of this addon set already follows.
//   - nodeExporter/kubeStateMetrics left at the chart's own enabled-by-default
//     value - node-exporter is the actual point of this chart (per-node CPU/
//     memory/disk/network metrics the hand-rolled install never had), and
//     kube-state-metrics comes along for free at no extra operational cost.
//   - extraManifests carries the two compatibility Services (see their own
//     doc comment) so this one HelmRelease produces everything needed.
//   - serviceMonitorSelectorNilUsesHelmValues/podMonitorSelectorNilUsesHelmValues
//     both false, with the chart's own default empty selectors left as-is:
//     the chart's own comment on the first key says a nil/{} value there
//     "will cause the prometheus resource to be created with selectors based
//     on values in the helm deployment" - i.e. restricted to ServiceMonitors/
//     PodMonitors carrying this release's own "release: kube-prometheus-stack"
//     label, which every ServiceMonitor/PodMonitor this operator creates
//     itself (see newServiceMonitor) or asks another chart to create (the
//     authentik chart's own server.metrics.serviceMonitor, the bundled
//     postgresql subchart's metrics.serviceMonitor, CNPG's
//     spec.monitoring.enablePodMonitor) does not carry. False here instead
//     leaves prometheusSpec.serviceMonitorSelector/podMonitorSelector at
//     their own already-empty-map chart defaults, which - confirmed directly
//     against the chart's own values.yaml comment - means "select
//     everything," in every namespace (serviceMonitorNamespaceSelector/
//     podMonitorNamespaceSelector already default to {} too).
func KubePrometheusStackHelmValues() (*apiextensionsv1.JSON, error) {
	raw, err := json.Marshal(map[string]any{
		"fullnameOverride":                   kubePrometheusStackFullname,
		"cleanPrometheusOperatorObjectNames": true,
		"grafana":                            map[string]any{"enabled": false},
		"alertmanager":                       map[string]any{"enabled": false},
		"prometheus": map[string]any{
			"prometheusSpec": map[string]any{
				"enableRemoteWriteReceiver":               true,
				"serviceMonitorSelectorNilUsesHelmValues": false,
				"podMonitorSelectorNilUsesHelmValues":     false,
			},
		},
		"extraManifests": prometheusCompatServices(),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal kube-prometheus-stack helm values: %w", err)
	}
	return &apiextensionsv1.JSON{Raw: raw}, nil
}

// KubePrometheusStackHelmRelease builds the Flux HelmRelease that installs
// kube-prometheus-stack onto a workload cluster remotely -
// kubeConfigSecretName is the "<cluster>-kubeconfig" Secret CAPI writes (see
// render.KubeconfigSecretName). CRDs default to Create-only on Helm's own
// install action and Skip on upgrade (see the fluxcd/helm-controller API's
// own doc comment on this) - both explicitly set to CreateReplace instead,
// since prometheus-operator's Prometheus/ServiceMonitor/PrometheusRule/etc.
// CRDs (bundled in the chart's own crds/ subchart) are exactly what this
// install exists to provide, and bumping kubePrometheusStackChartVersion
// later should actually update them rather than silently stay on whatever
// version first got installed.
func KubePrometheusStackHelmRelease(namespace, kubeConfigSecretName string, values *apiextensionsv1.JSON) *helmv2.HelmRelease {
	return &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      KubePrometheusStackHelmReleaseName,
			Namespace: namespace,
		},
		Spec: helmv2.HelmReleaseSpec{
			Interval:    metav1.Duration{Duration: helmRepoInterval},
			Timeout:     &metav1.Duration{Duration: helmInstallTimeout},
			ReleaseName: KubePrometheusStackHelmReleaseName,
			Chart: &helmv2.HelmChartTemplate{
				Spec: helmv2.HelmChartTemplateSpec{
					Chart:   kubePrometheusStackChartName,
					Version: kubePrometheusStackChartVersion,
					SourceRef: helmv2.CrossNamespaceObjectReference{
						Kind:      sourcev1.HelmRepositoryKind,
						Name:      kubePrometheusStackHelmRepositoryName,
						Namespace: namespace,
					},
				},
			},
			TargetNamespace: ObservabilityNamespace,
			Install:         &helmv2.Install{CreateNamespace: true, CRDs: helmv2.CreateReplace},
			Upgrade:         &helmv2.Upgrade{CRDs: helmv2.CreateReplace},
			Values:          values,
			KubeConfig: &fluxmeta.KubeConfigReference{
				SecretRef: &fluxmeta.SecretKeyReference{Name: kubeConfigSecretName},
			},
		},
	}
}
