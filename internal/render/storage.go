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

// localPathStorageManifest is upstream's own deploy/local-path-storage.yaml
// (https://github.com/rancher/local-path-provisioner, pinned at v0.0.37),
// vendored verbatim except for one addition: the "local-path" StorageClass
// it defines is annotated storageclass.kubernetes.io/is-default-class so
// PVCs render.HelmValues leaves without an explicit storageClassName (the
// authentik chart's bundled postgresql subchart, notably) bind automatically
// instead of sitting Pending forever.
//
//go:embed manifests/local-path-storage.yaml
var localPathStorageManifest []byte

// StorageObjects returns the dynamic-provisioner objects
// WorkloadClusterReconciler applies to a workload cluster once its control
// plane is up: there's no Incus-native CSI driver worth depending on (the
// only Incus/LXD-adjacent option, canonical/lxd-csi-driver, is LXD-only,
// explicitly early-stage with no backwards-compatibility guarantee, and
// requires devLXD volume management plus a bearer-token auth setup per
// cluster) - so every workload cluster gets Rancher's local-path-provisioner
// instead, backed by each node's own local disk. Without a default
// StorageClass, any PVC with no storageClassName (e.g. the authentik
// chart's bundled postgresql) stays Pending forever with "unbound immediate
// PersistentVolumeClaims", and any Helm install waiting on it times out.
func StorageObjects() ([]*unstructured.Unstructured, error) {
	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(localPathStorageManifest), 4096)
	var objs []*unstructured.Unstructured
	for {
		u := &unstructured.Unstructured{}
		if err := decoder.Decode(u); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode storage manifest: %w", err)
		}
		if len(u.Object) == 0 {
			continue
		}
		objs = append(objs, u)
	}
	return objs, nil
}
