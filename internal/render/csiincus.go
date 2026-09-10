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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// The csi-incus manifests below are vendored verbatim from
// ~/t/csi-incus/deploy/ (github.com/BeryJu/csi-incus) - upstream boilerplate
// (CSIDriver object, RBAC, the controller Deployment with its
// registry.k8s.io/sig-storage sidecars, the node DaemonSet), already wired to
// expect a "incus-credentials" Secret in kube-system with keys
// addr/client-cert/client-key/server-cert. Nothing here is templated per
// workload cluster.
var (
	//go:embed manifests/csi-incus/csidriver.yaml
	csiIncusDriverManifest []byte
	//go:embed manifests/csi-incus/rbac.yaml
	csiIncusRBACManifest []byte
	//go:embed manifests/csi-incus/controller.yaml
	csiIncusControllerManifest []byte
	//go:embed manifests/csi-incus/node.yaml
	csiIncusNodeManifest []byte
)

// CSIIncusCredentialsNamespace/Name must match what the vendored
// controller.yaml expects (see its own header comment).
const (
	CSIIncusCredentialsNamespace = "kube-system"
	CSIIncusCredentialsName      = "incus-credentials"
)

// CSIIncusStoragePool is the Incus storage pool csi-incus provisions volumes
// from on every workload cluster.
const CSIIncusStoragePool = "vms"

// CSIIncusStorageClassName is the sole default StorageClass on every
// workload cluster, replacing local-path-provisioner's.
const CSIIncusStorageClassName = "incus-block"

// CSIIncusStorageClass renders the sole StorageClass on every workload
// cluster (marked default - there's no local-path-provisioner fallback
// anymore): block content type (the driver's default, and the only content
// type that needs deciding here since every workload-cluster machine is an
// Incus VM - see render.ControlPlaneLXCMachineTemplate/
// WorkerLXCMachineTemplate, both instanceType "virtual-machine" - block
// volumes can't attach to containers). Not vendored from
// deploy/storageclass-example.yaml: the pool name is this project's own
// config, not upstream boilerplate, so a small hand-authored object matches
// this package's convention for project-owned pieces (see
// PrometheusObjects/PyroscopeObjects/TempoObjects).
func CSIIncusStorageClass() *storagev1.StorageClass {
	return &storagev1.StorageClass{
		TypeMeta: metav1.TypeMeta{Kind: "StorageClass", APIVersion: "storage.k8s.io/v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        CSIIncusStorageClassName,
			Annotations: map[string]string{"storageclass.kubernetes.io/is-default-class": "true"},
		},
		Provisioner:          "incus.csi.linuxcontainers.org",
		Parameters:           map[string]string{"pool": CSIIncusStoragePool, "contentType": "block"},
		ReclaimPolicy:        ptr.To(corev1.PersistentVolumeReclaimDelete),
		VolumeBindingMode:    ptr.To(storagev1.VolumeBindingWaitForFirstConsumer),
		AllowVolumeExpansion: ptr.To(true),
	}
}

// CSIIncusServiceObjects returns the CSIDriver, RBAC, controller Deployment,
// node DaemonSet, and default StorageClass WorkloadClusterReconciler applies
// to every workload cluster. VolumeSnapshotClass support is deliberately
// left out for now: this project doesn't install the
// snapshot.storage.k8s.io CRDs/external-snapshotter controller anywhere, so
// applying one would either fail outright or sit inert - the controller's
// own csi-snapshotter sidecar (in the vendored controller.yaml) stays as a
// harmless idle process in the meantime.
func CSIIncusServiceObjects() ([]client.Object, error) {
	var objs []client.Object
	for _, manifest := range [][]byte{csiIncusDriverManifest, csiIncusRBACManifest, csiIncusControllerManifest, csiIncusNodeManifest} {
		decoded, err := decodeManifest(manifest)
		if err != nil {
			return nil, fmt.Errorf("decode csi-incus manifest: %w", err)
		}
		objs = append(objs, decoded...)
	}
	objs = append(objs, CSIIncusStorageClass())
	return objs, nil
}

// CSIIncusCredentialsSecret builds the "incus-credentials" Secret the
// vendored controller.yaml expects, by remapping keys from the same
// Incus/LXC connection Secret this project already uses for CAPI
// provisioning (WorkloadClusterSpec.Incus.LXCSecretRef - see its own doc
// comment for its key names: server/project/client-crt/client-key, plus
// server-crt seen live on this project's own lxc-secret). Reuses the
// existing trust relationship instead of requiring a second client
// certificate to be generated and registered per csi-incus's own README.
// "project" has no equivalent in csi-incus's controller (no such flag/env
// var in its README) and is intentionally dropped.
func CSIIncusCredentialsSecret(source *corev1.Secret) *corev1.Secret {
	data := map[string][]byte{
		"addr":        source.Data["server"],
		"client-cert": source.Data["client-crt"],
		"client-key":  source.Data["client-key"],
	}
	if serverCert, ok := source.Data["server-crt"]; ok {
		data["server-cert"] = serverCert
	}
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{Kind: "Secret", APIVersion: "v1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      CSIIncusCredentialsName,
			Namespace: CSIIncusCredentialsNamespace,
		},
		Data: data,
	}
}
