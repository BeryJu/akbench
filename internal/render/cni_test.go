package render

import "testing"

func TestCNIHelmRepository(t *testing.T) {
	repo := CNIHelmRepository("default")

	if repo.Name != CNIHelmRepositoryName {
		t.Errorf("name = %q, want %q", repo.Name, CNIHelmRepositoryName)
	}
	if repo.Namespace != "default" {
		t.Errorf("namespace = %q, want default", repo.Namespace)
	}
	if repo.Spec.URL != cniChartRepoURL {
		t.Errorf("url = %q, want %q", repo.Spec.URL, cniChartRepoURL)
	}
}

func TestCNIHelmRelease(t *testing.T) {
	hr := CNIHelmRelease("default", "my-cluster-kubeconfig")

	if hr.Name != CNIHelmReleaseName {
		t.Errorf("name = %q, want %q", hr.Name, CNIHelmReleaseName)
	}
	if hr.Spec.Chart.Spec.Chart != cniChartName {
		t.Errorf("chart = %q, want %q", hr.Spec.Chart.Spec.Chart, cniChartName)
	}
	if hr.Spec.Chart.Spec.SourceRef.Name != CNIHelmRepositoryName {
		t.Errorf("sourceRef.name = %q, want %q", hr.Spec.Chart.Spec.SourceRef.Name, CNIHelmRepositoryName)
	}
	if hr.Spec.TargetNamespace != CNINamespace {
		t.Errorf("targetNamespace = %q, want %q", hr.Spec.TargetNamespace, CNINamespace)
	}
	if hr.Spec.Install == nil || !hr.Spec.Install.CreateNamespace {
		t.Error("expected Install.CreateNamespace = true (kube-flannel doesn't already exist)")
	}
	if hr.Spec.KubeConfig == nil || hr.Spec.KubeConfig.SecretRef.Name != "my-cluster-kubeconfig" {
		t.Errorf("kubeConfig.secretRef.name = %v, want my-cluster-kubeconfig", hr.Spec.KubeConfig)
	}
}
