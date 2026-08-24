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
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
	"github.com/goauthentik/authentik-bench/internal/grafana"
)

var _ = Describe("TestRun Controller", func() {
	const resourceName = "quick-run-1"
	typeNamespacedName := types.NamespacedName{Name: resourceName, Namespace: "default"}

	var topo *benchv1alpha1.AuthentikTopology
	var tc *benchv1alpha1.TestCase

	BeforeEach(func() {
		topo = createReadyTopology(context.Background(), "testrun-topology", "authentik-1", "authentik-1-server.ns.svc.cluster.local", 80)
		tc = &benchv1alpha1.TestCase{
			ObjectMeta: metav1.ObjectMeta{Name: "quick-login", Namespace: "default"},
			Spec: benchv1alpha1.TestCaseSpec{
				TopologyRef: corev1.LocalObjectReference{Name: topo.Name},
				Profile:     benchv1alpha1.ProfileQuick,
				VUs:         4,
				Iterations:  100,
				Flow:        "default-authentication-flow",
			},
		}
		Expect(k8sClient.Create(context.Background(), tc)).To(Succeed())

		run := &benchv1alpha1.TestRun{
			ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"},
			Spec:       benchv1alpha1.TestRunSpec{TestCaseRef: corev1.LocalObjectReference{Name: tc.Name}},
		}
		Expect(k8sClient.Create(context.Background(), run)).To(Succeed())
	})

	AfterEach(func() {
		forceDelete(context.Background(), &benchv1alpha1.TestRun{ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"}})
		forceDelete(context.Background(), tc)
		forceDelete(context.Background(), topo)
	})

	It("creates a Job with the testid/target/profile contract and tracks it to completion", func() {
		r := &TestRunReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Grafana: &grafana.Client{}}

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
