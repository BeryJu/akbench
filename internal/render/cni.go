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

// flannelManifest is upstream's own kube-flannel.yml
// (https://github.com/flannel-io/flannel/blob/master/Documentation/kube-flannel.yml),
// vendored verbatim rather than fetched at runtime. Its default
// net-conf.json Network (10.244.0.0/16) already matches the pod CIDR
// render.Cluster sets for every WorkloadCluster, so it needs no templating.
//
//go:embed manifests/kube-flannel.yaml
var flannelManifest []byte

// CNIObjects returns the CNI (currently always Flannel) objects
// WorkloadClusterReconciler applies to a workload cluster once its control
// plane is up: nothing schedules onto a kubeadm-bootstrapped cluster without
// one, since kubeadm itself doesn't install a CNI (kubelet reports "cni
// plugin not initialized" and every Node stays NotReady forever otherwise).
func CNIObjects() ([]client.Object, error) {
	return decodeManifest(flannelManifest)
}
