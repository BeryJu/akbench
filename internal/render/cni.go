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
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
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
func CNIObjects() ([]*unstructured.Unstructured, error) {
	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(flannelManifest), 4096)
	var objs []*unstructured.Unstructured
	for {
		u := &unstructured.Unstructured{}
		if err := decoder.Decode(u); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode CNI manifest: %w", err)
		}
		if len(u.Object) == 0 {
			continue
		}
		objs = append(objs, u)
	}
	return objs, nil
}
