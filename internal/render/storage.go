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

// localPathStorageManifest is upstream's own deploy/local-path-storage.yaml
// (https://github.com/rancher/local-path-provisioner, pinned at v0.0.37),
// vendored verbatim.
//
//go:embed manifests/local-path-storage.yaml
var localPathStorageManifest []byte

// StorageObjects returns Rancher's local-path-provisioner objects
// WorkloadClusterReconciler applies to every workload cluster, backed by
// each node's own local disk - a second, non-default StorageClass for
// anything that doesn't need real persistence/resize/snapshot semantics.
// The default StorageClass is csi-incus's (see CSIIncusStorageClass) - this
// manifest's own "local-path" StorageClass deliberately carries no
// is-default-class annotation, so exactly one StorageClass in the cluster
// is ever marked default (two would make PVCs with no storageClassName pick
// unpredictably between them).
func StorageObjects() ([]client.Object, error) {
	return decodeManifest(localPathStorageManifest)
}
