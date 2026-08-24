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

package api

import (
	"net/http"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// Without Cluster API installed the picker still needs one entry, or the UI
// cannot offer anywhere to deploy a topology.
func TestListClustersWithoutCAPI(t *testing.T) {
	s, _ := newServer(t, Config{})
	rec := do(t, s, http.MethodGet, "/api/v1/clusters", nil)
	requireStatus(t, rec, http.StatusOK)

	body := decodeBody[struct {
		Items []Cluster `json:"items"`
	}](t, rec)
	if len(body.Items) != 1 {
		t.Fatalf("got %d clusters, want just the management cluster", len(body.Items))
	}
	if !body.Items[0].Management || body.Items[0].Name != ManagementClusterName {
		t.Fatalf("got %+v, want the management cluster", body.Items[0])
	}
}

func TestGetManagementCluster(t *testing.T) {
	s, _ := newServer(t, Config{})
	rec := do(t, s, http.MethodGet, "/api/v1/clusters/"+ManagementClusterName, nil)
	requireStatus(t, rec, http.StatusOK)

	cluster := decodeBody[Cluster](t, rec)
	if !cluster.ControlPlaneReady {
		t.Fatalf("management cluster reports not ready: %+v", cluster)
	}
}

func TestGetUnknownClusterWithoutCAPI(t *testing.T) {
	s, _ := newServer(t, Config{})
	requireStatus(t, do(t, s, http.MethodGet, "/api/v1/clusters/workload-1", nil), http.StatusNotFound)
}

// Without Cluster API installed, registering/deprovisioning has to say so
// rather than half-work.
func TestRegisterClusterWithoutCAPI(t *testing.T) {
	s, _ := newServer(t, Config{})
	requireStatus(t, do(t, s, http.MethodPost, "/api/v1/clusters", map[string]any{"name": "workload-1"}),
		http.StatusNotImplemented)
	requireStatus(t, do(t, s, http.MethodDelete, "/api/v1/clusters/workload-1", nil),
		http.StatusNotImplemented)
}

// With Cluster API installed, workload clusters join the picker alongside the
// management cluster, carrying the readiness the operator waits on.
func TestListClustersWithCAPI(t *testing.T) {
	workload := &unstructured.Unstructured{}
	workload.SetGroupVersionKind(capiClusterGVK)
	workload.SetName("workload-1")
	workload.SetNamespace(testNamespace)
	if err := unstructured.SetNestedField(workload.Object, "Provisioned", "status", "phase"); err != nil {
		t.Fatalf("build cluster: %v", err)
	}
	if err := unstructured.SetNestedField(workload.Object, true, "status", "initialization", "controlPlaneInitialized"); err != nil {
		t.Fatalf("build cluster: %v", err)
	}

	s := New(fake.NewClientBuilder().
		WithScheme(capiScheme(t)).
		WithObjects(workload).
		Build(), Config{Namespace: testNamespace, Token: testToken, CAPIAvailable: true})

	rec := do(t, s, http.MethodGet, "/api/v1/clusters", nil)
	requireStatus(t, rec, http.StatusOK)

	body := decodeBody[struct {
		Items []Cluster `json:"items"`
	}](t, rec)
	if len(body.Items) != 2 {
		t.Fatalf("got %d clusters, want management + workload-1", len(body.Items))
	}
	got := body.Items[1]
	if got.Name != "workload-1" || got.Phase != "Provisioned" || !got.ControlPlaneReady || got.Management {
		t.Fatalf("workload cluster mapped as %+v", got)
	}
}

// With Cluster API installed, registering a cluster creates a WorkloadCluster
// CR the operator provisions from, and deleting it removes that CR again -
// the actual CAPI/Incus teardown is WorkloadClusterReconciler's job, not the
// API's.
func TestRegisterAndDeleteClusterWithCAPI(t *testing.T) {
	s, k8s := newServer(t, Config{CAPIAvailable: true})

	body := map[string]any{
		"metadata": map[string]any{"name": "workload-1"},
		"spec": map[string]any{
			"kubernetesVersion": "v1.31.0",
			"lxcSecretRef":      map[string]any{"name": "lxc-secret"},
		},
	}
	rec := do(t, s, http.MethodPost, "/api/v1/clusters", body)
	requireStatus(t, rec, http.StatusCreated)

	var wc benchv1alpha1.WorkloadCluster
	if err := k8s.Get(t.Context(), client.ObjectKey{Name: "workload-1", Namespace: testNamespace}, &wc); err != nil {
		t.Fatalf("expected WorkloadCluster to be created: %v", err)
	}
	if wc.Spec.KubernetesVersion != "v1.31.0" {
		t.Fatalf("got kubernetesVersion %q, want v1.31.0", wc.Spec.KubernetesVersion)
	}

	requireStatus(t, do(t, s, http.MethodPost, "/api/v1/clusters", map[string]any{"metadata": map[string]any{"name": ManagementClusterName}}),
		http.StatusBadRequest)

	requireStatus(t, do(t, s, http.MethodDelete, "/api/v1/clusters/workload-1", nil), http.StatusNoContent)
	if err := k8s.Get(t.Context(), client.ObjectKey{Name: "workload-1", Namespace: testNamespace}, &wc); !apierrors.IsNotFound(err) {
		t.Fatalf("expected WorkloadCluster to be deleted, got err=%v", err)
	}
}

// capiScheme knows Cluster API's Cluster as an unstructured type, which is how
// the backend reads it - it never needs CAPI's Go types.
func capiScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := testScheme(t)
	s.AddKnownTypeWithName(capiClusterGVK, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(capiClusterGVK.GroupVersion().WithKind("ClusterList"), &unstructured.UnstructuredList{})
	return s
}
