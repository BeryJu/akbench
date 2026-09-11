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

// TempoServiceName/TempoOTLPHTTPPort must match defaultTracingEndpoint
// (internal/controller/authentiktopology_controller.go:
// "http://tempo.bench-system.svc.cluster.local:4318") exactly - every
// authentik pod with errorReporting.enabled exports traces there
// regardless of which cluster it runs on, so each workload cluster's own
// Tempo must answer at this exact name. 4318 is the OTLP HTTP receiver's
// own standard default port.
const (
	TempoServiceName  = "tempo"
	TempoOTLPHTTPPort = 4318
	// TempoQueryPort is Tempo's own HTTP API (server.http_listen_port in
	// tempoConfig below) - what Grafana's Tempo datasource actually queries
	// against. Confirmed missing from this file's own Service/container
	// ports by reading the reference harness's compose.yml, whose own
	// comment says exactly this: Tempo's port 3200 is used internally by
	// Grafana/Prometheus, never exposed for ingestion - only 4318 (OTLP
	// ingest) was ever exposed here, so nothing could query traces back out
	// before this.
	TempoQueryPort     = 3200
	tempoConfigMapName = TempoServiceName + "-config"
)

func tempoLabels() map[string]string {
	return map[string]string{"app": TempoServiceName}
}

// tempoConfig is a minimal single-binary config: OTLP receivers only (no
// Jaeger/Zipkin - nothing here emits those), local-filesystem trace storage
// backed by an emptyDir (no HA/persistence, matching this project's
// no-HA posture elsewhere) rather than object storage, since there's no S3/
// GCS-alike available on an Incus-provisioned workload cluster and nothing
// yet needs traces to survive a pod restart.
const tempoConfig = `server:
  http_listen_port: 3200
distributor:
  receivers:
    otlp:
      protocols:
        http:
        grpc:
storage:
  trace:
    backend: local
    local:
      path: /var/tempo/traces
    wal:
      path: /var/tempo/wal
`

// TempoObjects returns the single-replica, no-HA Grafana Tempo
// WorkloadClusterReconciler installs onto every workload cluster, so
// authentik's OTLP trace export (AUTHENTIK_ERROR_REPORTING__OTEL_ENDPOINT)
// has somewhere real to land instead of failing silently against a
// nonexistent host.
func TempoObjects() []client.Object {
	labels := tempoLabels()
	return []client.Object{
		&corev1.Namespace{
			TypeMeta:   metav1.TypeMeta{Kind: "Namespace", APIVersion: "v1"},
			ObjectMeta: metav1.ObjectMeta{Name: ObservabilityNamespace},
		},
		&corev1.ConfigMap{
			TypeMeta:   metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
			ObjectMeta: metav1.ObjectMeta{Name: tempoConfigMapName, Namespace: ObservabilityNamespace},
			Data:       map[string]string{"tempo.yaml": tempoConfig},
		},
		&appsv1.Deployment{
			TypeMeta:   metav1.TypeMeta{Kind: "Deployment", APIVersion: "apps/v1"},
			ObjectMeta: metav1.ObjectMeta{Name: TempoServiceName, Namespace: ObservabilityNamespace, Labels: labels},
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To(int32(1)),
				Selector: &metav1.LabelSelector{MatchLabels: labels},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: labels},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "tempo",
								Image: "grafana/tempo:2.6.1",
								Args:  []string{"-config.file=/etc/tempo/tempo.yaml"},
								Ports: []corev1.ContainerPort{
									{Name: "otlp-http", ContainerPort: TempoOTLPHTTPPort},
									{Name: "tempo-query", ContainerPort: TempoQueryPort},
								},
								VolumeMounts: []corev1.VolumeMount{
									{Name: "config", MountPath: "/etc/tempo"},
									{Name: "data", MountPath: "/var/tempo"},
								},
							},
						},
						Volumes: []corev1.Volume{
							{
								Name: "config",
								VolumeSource: corev1.VolumeSource{
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{Name: tempoConfigMapName},
									},
								},
							},
							{
								Name:         "data",
								VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
							},
						},
					},
				},
			},
		},
		&corev1.Service{
			TypeMeta:   metav1.TypeMeta{Kind: "Service", APIVersion: "v1"},
			ObjectMeta: metav1.ObjectMeta{Name: TempoServiceName, Namespace: ObservabilityNamespace},
			Spec: corev1.ServiceSpec{
				Selector: labels,
				Ports: []corev1.ServicePort{
					{Name: "otlp-http", Port: TempoOTLPHTTPPort, TargetPort: intstr.FromInt32(TempoOTLPHTTPPort)},
					{Name: "tempo-query", Port: TempoQueryPort, TargetPort: intstr.FromInt32(TempoQueryPort)},
				},
			},
		},
	}
}
