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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ObservabilityNamespace is where every per-workload-cluster
// Prometheus/Pyroscope/Tempo instance lives, matching the namespace this
// project's own dev scripts (hack/dev-prometheus.yaml) already used.
const ObservabilityNamespace = "bench-system"

// PrometheusServiceName matches defaultRemoteWriteURL
// (internal/controller/testcase_controller.go): k6 workloads push to
// "prometheus-server.bench-system.svc.cluster.local" regardless of which
// cluster they run on, so every workload cluster's own Prometheus must use
// this exact name for that cluster-relative DNS name to resolve.
const PrometheusServiceName = "prometheus-server"

// PrometheusPort is the port Prometheus itself listens on, and also the
// port the plain ClusterIP Service exposes as "80" for
// defaultRemoteWriteURL's own http://prometheus-server.../api/v1/write
// (no :9090 in that URL).
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

func prometheusLabels() map[string]string {
	return map[string]string{"app": PrometheusServiceName}
}

// prometheusConfigMapName holds Prometheus' own config file. No scrape
// configs: every metric arrives via remote-write from k6, never scraped.
const prometheusConfigMapName = PrometheusServiceName + "-config"

// PrometheusObjects returns the single-replica, no-HA Prometheus
// WorkloadClusterReconciler installs onto every workload cluster: a
// remote-write receiver for k6's own experimental-prometheus-rw output
// (K6_PROMETHEUS_RW_TREND_AS_NATIVE_HISTOGRAM=true, hence
// --enable-feature=native-histograms - without it every push 500s), plus a
// NodePort Service so cmd/apiserver (on the management cluster) can query
// it directly. Matches hack/dev-prometheus.yaml, previously applied by hand.
func PrometheusObjects() []client.Object {
	labels := prometheusLabels()
	nodePort := int32(PrometheusNodePort)
	return []client.Object{
		&corev1.Namespace{
			TypeMeta:   metav1.TypeMeta{Kind: "Namespace", APIVersion: "v1"},
			ObjectMeta: metav1.ObjectMeta{Name: ObservabilityNamespace},
		},
		&corev1.ConfigMap{
			TypeMeta:   metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
			ObjectMeta: metav1.ObjectMeta{Name: prometheusConfigMapName, Namespace: ObservabilityNamespace},
			Data: map[string]string{
				"prometheus.yml": "global:\n  scrape_interval: 15s\nscrape_configs: []\n",
			},
		},
		&appsv1.Deployment{
			TypeMeta:   metav1.TypeMeta{Kind: "Deployment", APIVersion: "apps/v1"},
			ObjectMeta: metav1.ObjectMeta{Name: PrometheusServiceName, Namespace: ObservabilityNamespace, Labels: labels},
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To(int32(1)),
				Selector: &metav1.LabelSelector{MatchLabels: labels},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: labels},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "prometheus",
								Image: "prom/prometheus:v2.55.1",
								Args: []string{
									"--config.file=/etc/prometheus/prometheus.yml",
									"--storage.tsdb.path=/prometheus",
									"--web.enable-remote-write-receiver",
									"--web.enable-lifecycle",
									"--enable-feature=native-histograms",
								},
								Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: PrometheusPort}},
								VolumeMounts: []corev1.VolumeMount{
									{Name: "config", MountPath: "/etc/prometheus"},
								},
							},
						},
						Volumes: []corev1.Volume{
							{
								Name: "config",
								VolumeSource: corev1.VolumeSource{
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{Name: prometheusConfigMapName},
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
			ObjectMeta: metav1.ObjectMeta{Name: PrometheusServiceName, Namespace: ObservabilityNamespace},
			Spec: corev1.ServiceSpec{
				Selector: labels,
				Ports: []corev1.ServicePort{
					{Name: "http", Port: 80, TargetPort: intstr.FromInt32(PrometheusPort)},
					{Name: "prom-native", Port: PrometheusPort, TargetPort: intstr.FromInt32(PrometheusPort)},
				},
			},
		},
		&corev1.Service{
			TypeMeta:   metav1.TypeMeta{Kind: "Service", APIVersion: "v1"},
			ObjectMeta: metav1.ObjectMeta{Name: PrometheusNodePortServiceName, Namespace: ObservabilityNamespace},
			Spec: corev1.ServiceSpec{
				Type:     corev1.ServiceTypeNodePort,
				Selector: labels,
				Ports: []corev1.ServicePort{
					{Name: "http", Port: PrometheusPort, TargetPort: intstr.FromInt32(PrometheusPort), NodePort: nodePort},
				},
			},
		},
	}
}
