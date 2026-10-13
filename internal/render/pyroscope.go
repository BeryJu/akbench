package render

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// PyroscopeServiceName/PyroscopePort must match defaultPyroscopeHost
// (internal/controller/authentikdeployment_controller.go:
// "http://pyroscope.bench-system.svc.cluster.local:4040") exactly - that's
// the cluster-relative address every authentik pod pushes continuous
// profiles to regardless of which cluster it runs on, so each workload
// cluster's own Pyroscope must answer at this exact name.
const (
	PyroscopeServiceName = "pyroscope"
	PyroscopePort        = 4040
)

func pyroscopeLabels() map[string]string {
	return map[string]string{labelKeyApp: PyroscopeServiceName}
}

// PyroscopeObjects returns the single-replica, no-HA Grafana Pyroscope
// WorkloadClusterReconciler installs onto every workload cluster. No target
// flag: the image's default command runs every component in one process
// ("monolithic mode"), which is Pyroscope's own supported single-binary
// deployment shape and matches this project's existing no-HA,
// dev/benchmark-scale posture for the rest of its addons (Prometheus,
// csi-incus). Local filesystem storage (the image's own
// default), so profiles don't survive a pod restart - acceptable here since
// nothing yet reads this data back out; it exists so authentik's own
// continuous-profiling push has somewhere real to land instead of failing
// silently against a nonexistent host.
func PyroscopeObjects(wc *benchv1alpha1.WorkloadCluster) []client.Object {
	labels := pyroscopeLabels()
	nodeSelector, tolerations := MonitoringPoolPlacement(wc)
	return []client.Object{
		&corev1.Namespace{
			Kind: kindNamespace, APIVersion: apiVersionV1,
			Name: ObservabilityNamespace,
		},
		observabilityPVC(wc, "pyroscope-data", PyroscopeStorageSize),
		withPersistentData(&appsv1.Deployment{
			Kind: kindDeployment, APIVersion: apiVersionAppsV1,
			Name: PyroscopeServiceName, Namespace: ObservabilityNamespace, Labels: labels,
			Spec: appsv1.DeploymentSpec{
				Replicas: new(int32(1)),
				Selector: &metav1.LabelSelector{MatchLabels: labels},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: labels},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "pyroscope",
								Image: "grafana/pyroscope:1.8.0",
								Ports: []corev1.ContainerPort{{Name: portNameHTTP, ContainerPort: PyroscopePort}},
							},
						},
						NodeSelector: nodeSelector,
						Tolerations:  tolerations,
					},
				},
			},
		}, "pyroscope-data", "/data", 10001),
		&corev1.Service{
			Kind: kindService, APIVersion: apiVersionV1,
			Name: PyroscopeServiceName, Namespace: ObservabilityNamespace, Labels: labels,
			Spec: corev1.ServiceSpec{
				Selector: labels,
				Ports: []corev1.ServicePort{
					{Name: portNameHTTP, Port: PyroscopePort, TargetPort: intstr.FromInt32(PyroscopePort)},
				},
			},
		},
		// Pyroscope serves its own Prometheus-format /metrics on the same
		// "http" port everything else uses - confirmed live (docker run
		// grafana/pyroscope:1.8.0, curl :4040/metrics returns real
		// counter_*/go_* series), not a separate port.
		newServiceMonitor(PyroscopeServiceName, labels, portNameHTTP),
	}
}
