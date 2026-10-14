package controller

import (
	"context"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	benchv1alpha1 "github.com/authentik-labs/akbench/api/v1alpha1"
	"github.com/authentik-labs/akbench/internal/render"
)

var _ = Describe("AuthentikDeployment reset", func() {
	const resourceName = "deployment-reset"
	key := types.NamespacedName{Name: resourceName, Namespace: "default"}

	BeforeEach(func() {
		topo := &benchv1alpha1.AuthentikDeployment{
			Name: resourceName, Namespace: "default",
			Spec: benchv1alpha1.AuthentikDeploymentSpec{
				ClusterRef: corev1.LocalObjectReference{Name: testClusterName},
				Namespace:  "authentik-" + resourceName,
				Server:     benchv1alpha1.WorkloadSpec{Replicas: 1},
				Worker:     benchv1alpha1.WorkloadSpec{Replicas: 1},
				Postgres:   benchv1alpha1.PostgresSpec{Mode: benchv1alpha1.PostgresEmbedded},
			},
		}
		Expect(k8sClient.Create(ctx, topo)).To(Succeed())
	})

	AfterEach(func() {
		forceDelete(ctx, &benchv1alpha1.AuthentikDeployment{Name: resourceName, Namespace: "default"})
	})

	It("uninstalls the release, deletes database PVCs and blueprint jobs, then reinstalls", func() {
		r := &AuthentikDeploymentReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(100), Tracker: testTracker}
		_, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		var topo benchv1alpha1.AuthentikDeployment
		Expect(k8sClient.Get(ctx, key, &topo)).To(Succeed())
		ns := topo.Spec.Namespace

		pvc := func(name string) *corev1.PersistentVolumeClaim {
			return &corev1.PersistentVolumeClaim{
				Name: name, Namespace: ns,
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
					},
				},
			}
		}
		dbPVC := pvc("data-" + render.EmbeddedPostgresStatefulSetName(&topo) + "-0")
		Expect(k8sClient.Create(ctx, dbPVC)).To(Succeed())
		blueprintPVC := pvc(render.BlueprintPVCName(&topo))
		Expect(k8sClient.Create(ctx, blueprintPVC)).To(Succeed())
		DeferCleanup(func() { forceDelete(ctx, blueprintPVC) })

		job := render.BlueprintJob(&topo, nil, render.BlueprintJobName(&topo, "fixtures", "x"), "cm", "key")
		Expect(k8sClient.Create(ctx, job)).To(Succeed())
		otherJob := render.BlueprintJob(&topo, nil, "unrelated", "cm", "key")
		Expect(k8sClient.Create(ctx, otherJob)).To(Succeed())
		DeferCleanup(func() { forceDelete(ctx, otherJob) })

		if topo.Annotations == nil {
			topo.Annotations = map[string]string{}
		}
		topo.Annotations[benchv1alpha1.AnnotationResetRequested] = "now"
		Expect(k8sClient.Update(ctx, &topo)).To(Succeed())

		// Step through the reset. Nothing in envtest removes the PVC
		// protection finalizer, so strip it like a kube-controller-manager
		// would once no pod uses the volume.
		Eventually(func(g Gomega) {
			_, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: key})
			g.Expect(err).NotTo(HaveOccurred())
			var stuck corev1.PersistentVolumeClaim
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dbPVC), &stuck); err == nil && !stuck.DeletionTimestamp.IsZero() {
				stuck.Finalizers = nil
				_ = k8sClient.Update(ctx, &stuck)
			}
			g.Expect(k8sClient.Get(ctx, key, &topo)).To(Succeed())
			g.Expect(topo.Annotations).NotTo(HaveKey(benchv1alpha1.AnnotationResetRequested))
		}).Should(Succeed())

		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(dbPVC), &corev1.PersistentVolumeClaim{}))).To(BeTrue())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(job), &batchv1.Job{}))).To(BeTrue())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(blueprintPVC), &corev1.PersistentVolumeClaim{})).To(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(otherJob), &batchv1.Job{})).To(Succeed())

		// The next reconcile is the normal one again and reinstalls.
		_, err = r.Reconcile(context.Background(), reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, key, &helmv2.HelmRelease{})).To(Succeed())
		Expect(k8sClient.Get(ctx, key, &topo)).To(Succeed())
		ready := meta.FindStatusCondition(topo.Status.Conditions, benchv1alpha1.ConditionReady)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Reason).NotTo(Equal(benchv1alpha1.ReasonResetting))
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
	})
})
