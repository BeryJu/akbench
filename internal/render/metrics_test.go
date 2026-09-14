package render

import (
	"encoding/json"
	"testing"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
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
// Grafana/Alertmanager both disabled (this project runs its own Grafana and
// has no alerting need), and the two compatibility Services every other
// consumer of PrometheusServiceName/PrometheusNodePortServiceName expects.
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

	if enabled, _ := values["grafana"].(map[string]any)["enabled"].(bool); enabled {
		t.Error("grafana.enabled = true, want false (this project runs its own Grafana)")
	}
	if enabled, _ := values["alertmanager"].(map[string]any)["enabled"].(bool); enabled {
		t.Error("alertmanager.enabled = true, want false (no alerting need)")
	}

	manifests, ok := values["extraManifests"].([]any)
	if !ok || len(manifests) != 2 {
		t.Fatalf("extraManifests = %v, want 2 compatibility Service objects", values["extraManifests"])
	}
	var sawClusterIP, sawNodePort bool
	for _, m := range manifests {
		obj, ok := m.(map[string]any)
		if !ok {
			t.Fatalf("extraManifests entry is not an object: %v", m)
		}
		if obj["kind"] != "Service" {
			t.Errorf("extraManifests entry kind = %v, want Service", obj["kind"])
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
		}
	}
	if !sawClusterIP {
		t.Errorf("extraManifests missing a Service named %q", PrometheusServiceName)
	}
	if !sawNodePort {
		t.Errorf("extraManifests missing a Service named %q", PrometheusNodePortServiceName)
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
