package render

import (
	"crypto/sha256"
	"encoding/hex"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TempoServiceName/TempoOTLPHTTPPort must match defaultTracingEndpoint
// (internal/controller/authentikdeployment_controller.go:
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

func tempoConfigChecksum() string {
	sum := sha256.Sum256([]byte(tempoConfig))
	return hex.EncodeToString(sum[:])
}

func tempoLabels() map[string]string {
	return map[string]string{labelKeyApp: TempoServiceName}
}

// tempoConfig is a minimal single-binary config: OTLP receivers only (no
// Jaeger/Zipkin - nothing here emits those), local-filesystem trace storage
// backed by an emptyDir (no HA/persistence, matching this project's
// no-HA posture elsewhere) rather than object storage, since there's no S3/
// GCS-alike available on an Incus-provisioned workload cluster and nothing
// yet needs traces to survive a pod restart.
//
// The metrics-generator runs only its local-blocks processor: that's what
// TraceQL metrics queries (/api/metrics/query_range - quantile_over_time,
// rate, ... grouped by any span attribute) read from. Without it every such
// query fails with "empty ring", leaving only search and single-trace fetch.
// No span-metrics/service-graphs processors and no remote write - nothing
// here consumes generated Prometheus series.
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
metrics_generator:
  storage:
    path: /var/tempo/generator/wal
  traces_storage:
    path: /var/tempo/generator/traces
  processor:
    local_blocks:
      filter_server_spans: false
      flush_to_storage: true
overrides:
  defaults:
    metrics_generator:
      processors: [local-blocks]
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
			Kind: kindNamespace, APIVersion: apiVersionV1,
			Name: ObservabilityNamespace,
		},
		&corev1.ConfigMap{
			Kind: kindConfigMap, APIVersion: apiVersionV1,
			Name: tempoConfigMapName, Namespace: ObservabilityNamespace,
			Data: map[string]string{"tempo.yaml": tempoConfig},
		},
		&appsv1.Deployment{
			Kind: kindDeployment, APIVersion: apiVersionAppsV1,
			Name: TempoServiceName, Namespace: ObservabilityNamespace, Labels: labels,
			Spec: appsv1.DeploymentSpec{
				Replicas: new(int32(1)),
				Selector: &metav1.LabelSelector{MatchLabels: labels},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: labels,
						// Tempo only reads its config at startup - a changed
						// ConfigMap must roll the pod to take effect.
						Annotations: map[string]string{"checksum/config": tempoConfigChecksum()},
					},
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
								ConfigMap: &corev1.ConfigMapVolumeSource{
									Name: tempoConfigMapName,
								},
							},
							{
								Name:     "data",
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
		&corev1.Service{
			Kind: kindService, APIVersion: apiVersionV1,
			Name: TempoServiceName, Namespace: ObservabilityNamespace, Labels: labels,
			Spec: corev1.ServiceSpec{
				Selector: labels,
				Ports: []corev1.ServicePort{
					{Name: "otlp-http", Port: TempoOTLPHTTPPort, TargetPort: intstr.FromInt32(TempoOTLPHTTPPort)},
					{Name: "tempo-query", Port: TempoQueryPort, TargetPort: intstr.FromInt32(TempoQueryPort)},
				},
			},
		},
		// Tempo serves its own Prometheus-format /metrics on its HTTP API
		// port (tempo-query/3200, server.http_listen_port in tempoConfig
		// above), not the OTLP ingest port - confirmed live (docker run
		// grafana/tempo:2.6.1, curl :3200/metrics returns real go_*/
		// deprecated_flags_inuse_total series).
		newServiceMonitor(TempoServiceName, labels, "tempo-query"),
	}
}
