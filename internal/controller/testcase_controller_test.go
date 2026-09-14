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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

var _ = Describe("TestCase Controller", func() {
	var topo *benchv1alpha1.AuthentikDeployment

	BeforeEach(func() {
		topo = createReadyDeployment(context.Background(), "testcase-deployment", "authentik-1", "authentik-1-server.ns.svc.cluster.local", 80)
	})

	AfterEach(func() {
		forceDelete(context.Background(), topo)
	})

	Context("Burn profile", func() {
		const resourceName = "burn-login"
		typeNamespacedName := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			tc := &benchv1alpha1.TestCase{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"},
				Spec: benchv1alpha1.TestCaseSpec{
					DeploymentRef: corev1.LocalObjectReference{Name: topo.Name},
					Profile:       benchv1alpha1.ProfileBurn,
					VUs:           8,
					Flow:          "default-authentication-flow",
				},
			}
			Expect(k8sClient.Create(context.Background(), tc)).To(Succeed())
		})

		AfterEach(func() {
			forceDelete(context.Background(), &benchv1alpha1.TestCase{ObjectMeta: metav1.ObjectMeta{Name: typeNamespacedName.Name, Namespace: typeNamespacedName.Namespace}})
		})

		It("owns a Deployment tagged with the testid/target/profile contract", func() {
			r := &TestCaseReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(100)}
			_, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			var dep appsv1.Deployment
			depKey := types.NamespacedName{Name: "k6-" + resourceName, Namespace: "default"}
			Expect(k8sClient.Get(context.Background(), depKey, &dep)).To(Succeed())

			args := dep.Spec.Template.Spec.Containers[0].Args
			Expect(args).To(ContainElement("testid=" + resourceName))
			Expect(args).To(ContainElement("target=authentik-1"))
			Expect(args).To(ContainElement("profile=burn"))

			env := envMap(dep.Spec.Template.Spec.Containers[0].Env)
			Expect(env["TARGET"]).To(Equal("authentik-1"))
			Expect(env["BENCH_HOST"]).To(Equal("authentik-1-server.ns.svc.cluster.local"))
			Expect(env["BENCH_PORT"]).To(Equal("80"))
			Expect(env["PROFILE"]).To(Equal("burn"))

			var tc benchv1alpha1.TestCase
			Expect(k8sClient.Get(context.Background(), typeNamespacedName, &tc)).To(Succeed())
			Expect(tc.Status.BurnWorkloadRef).NotTo(BeNil())
			Expect(tc.Status.BurnWorkloadRef.Name).To(Equal("k6-" + resourceName))
			ready := meta.FindStatusCondition(tc.Status.Conditions, benchv1alpha1.ConditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		})
	})

	Context("Quick profile", func() {
		const resourceName = "quick-login"
		typeNamespacedName := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			tc := &benchv1alpha1.TestCase{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"},
				Spec: benchv1alpha1.TestCaseSpec{
					DeploymentRef: corev1.LocalObjectReference{Name: topo.Name},
					Profile:       benchv1alpha1.ProfileQuick,
					VUs:           4,
					Iterations:    100,
					Flow:          "default-authentication-flow",
				},
			}
			Expect(k8sClient.Create(context.Background(), tc)).To(Succeed())
		})

		AfterEach(func() {
			forceDelete(context.Background(), &benchv1alpha1.TestCase{ObjectMeta: metav1.ObjectMeta{Name: typeNamespacedName.Name, Namespace: typeNamespacedName.Namespace}})
		})

		It("does not create a burn Deployment - execution belongs to a TestRun", func() {
			r := &TestCaseReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(100)}
			_, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			var dep appsv1.Deployment
			depKey := types.NamespacedName{Name: "k6-" + resourceName, Namespace: "default"}
			err = k8sClient.Get(context.Background(), depKey, &dep)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			var tc benchv1alpha1.TestCase
			Expect(k8sClient.Get(context.Background(), typeNamespacedName, &tc)).To(Succeed())
			scriptReady := meta.FindStatusCondition(tc.Status.Conditions, benchv1alpha1.ConditionScriptReady)
			Expect(scriptReady).NotTo(BeNil())
			Expect(scriptReady.Status).To(Equal(metav1.ConditionTrue))
		})
	})

	Context("inline script", func() {
		const resourceName = "inline-script"
		typeNamespacedName := types.NamespacedName{Name: resourceName, Namespace: "default"}
		const inlineBody = "export default function () { /* custom */ }"

		BeforeEach(func() {
			tc := &benchv1alpha1.TestCase{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"},
				Spec: benchv1alpha1.TestCaseSpec{
					DeploymentRef: corev1.LocalObjectReference{Name: topo.Name},
					Inline:        inlineBody,
					Profile:       benchv1alpha1.ProfileQuick,
					VUs:           1,
					Iterations:    1,
				},
			}
			Expect(k8sClient.Create(context.Background(), tc)).To(Succeed())
		})

		AfterEach(func() {
			forceDelete(context.Background(), &benchv1alpha1.TestCase{ObjectMeta: metav1.ObjectMeta{Name: typeNamespacedName.Name, Namespace: typeNamespacedName.Namespace}})
		})

		It("mirrors the inline body verbatim into the script ConfigMap under script.ts, overriding template", func() {
			r := &TestCaseReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(100)}
			_, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			var cm corev1.ConfigMap
			cmKey := types.NamespacedName{Name: "k6-script-" + resourceName, Namespace: "default"}
			Expect(k8sClient.Get(context.Background(), cmKey, &cm)).To(Succeed())
			Expect(cm.Data).To(HaveKeyWithValue("script.ts", inlineBody))
		})
	})

	Context("unknown template", func() {
		const resourceName = "bad-template"
		typeNamespacedName := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			tc := &benchv1alpha1.TestCase{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"},
				Spec: benchv1alpha1.TestCaseSpec{
					DeploymentRef: corev1.LocalObjectReference{Name: topo.Name},
					Template:      "does-not-exist",
					Profile:       benchv1alpha1.ProfileQuick,
					VUs:           1,
					Iterations:    1,
				},
			}
			Expect(k8sClient.Create(context.Background(), tc)).To(Succeed())
		})

		AfterEach(func() {
			forceDelete(context.Background(), &benchv1alpha1.TestCase{ObjectMeta: metav1.ObjectMeta{Name: typeNamespacedName.Name, Namespace: typeNamespacedName.Namespace}})
		})

		It("reports DependencyNotReady with a clear message instead of silently falling back", func() {
			r := &TestCaseReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(100)}
			_, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			var tc benchv1alpha1.TestCase
			Expect(k8sClient.Get(context.Background(), typeNamespacedName, &tc)).To(Succeed())
			ready := meta.FindStatusCondition(tc.Status.Conditions, benchv1alpha1.ConditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(benchv1alpha1.ReasonDependencyNotReady))

			script := meta.FindStatusCondition(tc.Status.Conditions, benchv1alpha1.ConditionScriptReady)
			Expect(script).NotTo(BeNil())
			Expect(script.Status).To(Equal(metav1.ConditionFalse))
			Expect(script.Message).To(ContainSubstring("does-not-exist"))
		})
	})
})

func envMap(vars []corev1.EnvVar) map[string]string {
	m := make(map[string]string, len(vars))
	for _, v := range vars {
		m[v.Name] = v.Value
	}
	return m
}
