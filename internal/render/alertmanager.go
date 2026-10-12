package render

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// Alertmanager is bundled in the same kube-prometheus-stack chart install as
// Prometheus (see KubePrometheusStackHelmValues/ensurePrometheus) rather than
// a separate addon - prometheus-operator wires the Alertmanager CR it
// creates straight to the Prometheus CR it also creates, so splitting them
// into two installs would only add a second HelmRelease for no benefit.
const (
	// AlertmanagerServiceName matches the chart's own generated Service name
	// for the Alertmanager StatefulSet it creates (prefixed the same way
	// Prometheus's own Service is - "alertmanager-operated" is the
	// headless one prometheus-operator itself manages; this project's own
	// compat Service, built the same way prometheusCompatServices already
	// is, selects the Alertmanager pod directly by label instead of relying
	// on that name).
	AlertmanagerServiceName = "alertmanager"

	// AlertmanagerPort is Alertmanager's own default listen port, unchanged
	// by this chart.
	AlertmanagerPort = 9093

	// AlertmanagerNodePortServiceName/AlertmanagerNodePort expose
	// Alertmanager's API (GET /api/v2/alerts, etc.) outside its own
	// cluster, the same reachability pattern PrometheusNodePort/
	// GrafanaNodePort already establish - a management-cluster caller (or
	// cmd/apiserver) reaches a workload cluster's node IP directly, not the
	// reverse. Pinned rather than auto-assigned for the same reason those
	// two are: WorkloadClusterReconciler never has to read the Service back
	// just to learn its own port.
	AlertmanagerNodePortServiceName = "alertmanager-nodeport"
	AlertmanagerNodePort            = 30912
)

// alertmanagerPodLabels mirrors prometheusPodLabels - confirmed against the
// same chart's templates/alertmanager/service.yaml selector:
// app.kubernetes.io/name=alertmanager, app.kubernetes.io/instance equal to
// the Alertmanager CR's own name, which cleanPrometheusOperatorObjectNames
// (set true in KubePrometheusStackHelmValues) makes equal to
// kubePrometheusStackFullname exactly, the same naming rule it already
// applies to the Prometheus CR.
func alertmanagerPodLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name": "alertmanager",
		labelAppInstance:         kubePrometheusStackFullname,
	}
}

// alertmanagerCompatServices are this project's own ClusterIP+NodePort pair
// for Alertmanager, built the same way prometheusCompatServices already is
// and injected into the same HelmRelease via the same extraManifests value -
// one install still produces everything needed, no second apply step
// against the remote cluster.
func alertmanagerCompatServices() []map[string]any {
	labels := alertmanagerPodLabels()
	clusterIP := &corev1.Service{
		Kind: kindService, APIVersion: apiVersionV1,
		Name: AlertmanagerServiceName, Namespace: ObservabilityNamespace,
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{Name: portNameHTTP, Port: AlertmanagerPort, TargetPort: intstr.FromInt32(AlertmanagerPort)},
			},
		},
	}
	nodePort := &corev1.Service{
		Kind: kindService, APIVersion: apiVersionV1,
		Name: AlertmanagerNodePortServiceName, Namespace: ObservabilityNamespace,
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeNodePort,
			Selector: labels,
			Ports: []corev1.ServicePort{
				{Name: portNameHTTP, Port: AlertmanagerPort, TargetPort: intstr.FromInt32(AlertmanagerPort), NodePort: AlertmanagerNodePort},
			},
		},
	}
	out := make([]map[string]any, 0, 2)
	for _, svc := range []*corev1.Service{clusterIP, nodePort} {
		m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(svc)
		if err != nil {
			// Same reasoning as prometheusCompatServices' own panic: svc is
			// fixed and entirely literal, so a conversion failure here would
			// mean this function itself is broken, not a runtime condition
			// any caller could recover from.
			panic(fmt.Sprintf("convert alertmanager compat service %q to unstructured: %v", svc.Name, err))
		}
		out = append(out, m)
	}
	return out
}

// defaultAlertmanagerConfig is the chart's own "alertmanager.config" value -
// written verbatim into the Secret Alertmanager loads its config from (the
// chart's own simple-config mechanism; bypasses the AlertmanagerConfig CRD
// entirely). No real notification integration is configured here: nothing
// about who should be paged, or through what channel (Slack/email/webhook/
// PagerDuty/...), was specified anywhere this project's requirements came
// from, and inventing one would mean guessing credentials or endpoints that
// don't exist. The "null" receiver is Alertmanager's own standard idiom for
// exactly this situation - every alert still fires, groups, and is visible
// through Alertmanager's own API/UI (GET /api/v2/alerts via
// AlertmanagerNodePort above), it just isn't forwarded anywhere. Wiring a
// real receiver later is a route/receivers values change only - everything
// else here (the rules themselves, the Service, the URL plumbing) stays the
// same.
func defaultAlertmanagerConfig() map[string]any {
	return map[string]any{
		"global": map[string]any{
			"resolve_timeout": "5m",
		},
		"route": map[string]any{
			"group_by":        []string{"alertname", "job"},
			"group_wait":      "30s",
			"group_interval":  "5m",
			"repeat_interval": "4h",
			"receiver":        "null",
		},
		"receivers": []map[string]any{
			{"name": "null"},
		},
	}
}
