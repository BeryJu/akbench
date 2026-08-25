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

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	controlplanev1 "sigs.k8s.io/cluster-api/api/controlplane/kubeadm/v1beta2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

var _ = Describe("WorkloadCluster Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			resourceName      = "test-resource"
			resourceNamespace = "default"
		)

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: resourceNamespace,
		}
		workloadcluster := &benchv1alpha1.WorkloadCluster{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind WorkloadCluster")
			err := k8sClient.Get(ctx, typeNamespacedName, workloadcluster)
			if err != nil && errors.IsNotFound(err) {
				resource := &benchv1alpha1.WorkloadCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: resourceNamespace,
					},
					Spec: benchv1alpha1.WorkloadClusterSpec{
						KubernetesVersion: "v1.31.0",
						Provider:          benchv1alpha1.InfrastructureIncus,
						ControlPlane:      benchv1alpha1.MachineSpec{Replicas: 1},
						Workers:           benchv1alpha1.MachineSpec{Replicas: 2},
						Incus: &benchv1alpha1.IncusProviderSpec{
							LXCSecretRef: corev1.LocalObjectReference{Name: "lxc-secret"},
							ControlPlane: benchv1alpha1.IncusMachineSpec{Flavor: "c2-m4"},
							Workers:      benchv1alpha1.IncusMachineSpec{Flavor: "c2-m4"},
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &benchv1alpha1.WorkloadCluster{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance WorkloadCluster")
			forceDelete(ctx, resource)
			forceDelete(ctx, &clusterv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: resourceNamespace}})
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &WorkloadClusterReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("rendering the CAPI Cluster, but not yet the control plane")
			var cluster clusterv1.Cluster
			Expect(k8sClient.Get(ctx, typeNamespacedName, &cluster)).To(Succeed())
			Expect(cluster.Spec.InfrastructureRef.Kind).To(Equal("LXCCluster"))
			Expect(cluster.Spec.ControlPlaneRef.Name).To(Equal(resourceName + "-control-plane"))

			// No real Incus/CAPI infrastructure controller runs in envtest to
			// provision a load balancer and copy its address up into
			// Cluster.spec.controlPlaneEndpoint, so the KubeadmControlPlane
			// must not exist yet - creating it before controlPlaneEndpoint is
			// set is exactly the bug that produced a control-plane apiserver
			// certificate missing the load balancer's IP as a SAN.
			var kcp controlplanev1.KubeadmControlPlane
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-control-plane", Namespace: resourceNamespace}, &kcp)).
				To(MatchError(errors.IsNotFound, "IsNotFound"))
			Expect(k8sClient.Get(ctx, typeNamespacedName, workloadcluster)).To(Succeed())
			ready := meta.FindStatusCondition(workloadcluster.Status.Conditions, benchv1alpha1.ConditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Message).To(ContainSubstring("controlPlaneEndpoint"))

			By("simulating the infrastructure provider populating controlPlaneEndpoint")
			cluster.Spec.ControlPlaneEndpoint = clusterv1.APIEndpoint{Host: "10.0.0.1", Port: 6443}
			Expect(k8sClient.Update(ctx, &cluster)).To(Succeed())

			By("reconciling again and confirming the control plane now renders")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resourceName + "-control-plane", Namespace: resourceNamespace}, &kcp)).To(Succeed())
			Expect(*kcp.Spec.Replicas).To(Equal(int32(1)))

			By("reconciling once more and confirming the status reflects the (not-yet-ready) Cluster")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, typeNamespacedName, workloadcluster)).To(Succeed())
			Expect(workloadcluster.Status.ControlPlaneReady).To(BeFalse())
		})
	})
})
