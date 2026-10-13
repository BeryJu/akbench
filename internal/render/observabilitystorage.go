package render

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// Persistent storage for the observability stack, so a restart or a pod
// rescheduled onto another monitoring node (e.g. after the node it ran on
// went NotReady) keeps its data: metrics, alerts' silences, Grafana's
// annotations (which TestRunReconciler posts per run), traces and profiles.
// Volumes are ReadWriteOnce, so every Deployment using one rolls with
// Recreate - the old pod has to let go of the volume before the new one can
// attach it.
const (
	PrometheusStorageSize   = "20Gi"
	AlertmanagerStorageSize = "1Gi"
	GrafanaStorageSize      = "2Gi"
	TempoStorageSize        = "10Gi"
	PyroscopeStorageSize    = "10Gi"

	// prometheusRetentionSize keeps Prometheus' TSDB comfortably inside
	// PrometheusStorageSize (WAL and compaction need headroom).
	prometheusRetentionSize = "16GB"
)

// observabilityStorageClass is the StorageClass observability volumes use:
// csi-incus' on an Incus cluster (ensureCSIIncus installs it as the only
// one), otherwise the cluster's default.
func observabilityStorageClass(wc *benchv1alpha1.WorkloadCluster) *string {
	if wc == nil || wc.Spec.Incus == nil {
		return nil
	}
	return new(CSIIncusStorageClassName)
}

// observabilityPVC is a PersistentVolumeClaim in ObservabilityNamespace.
func observabilityPVC(wc *benchv1alpha1.WorkloadCluster, name, size string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		Kind: "PersistentVolumeClaim", APIVersion: apiVersionV1,
		Name: name, Namespace: ObservabilityNamespace,
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			StorageClassName: observabilityStorageClass(wc),
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(size)},
			},
		},
	}
}

// volumeClaimTemplate is the kube-prometheus-stack chart's storageSpec
// shape for Prometheus/Alertmanager, whose StatefulSets create their own
// PVCs.
func volumeClaimTemplate(wc *benchv1alpha1.WorkloadCluster, size string) map[string]any {
	spec := map[string]any{
		"accessModes": []string{string(corev1.ReadWriteOnce)},
		"resources":   map[string]any{"requests": map[string]any{"storage": size}},
	}
	if class := observabilityStorageClass(wc); class != nil {
		spec["storageClassName"] = *class
	}
	return map[string]any{"volumeClaimTemplate": map[string]any{"spec": spec}}
}

// withPersistentData mounts claim at mountPath in the Deployment's (only)
// container, makes the volume writable by the image's non-root user
// (fsGroup), and switches the rollout to Recreate - a ReadWriteOnce volume
// can't be attached to the old and the new pod at once.
func withPersistentData(d *appsv1.Deployment, claim, mountPath string, fsGroup int64) *appsv1.Deployment {
	pod := &d.Spec.Template.Spec
	pod.Volumes = append(pod.Volumes, corev1.Volume{
		Name:                  "data",
		PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claim},
	})
	pod.Containers[0].VolumeMounts = append(pod.Containers[0].VolumeMounts, corev1.VolumeMount{Name: "data", MountPath: mountPath})
	pod.SecurityContext = &corev1.PodSecurityContext{FSGroup: new(fsGroup)}
	d.Spec.Strategy = appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}
	return d
}
