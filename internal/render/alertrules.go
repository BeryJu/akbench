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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// PrometheusRule is prometheus-operator's own CRD (monitoring.coreos.com/v1,
// same source as ServiceMonitor - see servicemonitor.go's own doc comment
// for why this is unstructured.Unstructured rather than a typed import).
const (
	prometheusRuleAPIVersion = "monitoring.coreos.com/v1"
	prometheusRuleKind       = "PrometheusRule"

	// DefaultAlertRulesName is this project's own PrometheusRule object -
	// distinct from the many PrometheusRule objects kube-prometheus-stack's
	// own defaultRules.* values already create (KubePodCrashLooping,
	// KubeDeploymentReplicasMismatch, KubePersistentVolumeFillingUp, node
	// pressure, etc. - defaultRules.create was never disabled in
	// KubePrometheusStackHelmValues, so those already exist and are already
	// evaluated; they just had nowhere to go before Alertmanager existed).
	// This one adds only what the generic rules can't know: alerts specific
	// to this project's own authentik/postgres/k6 metrics.
	DefaultAlertRulesName = "authentik-bench-default-alerts"
)

// DefaultAlertRules builds the PrometheusRule this project ships by default,
// applied directly by WorkloadClusterReconciler.ensurePrometheus (not
// through the kube-prometheus-stack HelmRelease's own extraManifests value -
// see KubePrometheusStackHelmValues' own doc comment for why: this chart
// runs every extraManifests entry through Helm's own `tpl`, which breaks on
// this object's "{{ $labels... }}" alert-annotation syntax). Every
// expression here
// was checked against this project's own live Prometheus
// (http://<node-ip>:30909/api/v1/query) before being written down, not
// guessed from documentation - this codebase has been burned twice already
// by dashboard panels querying metric names that sounded right but never
// actually existed (see gitops/dashboards/README.md's own "Caveats"
// section).
//
//   - AuthentikBenchServiceScrapeDown: up{job=~".+-metrics"} == 0 catches an
//     authentik-server/-worker/postgresql ServiceMonitor target going
//     unreachable (confirmed live this metric and label pattern are real:
//     job names follow "<release>-authentik-server-metrics" etc., and
//     several were actually down when this was written, including from the
//     postgres-PVC-full incident this project hit earlier).
//   - AuthentikBenchPostgresDown: pg_up == 0 is a stricter signal than the
//     scrape-level alert above - it means postgres_exporter itself reached
//     Postgres and got told the server isn't accepting connections, not
//     just that the exporter's own /metrics endpoint is unreachable.
//   - AuthentikBenchHighRequestFailureRate: a request-volume-weighted ratio
//     of k6_http_reqs_total{expected_response="false"} over all
//     k6_http_reqs_total, grouped by testid/target/profile - NOT a bare
//     k6_http_req_failed_rate threshold (that was this rule's first version,
//     and it was wrong: k6 tags every metric with the full request context,
//     including its own status code, so k6_http_req_failed_rate emits one
//     series per distinct (method,status,url,...) combination, and every
//     series belonging to a non-2xx/3xx status is pinned at exactly 1 by
//     definition - averaging across these series is not the same as the
//     true proportion of failed requests, and can read as "25% failed" (1
//     failing series out of 4 distinct ones) when the real, volume-weighted
//     failure rate is 0.05%. Confirmed live against a real burn test: 105
//     failed requests out of ~196,150 total. gitops/dashboards/k6.json's own
//     "Failed requests" panel had the same flaw (an unweighted
//     `avg by (testid) (k6_http_req_failed_rate{...})`) and was fixed
//     alongside this rule - see that file's own history.
//   - AuthentikBenchWorkerTaskErrors: authentik_tasks_errors_total counts
//     authentik's own background task failures (blueprint apply, outpost
//     sync, etc.) - a real signal no generic k8s-level rule could derive.
//
// Deliberately NOT duplicated here: node/PVC/pod-restart health, which
// kube-prometheus-stack's own default rules already cover once Alertmanager
// exists to receive them.
func DefaultAlertRules() *unstructured.Unstructured {
	rules := []any{
		map[string]any{
			"alert": "AuthentikBenchServiceScrapeDown",
			"expr":  `up{job=~".+-metrics"} == 0`,
			"for":   "10m",
			"labels": map[string]any{
				"severity": "warning",
			},
			"annotations": map[string]any{
				"summary":     "{{ $labels.job }} on {{ $labels.instance }} has been unreachable for 10m",
				"description": "Prometheus can't scrape {{ $labels.job }} ({{ $labels.instance }}) - the pod may be down, crash-looping, or its /metrics port unreachable.",
			},
		},
		map[string]any{
			"alert": "AuthentikBenchPostgresDown",
			"expr":  `pg_up == 0`,
			"for":   "5m",
			"labels": map[string]any{
				"severity": "critical",
			},
			"annotations": map[string]any{
				"summary":     "postgres_exporter on {{ $labels.instance }} reports Postgres is down",
				"description": "pg_up == 0 for 5m: postgres_exporter reached its target but Postgres itself isn't accepting connections.",
			},
		},
		map[string]any{
			"alert": "AuthentikBenchHighRequestFailureRate",
			"expr": `sum by (testid, target, profile) (rate(k6_http_reqs_total{expected_response="false"}[5m]))` +
				` / sum by (testid, target, profile) (rate(k6_http_reqs_total[5m])) > 0.05`,
			"for": "5m",
			"labels": map[string]any{
				"severity": "warning",
			},
			"annotations": map[string]any{
				"summary":     "test {{ $labels.testid }} against {{ $labels.target }} has a >5% HTTP failure rate",
				"description": "More than 5% of requests failed over the last 5m on testid={{ $labels.testid }} target={{ $labels.target }} profile={{ $labels.profile }}, weighted by actual request volume (not an average across k6's own per-status series).",
			},
		},
		map[string]any{
			"alert": "AuthentikBenchWorkerTaskErrors",
			"expr":  `increase(authentik_tasks_errors_total[15m]) > 0`,
			"for":   "5m",
			"labels": map[string]any{
				"severity": "warning",
			},
			"annotations": map[string]any{
				"summary":     "authentik worker on {{ $labels.instance }} is recording background task errors",
				"description": "authentik_tasks_errors_total increased in the last 15m on {{ $labels.instance }} - check that pod's own worker logs for the failing task.",
			},
		},
	}

	u := &unstructured.Unstructured{}
	u.SetAPIVersion(prometheusRuleAPIVersion)
	u.SetKind(prometheusRuleKind)
	u.SetName(DefaultAlertRulesName)
	u.SetNamespace(ObservabilityNamespace)
	_ = unstructured.SetNestedSlice(u.Object, []any{
		map[string]any{
			"name":  "authentik-bench.rules",
			"rules": rules,
		},
	}, "spec", "groups")
	return u
}
