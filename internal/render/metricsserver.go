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
	_ "embed"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// metricsServerManifest is upstream's own components.yaml
// (https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml,
// pinned at v0.9.0), vendored verbatim except for one addition:
// --kubelet-insecure-tls on the metrics-server container. kubeadm-bootstrapped
// clusters (every WorkloadCluster this operator creates) don't wire up a
// kubelet serving certificate signed by a CA metrics-server trusts by
// default, so without this flag every kubelet scrape fails with "x509:
// certificate signed by unknown authority" and `kubectl top nodes/pods`
// never returns data - a well-known, standard requirement for kubeadm
// clusters specifically (cloud-managed clusters normally don't need it).
//
//go:embed manifests/metrics-server.yaml
var metricsServerManifest []byte

// MetricsServerObjects returns the metrics-server objects
// WorkloadClusterReconciler applies to a workload cluster once its control
// plane is up: without it, `kubectl top`/the Kubernetes Dashboard-style CPU
// and memory figures (and Headlamp's own per-node/per-pod usage graphs) have
// no metrics.k8s.io API to read from - kubeadm doesn't install this any more
// than it installs a CNI.
func MetricsServerObjects() ([]client.Object, error) {
	return decodeManifest(metricsServerManifest)
}
