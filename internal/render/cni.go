package render

import (
	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Flannel is installed via its own official Helm chart now (flannel-io.github.io/flannel),
// the same Flux HelmRepository + HelmRelease mechanism csi-incus uses
// (internal/render/csiincus.go) - replacing an earlier vendored
// kube-flannel.yml go:embed. The chart's default podCidr (10.244.0.0/16)
// already matches the pod CIDR render.Cluster sets for every
// WorkloadCluster, so no values overrides are needed.
const (
	CNIHelmRepositoryName = "flannel"
	cniChartName          = "flannel"
	cniChartRepoURL       = "https://flannel-io.github.io/flannel"

	// cniChartVersion is pinned - matches the chart version last verified
	// working; bump deliberately.
	cniChartVersion = "v0.28.9"

	CNIHelmReleaseName = "flannel"

	// CNINamespace matches the original vendored manifest's namespace.
	CNINamespace = "kube-flannel"
)

// CNIHelmRepository is the (singleton, per-namespace) HelmRepository
// pointing at the official Flannel chart repo.
func CNIHelmRepository(namespace string) *sourcev1.HelmRepository {
	return &sourcev1.HelmRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      CNIHelmRepositoryName,
			Namespace: namespace,
		},
		Spec: sourcev1.HelmRepositorySpec{
			URL:      cniChartRepoURL,
			Interval: metav1.Duration{Duration: helmRepoInterval},
		},
	}
}

// CNIHelmRelease builds the Flux HelmRelease that installs Flannel onto a
// workload cluster remotely - kubeConfigSecretName is the
// "<cluster>-kubeconfig" Secret CAPI writes (see render.KubeconfigSecretName).
// CreateNamespace is set since, unlike csi-incus's kube-system, kube-flannel
// doesn't already exist on a fresh kubeadm cluster.
func CNIHelmRelease(namespace, kubeConfigSecretName string) *helmv2.HelmRelease {
	return &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      CNIHelmReleaseName,
			Namespace: namespace,
		},
		Spec: helmv2.HelmReleaseSpec{
			Interval:    metav1.Duration{Duration: helmRepoInterval},
			Timeout:     &metav1.Duration{Duration: helmInstallTimeout},
			ReleaseName: CNIHelmReleaseName,
			Chart: &helmv2.HelmChartTemplate{
				Spec: helmv2.HelmChartTemplateSpec{
					Chart:   cniChartName,
					Version: cniChartVersion,
					SourceRef: helmv2.CrossNamespaceObjectReference{
						Kind:      sourcev1.HelmRepositoryKind,
						Name:      CNIHelmRepositoryName,
						Namespace: namespace,
					},
				},
			},
			TargetNamespace: CNINamespace,
			Install:         &helmv2.Install{CreateNamespace: true},
			KubeConfig: &fluxmeta.KubeConfigReference{
				SecretRef: &fluxmeta.SecretKeyReference{Name: kubeConfigSecretName},
			},
		},
	}
}
