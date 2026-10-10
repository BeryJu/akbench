package render

import (
	"encoding/json"
	"strings"
	"testing"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestKubePrometheusStackHelmRepository(t *testing.T) {
	repo := KubePrometheusStackHelmRepository("default")

	if repo.Name != kubePrometheusStackHelmRepositoryName {
		t.Errorf("name = %q, want %q", repo.Name, kubePrometheusStackHelmRepositoryName)
	}
	if repo.Namespace != "default" {
		t.Errorf("namespace = %q, want default", repo.Namespace)
	}
	if repo.Spec.URL != kubePrometheusStackChartRepoURL {
		t.Errorf("url = %q, want %q", repo.Spec.URL, kubePrometheusStackChartRepoURL)
	}
}

// TestKubePrometheusStackHelmValues guards the values this project actually
// depends on: the remote-write receiver k6 pushes to (no matching
// enable-feature for native histograms - see KubePrometheusStackHelmValues'
// own doc comment for why that's correct, not an oversight), the bundled
// Grafana disabled (this project runs its own Grafana), Alertmanager
// enabled with this project's own default config/rules, and the
// compatibility Services every other consumer of
// PrometheusServiceName/PrometheusNodePortServiceName/
// AlertmanagerServiceName/AlertmanagerNodePortServiceName expects.
func TestKubePrometheusStackHelmValues(t *testing.T) {
	raw, err := KubePrometheusStackHelmValues()
	if err != nil {
		t.Fatalf("KubePrometheusStackHelmValues: %v", err)
	}

	var values map[string]any
	if err := json.Unmarshal(raw.Raw, &values); err != nil {
		t.Fatalf("unmarshal values: %v", err)
	}

	prometheusSpec, _ := values["prometheus"].(map[string]any)["prometheusSpec"].(map[string]any)
	if enabled, _ := prometheusSpec["enableRemoteWriteReceiver"].(bool); !enabled {
		t.Errorf("prometheus.prometheusSpec.enableRemoteWriteReceiver = %v, want true", prometheusSpec["enableRemoteWriteReceiver"])
	}
	// Both false: the chart's own default (true) restricts discovery to
	// ServiceMonitors/PodMonitors carrying this release's own "release: ..."
	// label, which none of the ones this operator creates or asks other
	// charts to create (authentik's own, the bundled postgresql subchart's,
	// CNPG's PodMonitor, this package's own newServiceMonitor) carry.
	if v, ok := prometheusSpec["serviceMonitorSelectorNilUsesHelmValues"].(bool); !ok || v {
		t.Errorf("prometheus.prometheusSpec.serviceMonitorSelectorNilUsesHelmValues = %v, want false", prometheusSpec["serviceMonitorSelectorNilUsesHelmValues"])
	}
	if v, ok := prometheusSpec["podMonitorSelectorNilUsesHelmValues"].(bool); !ok || v {
		t.Errorf("prometheus.prometheusSpec.podMonitorSelectorNilUsesHelmValues = %v, want false", prometheusSpec["podMonitorSelectorNilUsesHelmValues"])
	}
	if v, ok := prometheusSpec["ruleSelectorNilUsesHelmValues"].(bool); !ok || v {
		t.Errorf("prometheus.prometheusSpec.ruleSelectorNilUsesHelmValues = %v, want false", prometheusSpec["ruleSelectorNilUsesHelmValues"])
	}

	if enabled, _ := values["grafana"].(map[string]any)["enabled"].(bool); enabled {
		t.Error("grafana.enabled = true, want false (this project runs its own Grafana)")
	}
	alertmanager, _ := values["alertmanager"].(map[string]any)
	if enabled, _ := alertmanager["enabled"].(bool); !enabled {
		t.Error("alertmanager.enabled = false, want true")
	}
	if _, ok := alertmanager["config"].(map[string]any)["route"]; !ok {
		t.Errorf("alertmanager.config.route missing: %v", alertmanager["config"])
	}

	// DefaultAlertRules is deliberately NOT among these - see its own doc
	// comment (and KubePrometheusStackHelmValues') for why a PrometheusRule
	// containing "{{ $labels... }}" can't survive this chart's own
	// extraManifests -> tpl pipeline; it's applied directly by
	// ensurePrometheus instead (TestDefaultAlertRules covers it).
	manifests, ok := values["extraManifests"].([]any)
	if !ok || len(manifests) != 4 {
		t.Fatalf("extraManifests = %v, want 4 objects (2 Prometheus compat Services, 2 Alertmanager compat Services)", values["extraManifests"])
	}
	var sawClusterIP, sawNodePort, sawAlertmanagerClusterIP, sawAlertmanagerNodePort bool
	for _, m := range manifests {
		obj, ok := m.(map[string]any)
		if !ok {
			t.Fatalf("extraManifests entry is not an object: %v", m)
		}
		if obj["kind"] != "Service" {
			t.Errorf("extraManifests entry kind = %v, want Service", obj["kind"])
			continue
		}
		metadata, _ := obj["metadata"].(map[string]any)
		if metadata["namespace"] != ObservabilityNamespace {
			t.Errorf("extraManifests entry namespace = %v, want %q", metadata["namespace"], ObservabilityNamespace)
		}
		switch metadata["name"] {
		case PrometheusServiceName:
			sawClusterIP = true
		case PrometheusNodePortServiceName:
			sawNodePort = true
			spec, _ := obj["spec"].(map[string]any)
			if spec["type"] != "NodePort" {
				t.Errorf("%s spec.type = %v, want NodePort", PrometheusNodePortServiceName, spec["type"])
			}
		case AlertmanagerServiceName:
			sawAlertmanagerClusterIP = true
		case AlertmanagerNodePortServiceName:
			sawAlertmanagerNodePort = true
			spec, _ := obj["spec"].(map[string]any)
			if spec["type"] != "NodePort" {
				t.Errorf("%s spec.type = %v, want NodePort", AlertmanagerNodePortServiceName, spec["type"])
			}
		}
	}
	for name, saw := range map[string]bool{
		PrometheusServiceName:           sawClusterIP,
		PrometheusNodePortServiceName:   sawNodePort,
		AlertmanagerServiceName:         sawAlertmanagerClusterIP,
		AlertmanagerNodePortServiceName: sawAlertmanagerNodePort,
	} {
		if !saw {
			t.Errorf("extraManifests missing a Service named %q", name)
		}
	}
}

// TestDefaultAlertRules guards the one thing that actually broke live when
// this was first wired through extraManifests instead (see
// KubePrometheusStackHelmValues' own doc comment): every alert's
// annotations must keep their "{{ $labels... }}" Prometheus/Alertmanager
// templating syntax intact, verbatim, since nothing in this project's own
// code ever touches it - only Prometheus's rule engine expands it, at alert
// time.
func TestDefaultAlertRules(t *testing.T) {
	rule := DefaultAlertRules()

	if rule.GetKind() != "PrometheusRule" {
		t.Errorf("kind = %q, want PrometheusRule", rule.GetKind())
	}
	if rule.GetName() != DefaultAlertRulesName {
		t.Errorf("name = %q, want %q", rule.GetName(), DefaultAlertRulesName)
	}
	if rule.GetNamespace() != ObservabilityNamespace {
		t.Errorf("namespace = %q, want %q", rule.GetNamespace(), ObservabilityNamespace)
	}

	groups, ok, err := unstructured.NestedSlice(rule.Object, "spec", "groups")
	if err != nil || !ok {
		t.Fatalf("spec.groups: ok=%v err=%v", ok, err)
	}
	group, ok := groups[0].(map[string]any)
	if !ok {
		t.Fatalf("spec.groups[0] is not an object: %v", groups[0])
	}
	rules, ok := group["rules"].([]any)
	if !ok || len(rules) == 0 {
		t.Fatalf("spec.groups[0].rules missing or empty: %v", group["rules"])
	}

	wantAlerts := map[string]bool{
		"AuthentikBenchServiceScrapeDown":      false,
		"AuthentikBenchPostgresDown":           false,
		"AuthentikBenchHighRequestFailureRate": false,
		"AuthentikBenchWorkerTaskErrors":       false,
	}
	for _, r := range rules {
		rr, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("rule entry is not an object: %v", r)
		}
		name, _ := rr["alert"].(string)
		if _, known := wantAlerts[name]; !known {
			t.Errorf("unexpected alert name %q", name)
			continue
		}
		wantAlerts[name] = true
		if _, ok := rr["expr"].(string); !ok || rr["expr"] == "" {
			t.Errorf("%s: expr missing", name)
		}
		if _, ok := rr["for"].(string); !ok {
			t.Errorf("%s: for missing", name)
		}
		// Every alert's summary references at least one $labels field -
		// confirming it, specifically, keeps its literal "{{ }}" intact is
		// what actually caught the extraManifests/tpl bug live, not a
		// generic "annotations exist" check.
		annotations, _ := rr["annotations"].(map[string]any)
		summary, _ := annotations["summary"].(string)
		if !strings.Contains(summary, "{{") || !strings.Contains(summary, "}}") {
			t.Errorf("%s: annotations.summary = %q, want it to keep its \"{{ $labels... }}\" templating syntax verbatim", name, summary)
		}
		if _, ok := annotations["description"].(string); !ok {
			t.Errorf("%s: annotations.description missing", name)
		}
	}
	for name, saw := range wantAlerts {
		if !saw {
			t.Errorf("missing alert %q", name)
		}
	}
}

func TestKubePrometheusStackHelmRelease(t *testing.T) {
	values, err := KubePrometheusStackHelmValues()
	if err != nil {
		t.Fatalf("KubePrometheusStackHelmValues: %v", err)
	}

	hr := KubePrometheusStackHelmRelease("default", "my-cluster-kubeconfig", values)

	if hr.Name != KubePrometheusStackHelmReleaseName {
		t.Errorf("name = %q, want %q", hr.Name, KubePrometheusStackHelmReleaseName)
	}
	if hr.Spec.Chart.Spec.Chart != kubePrometheusStackChartName {
		t.Errorf("chart = %q, want %q", hr.Spec.Chart.Spec.Chart, kubePrometheusStackChartName)
	}
	if hr.Spec.TargetNamespace != ObservabilityNamespace {
		t.Errorf("targetNamespace = %q, want %q", hr.Spec.TargetNamespace, ObservabilityNamespace)
	}
	if hr.Spec.KubeConfig == nil || hr.Spec.KubeConfig.SecretRef.Name != "my-cluster-kubeconfig" {
		t.Errorf("kubeConfig.secretRef.name = %v, want my-cluster-kubeconfig", hr.Spec.KubeConfig)
	}
	// The chart's own CRDs (Prometheus/ServiceMonitor/PrometheusRule/...)
	// are exactly what this install exists to provide - both install and
	// upgrade must actually apply them, not just on first install.
	if hr.Spec.Install == nil || hr.Spec.Install.CRDs != helmv2.CreateReplace {
		t.Errorf("install.crds = %v, want CreateReplace", hr.Spec.Install)
	}
	if hr.Spec.Upgrade == nil || hr.Spec.Upgrade.CRDs != helmv2.CreateReplace {
		t.Errorf("upgrade.crds = %v, want CreateReplace", hr.Spec.Upgrade)
	}
}
