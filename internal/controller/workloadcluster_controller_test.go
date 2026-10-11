package controller

import (
	"context"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	controlplanev1 "sigs.k8s.io/cluster-api/api/controlplane/kubeadm/v1beta2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
	"github.com/goauthentik/authentik-bench/internal/render"
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
						WorkerPools: []benchv1alpha1.WorkerPoolSpec{
							{Name: "workers", Replicas: 2, Incus: benchv1alpha1.IncusMachineSpec{Flavor: "c2-m4"}},
						},
						Incus: &benchv1alpha1.IncusProviderSpec{
							LXCSecretRef: corev1.LocalObjectReference{Name: "lxc-secret"},
							ControlPlane: benchv1alpha1.IncusMachineSpec{Flavor: "c2-m4"},
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
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(100),
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

			By("confirming the worker pool's MachineDeployment was created")
			mdName := resourceName + "-workers"
			var md clusterv1.MachineDeployment
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: mdName, Namespace: resourceNamespace}, &md)).To(Succeed())
			Expect(*md.Spec.Replicas).To(Equal(int32(2)))
			originalTemplateName := md.Spec.Template.Spec.InfrastructureRef.Name

			By("editing the pool's replicas and flavor, and confirming they're applied in place")
			Expect(k8sClient.Get(ctx, typeNamespacedName, workloadcluster)).To(Succeed())
			workloadcluster.Spec.WorkerPools[0].Replicas = 5
			workloadcluster.Spec.WorkerPools[0].Incus.Flavor = "c8-m16"
			Expect(k8sClient.Update(ctx, workloadcluster)).To(Succeed())

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: mdName, Namespace: resourceNamespace}, &md)).To(Succeed())
			Expect(*md.Spec.Replicas).To(Equal(int32(5)))
			Expect(md.Spec.Template.Spec.InfrastructureRef.Name).NotTo(Equal(originalTemplateName),
				"a flavor edit must repoint the MachineDeployment at a new LXCMachineTemplate to trigger Cluster API's rollout")

			By("enabling autoscaling on the pool and confirming the annotations appear")
			Expect(k8sClient.Get(ctx, typeNamespacedName, workloadcluster)).To(Succeed())
			workloadcluster.Spec.WorkerPools[0].Autoscaling = &benchv1alpha1.AutoscalingSpec{MinReplicas: 1, MaxReplicas: 9}
			Expect(k8sClient.Update(ctx, workloadcluster)).To(Succeed())

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: mdName, Namespace: resourceNamespace}, &md)).To(Succeed())
			Expect(md.Annotations[render.ClusterAutoscalerMinSizeAnnotation]).To(Equal("1"))
			Expect(md.Annotations[render.ClusterAutoscalerMaxSizeAnnotation]).To(Equal("9"))

			By("simulating cluster-autoscaler scaling the pool, and confirming a reconcile doesn't fight it back")
			scaledReplicas := int32(3)
			md.Spec.Replicas = &scaledReplicas
			Expect(k8sClient.Update(ctx, &md)).To(Succeed())

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: mdName, Namespace: resourceNamespace}, &md)).To(Succeed())
			Expect(*md.Spec.Replicas).To(Equal(int32(3)),
				"WorkerPoolSpec.Replicas must not be reasserted once autoscaling is enabled - that would fight every scale-up/down cluster-autoscaler makes")

			By("adding a second pool and confirming it gets its own MachineDeployment")
			Expect(k8sClient.Get(ctx, typeNamespacedName, workloadcluster)).To(Succeed())
			workloadcluster.Spec.WorkerPools = append(workloadcluster.Spec.WorkerPools,
				benchv1alpha1.WorkerPoolSpec{Name: "extra", Replicas: 1, Incus: benchv1alpha1.IncusMachineSpec{Flavor: "c1-m2"}})
			Expect(k8sClient.Update(ctx, workloadcluster)).To(Succeed())

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			extraMDName := resourceName + "-extra"
			var extraMD clusterv1.MachineDeployment
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: extraMDName, Namespace: resourceNamespace}, &extraMD)).To(Succeed())

			By("removing the extra pool and confirming only its MachineDeployment is deleted")
			Expect(k8sClient.Get(ctx, typeNamespacedName, workloadcluster)).To(Succeed())
			workloadcluster.Spec.WorkerPools = workloadcluster.Spec.WorkerPools[:1]
			Expect(k8sClient.Update(ctx, workloadcluster)).To(Succeed())

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: extraMDName, Namespace: resourceNamespace}, &extraMD)).
				To(MatchError(errors.IsNotFound, "IsNotFound"))
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: mdName, Namespace: resourceNamespace}, &md)).To(Succeed())
		})

		// ensureClusterAutoscaler is gated behind ControlPlaneReady inside
		// Reconcile, which this envtest suite never reaches (no real
		// Incus/CAPI infrastructure controller runs here to get that far -
		// see the big test above's own comment on that). Called directly
		// instead, the same way a real reconcile would once that gate
		// passes.
		It("installs cluster-autoscaler only while a pool wants it, and tears it down once none do", func() {
			controllerReconciler := &WorkloadClusterReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(100),
			}
			// BeforeEach only populates workloadcluster via Get when the
			// resource already existed from a prior spec; on first creation
			// it leaves the shared var at its zero value (empty
			// Namespace, which ClusterAutoscalerHelmRelease needs) - every
			// other It in this file re-Gets before relying on it for the
			// same reason.
			Expect(k8sClient.Get(ctx, typeNamespacedName, workloadcluster)).To(Succeed())
			hrKey := types.NamespacedName{Name: render.ClusterAutoscalerHelmReleaseName(workloadcluster), Namespace: resourceNamespace}

			By("with no pool autoscaling, confirming ensureClusterAutoscaler is a no-op")
			Expect(controllerReconciler.ensureClusterAutoscaler(ctx, workloadcluster)).To(Succeed())
			Expect(k8sClient.Get(ctx, hrKey, &helmv2.HelmRelease{})).To(MatchError(errors.IsNotFound, "IsNotFound"))

			By("enabling autoscaling and confirming the HelmRelease is created")
			Expect(k8sClient.Get(ctx, typeNamespacedName, workloadcluster)).To(Succeed())
			workloadcluster.Spec.WorkerPools[0].Autoscaling = &benchv1alpha1.AutoscalingSpec{MinReplicas: 1, MaxReplicas: 5}
			Expect(k8sClient.Update(ctx, workloadcluster)).To(Succeed())

			Expect(controllerReconciler.ensureClusterAutoscaler(ctx, workloadcluster)).To(Succeed())
			var hr helmv2.HelmRelease
			Expect(k8sClient.Get(ctx, hrKey, &hr)).To(Succeed())
			Expect(hr.Spec.KubeConfig).To(BeNil(), "cluster-autoscaler must install locally, not remotely onto the workload cluster")

			By("disabling autoscaling again and confirming the HelmRelease is torn down")
			Expect(k8sClient.Get(ctx, typeNamespacedName, workloadcluster)).To(Succeed())
			workloadcluster.Spec.WorkerPools[0].Autoscaling = nil
			Expect(k8sClient.Update(ctx, workloadcluster)).To(Succeed())

			Expect(controllerReconciler.ensureClusterAutoscaler(ctx, workloadcluster)).To(Succeed())
			Expect(k8sClient.Get(ctx, hrKey, &helmv2.HelmRelease{})).To(MatchError(errors.IsNotFound, "IsNotFound"))
		})
	})
})
