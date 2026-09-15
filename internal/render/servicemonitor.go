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

// ServiceMonitor is prometheus-operator's own CRD (monitoring.coreos.com/v1,
// bundled in kube-prometheus-stack's own crds/ subchart - see metrics.go's
// KubePrometheusStackHelmRelease). Built as unstructured.Unstructured rather
// than importing prometheus-operator's typed API, the same call cnpg.go
// already made for CloudNativePG's Cluster CRD: one small object shape
// doesn't justify pulling in a second large module's whole dependency tree.
const (
	serviceMonitorAPIVersion = "monitoring.coreos.com/v1"
	serviceMonitorKind       = "ServiceMonitor"
)

// newServiceMonitor builds a minimal ServiceMonitor scraping "/metrics" off
// one named port of whatever Service carries the given labels in its own
// .metadata.labels (not .spec.selector - a ServiceMonitor's own
// spec.selector.matchLabels matches the target Service object itself, so
// every Service this selects must have these same labels set at the object
// level, not just as its pod selector). Requires no matching label on the
// ServiceMonitor object itself: this project's own kube-prometheus-stack
// install sets serviceMonitorSelectorNilUsesHelmValues: false (see
// KubePrometheusStackHelmValues), so its Prometheus already watches every
// ServiceMonitor in every namespace regardless of label.
func newServiceMonitor(name, namespace string, serviceLabels map[string]string, portName string) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetAPIVersion(serviceMonitorAPIVersion)
	u.SetKind(serviceMonitorKind)
	u.SetName(name)
	u.SetNamespace(namespace)
	_ = unstructured.SetNestedStringMap(u.Object, serviceLabels, "spec", "selector", "matchLabels")
	_ = unstructured.SetNestedSlice(u.Object, []any{
		map[string]any{"port": portName, "path": "/metrics", "interval": "30s"},
	}, "spec", "endpoints")
	return u
}
