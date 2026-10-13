package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

var _ = Describe("TestRun Controller", func() {
	const resourceName = "quick-run-1"
	typeNamespacedName := types.NamespacedName{Name: resourceName, Namespace: "default"}

	var topo *benchv1alpha1.AuthentikDeployment
	var tc *benchv1alpha1.TestCase

	BeforeEach(func() {
		topo = createReadyDeployment(context.Background(), "testrun-deployment", "authentik-1", "authentik-1-server.ns.svc.cluster.local")
		tc = &benchv1alpha1.TestCase{
			Name: "quick-login", Namespace: "default",
			Spec: benchv1alpha1.TestCaseSpec{
				DeploymentRefs: []corev1.LocalObjectReference{{Name: topo.Name}},
				Profile:        benchv1alpha1.ProfileQuick,
				VUs:            4,
				Iterations:     100,
				Flow:           "default-authentication-flow",
			},
		}
		Expect(k8sClient.Create(context.Background(), tc)).To(Succeed())

		run := &benchv1alpha1.TestRun{
			Name: resourceName, Namespace: "default",
			Spec: benchv1alpha1.TestRunSpec{TestCaseRef: corev1.LocalObjectReference{Name: tc.Name}},
		}
		Expect(k8sClient.Create(context.Background(), run)).To(Succeed())
	})

	AfterEach(func() {
		forceDelete(context.Background(), &benchv1alpha1.TestRun{Name: resourceName, Namespace: "default"})
		forceDelete(context.Background(), tc)
		forceDelete(context.Background(), topo)
	})

	It("creates a Job with the testid/target/profile contract and tracks it to completion", func() {
		r := &TestRunReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(100), Tracker: testTracker}

		_, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())

		var run benchv1alpha1.TestRun
		Expect(k8sClient.Get(context.Background(), typeNamespacedName, &run)).To(Succeed())
		Expect(run.Status.Phase).To(Equal(benchv1alpha1.TestRunRunning))
		Expect(run.Status.JobRef).NotTo(BeNil())
		Expect(run.Status.ResolvedSpec).NotTo(BeNil())
		Expect(run.Status.ResolvedSpec.VUs).To(Equal(int32(4)))

		jobKey := types.NamespacedName{Name: run.Status.JobRef.Name, Namespace: "default"}
		var job batchv1.Job
		Expect(k8sClient.Get(context.Background(), jobKey, &job)).To(Succeed())
		args := job.Spec.Template.Spec.Containers[0].Args
		Expect(args).To(ContainElement("testid=" + tc.Name))
		Expect(args).To(ContainElement("target=authentik-1"))
		Expect(args).To(ContainElement("profile=quick"))

		By("simulating the Job completing successfully")
		job.Status.Succeeded = 1
		Expect(k8sClient.Status().Update(context.Background(), &job)).To(Succeed())

		_, err = r.Reconcile(context.Background(), reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(context.Background(), typeNamespacedName, &run)).To(Succeed())
		Expect(run.Status.Phase).To(Equal(benchv1alpha1.TestRunSucceeded))
		Expect(run.Status.CompletionTime).NotTo(BeNil())

		By("re-reconciling a terminal TestRun is a no-op")
		_, err = r.Reconcile(context.Background(), reconcile.Request{NamespacedName: typeNamespacedName})
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("TestRun Controller, multi-deployment TestCase", func() {
	var topoA, topoB *benchv1alpha1.AuthentikDeployment
	var tc *benchv1alpha1.TestCase

	BeforeEach(func() {
		topoA = createReadyDeployment(context.Background(), "testrun-multi-a", "target-a", "a.ns.svc.cluster.local")
		topoB = createReadyDeployment(context.Background(), "testrun-multi-b", "target-b", "b.ns.svc.cluster.local")
		tc = &benchv1alpha1.TestCase{
			Name: "quick-login-multi", Namespace: "default",
			Spec: benchv1alpha1.TestCaseSpec{
				DeploymentRefs: []corev1.LocalObjectReference{{Name: topoA.Name}, {Name: topoB.Name}},
				Profile:        benchv1alpha1.ProfileQuick,
				VUs:            4,
				Iterations:     100,
				Flow:           "default-authentication-flow",
			},
		}
		Expect(k8sClient.Create(context.Background(), tc)).To(Succeed())
	})

	AfterEach(func() {
		forceDelete(context.Background(), &benchv1alpha1.TestRun{Name: "quick-run-multi-a", Namespace: "default"})
		forceDelete(context.Background(), &benchv1alpha1.TestRun{Name: "quick-run-multi-b", Namespace: "default"})
		forceDelete(context.Background(), &benchv1alpha1.TestRun{Name: "quick-run-multi-foreign", Namespace: "default"})
		forceDelete(context.Background(), tc)
		forceDelete(context.Background(), topoA)
		forceDelete(context.Background(), topoB)
	})

	It("runs each pinned TestRun against its own deploymentRef, not the TestCase's first deployment", func() {
		runA := &benchv1alpha1.TestRun{
			Name: "quick-run-multi-a", Namespace: "default",
			Spec: benchv1alpha1.TestRunSpec{
				TestCaseRef:   corev1.LocalObjectReference{Name: tc.Name},
				DeploymentRef: &corev1.LocalObjectReference{Name: topoB.Name},
			},
		}
		Expect(k8sClient.Create(context.Background(), runA)).To(Succeed())

		r := &TestRunReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(100), Tracker: testTracker}
		_, err := r.Reconcile(context.Background(), reconcile.Request{Name: runA.Name, Namespace: "default"})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: runA.Name, Namespace: "default"}, runA)).To(Succeed())
		Expect(runA.Status.JobRef).NotTo(BeNil())

		var job batchv1.Job
		Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: runA.Status.JobRef.Name, Namespace: "default"}, &job)).To(Succeed())
		args := job.Spec.Template.Spec.Containers[0].Args
		Expect(args).To(ContainElement("target=target-b"))
		Expect(args).NotTo(ContainElement("target=target-a"))
	})

	It("falls back to the TestCase's first deployment when deploymentRef is unset", func() {
		runB := &benchv1alpha1.TestRun{
			Name: "quick-run-multi-b", Namespace: "default",
			Spec: benchv1alpha1.TestRunSpec{TestCaseRef: corev1.LocalObjectReference{Name: tc.Name}},
		}
		Expect(k8sClient.Create(context.Background(), runB)).To(Succeed())

		r := &TestRunReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(100), Tracker: testTracker}
		_, err := r.Reconcile(context.Background(), reconcile.Request{Name: runB.Name, Namespace: "default"})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: runB.Name, Namespace: "default"}, runB)).To(Succeed())
		Expect(runB.Status.JobRef).NotTo(BeNil())

		var job batchv1.Job
		Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: runB.Status.JobRef.Name, Namespace: "default"}, &job)).To(Succeed())
		args := job.Spec.Template.Spec.Containers[0].Args
		Expect(args).To(ContainElement("target=target-a"))
	})

	It("fails a TestRun pinned to a deployment the TestCase doesn't drive", func() {
		run := &benchv1alpha1.TestRun{
			Name: "quick-run-multi-foreign", Namespace: "default",
			Spec: benchv1alpha1.TestRunSpec{
				TestCaseRef:   corev1.LocalObjectReference{Name: tc.Name},
				DeploymentRef: &corev1.LocalObjectReference{Name: "not-a-target"},
			},
		}
		Expect(k8sClient.Create(context.Background(), run)).To(Succeed())

		r := &TestRunReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(100), Tracker: testTracker}
		_, err := r.Reconcile(context.Background(), reconcile.Request{Name: run.Name, Namespace: "default"})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: run.Name, Namespace: "default"}, run)).To(Succeed())
		Expect(run.Status.Phase).To(Equal(benchv1alpha1.TestRunFailed))
		Expect(run.Status.JobRef).To(BeNil())
	})
})
