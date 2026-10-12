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

// Shared literals repeated across this package's own hand-authored objects
// (as opposed to vendored third-party manifests) - every metav1.TypeMeta a
// project-authored Deployment/Service/Namespace/ConfigMap sets must match
// its real Go type exactly, so typo'd Kind/APIVersion strings are a real
// correctness risk worth centralizing against, not just a style nit.
const (
	kindNamespace  = "Namespace"
	kindConfigMap  = "ConfigMap"
	kindDeployment = "Deployment"
	kindService    = "Service"

	apiVersionV1     = "v1"
	apiVersionAppsV1 = "apps/v1"

	// portNameHTTP is the conventional single-port name every plain-HTTP
	// Service/container port in this package uses.
	portNameHTTP = "http"

	// labelKeyApp is the selector label every single-Deployment addon in this
	// package (Grafana, Pyroscope, Tempo, oidc-test-client) uses to tie its
	// own Deployment to its own Service.
	labelKeyApp = "app"

	// labelAppInstance is the standard Kubernetes "recommended label"
	// (kubernetes.io/docs/concepts/overview/working-with-objects/common-labels)
	// kube-prometheus-stack's own charts key their component Services by -
	// used to build a selector that reaches them without guessing their
	// release name.
	labelAppInstance = "app.kubernetes.io/instance"

	// topologyKeyHostname pins an affinity/anti-affinity term to "one match
	// per node" - every required (anti-)affinity term in this package uses
	// node-level, not zone-level, spreading.
	topologyKeyHostname = "kubernetes.io/hostname"
)
