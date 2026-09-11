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
	"encoding/json"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
	"github.com/goauthentik/authentik-bench/internal/render"
)

var _ = Describe("AuthentikTopology Controller", func() {
	Context("Embedded postgres, Default placement (M1 scope)", func() {
		const resourceName = "topology-embedded"
		var typeNamespacedName types.NamespacedName

		BeforeEach(func() {
			typeNamespacedName = types.NamespacedName{Name: resourceName, Namespace: "default"}
			topo := &benchv1alpha1.AuthentikTopology{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"},
				Spec: benchv1alpha1.AuthentikTopologySpec{
					Namespace: "authentik-" + resourceName,
					Server:    benchv1alpha1.WorkloadSpec{Replicas: 2},
					Worker:    benchv1alpha1.WorkloadSpec{Replicas: 1},
					Postgres:  benchv1alpha1.PostgresSpec{Mode: benchv1alpha1.PostgresEmbedded},
				},
			}
			Expect(k8sClient.Create(ctx, topo)).To(Succeed())
		})

		AfterEach(func() {
			forceDelete(ctx, &benchv1alpha1.AuthentikTopology{ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"}})
		})

		It("renders a HelmRepository and a HelmRelease with the topology's values", func() {
			r := &AuthentikTopologyReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(100)}
			_, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			var hr helmv2.HelmRelease
			Expect(k8sClient.Get(context.Background(), typeNamespacedName, &hr)).To(Succeed())
			Expect(hr.Spec.TargetNamespace).To(Equal("authentik-" + resourceName))
			Expect(hr.Spec.Chart.Spec.Chart).To(Equal("authentik"))
			Expect(hr.Spec.KubeConfig).To(BeNil(), "M1 topologies deploy to the local cluster")

			var values render.AuthentikTopologyHelmValues
			Expect(hr.Spec.Values).NotTo(BeNil())
			Expect(json.Unmarshal(hr.Spec.Values.Raw, &values)).To(Succeed())
			Expect(values.Server["replicas"]).To(BeNumerically("==", 2))
			Expect(values.Postgres["enabled"]).To(BeTrue())

			var topo benchv1alpha1.AuthentikTopology
			Expect(k8sClient.Get(context.Background(), typeNamespacedName, &topo)).To(Succeed())
			postgresReady := meta.FindStatusCondition(topo.Status.Conditions, benchv1alpha1.ConditionPostgresReady)
			Expect(postgresReady).NotTo(BeNil())
			Expect(postgresReady.Status).To(Equal(metav1.ConditionTrue))
			// No real helm-controller runs in envtest, so HelmReleaseReady/Ready
			// stay False until it reconciles the HelmRelease we created above -
			// that's the meaningful thing to assert here, not a fake Ready=True.
			helmReleaseReady := meta.FindStatusCondition(topo.Status.Conditions, benchv1alpha1.ConditionHelmReleaseReady)
			Expect(helmReleaseReady).NotTo(BeNil())
			Expect(helmReleaseReady.Status).To(Equal(metav1.ConditionFalse))
		})
	})

	Context("Dedicated placement", func() {
		const resourceName = "topology-dedicated"
		var typeNamespacedName types.NamespacedName

		BeforeEach(func() {
			typeNamespacedName = types.NamespacedName{Name: resourceName, Namespace: "default"}
			topo := &benchv1alpha1.AuthentikTopology{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"},
				Spec: benchv1alpha1.AuthentikTopologySpec{
					Namespace: "authentik-" + resourceName,
					Server:    benchv1alpha1.WorkloadSpec{Replicas: 1},
					Worker:    benchv1alpha1.WorkloadSpec{Replicas: 1},
					Postgres:  benchv1alpha1.PostgresSpec{Mode: benchv1alpha1.PostgresEmbedded},
					Placement: benchv1alpha1.PlacementSpec{Mode: benchv1alpha1.PlacementDedicated},
				},
			}
			Expect(k8sClient.Create(ctx, topo)).To(Succeed())
		})

		AfterEach(func() {
			forceDelete(ctx, &benchv1alpha1.AuthentikTopology{ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"}})
		})

		It("renders a required pod anti-affinity directly on server/worker", func() {
			r := &AuthentikTopologyReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(100)}
			_, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			var hr helmv2.HelmRelease
			Expect(k8sClient.Get(context.Background(), typeNamespacedName, &hr)).To(Succeed())
			var values render.AuthentikTopologyHelmValues
			Expect(json.Unmarshal(hr.Spec.Values.Raw, &values)).To(Succeed())
			// server.affinity/worker.affinity, not global.affinity.podAntiAffinity:
			// the chart's own preset mechanism is no longer used at all -
			// ServerWorkerAffinity renders the equivalent required
			// anti-affinity directly, alongside the always-present
			// cross-topology term (see its own doc comment for why the two
			// can't be mixed: server.affinity/worker.affinity being set to
			// anything makes the chart skip its preset entirely).
			raw, err := json.Marshal(values.Server["affinity"])
			Expect(err).NotTo(HaveOccurred())
			var affinity map[string]any
			Expect(json.Unmarshal(raw, &affinity)).To(Succeed())
			podAntiAffinity, _ := affinity["podAntiAffinity"].(map[string]any)
			terms, _ := podAntiAffinity["requiredDuringSchedulingIgnoredDuringExecution"].([]any)
			Expect(terms).To(HaveLen(2), "want cross-topology + own-release required terms")
		})
	})

	Context("External postgres", func() {
		const resourceName = "topology-external"
		var typeNamespacedName types.NamespacedName

		BeforeEach(func() {
			typeNamespacedName = types.NamespacedName{Name: resourceName, Namespace: "default"}
			targetNs := "authentik-" + resourceName
			Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: targetNs}})).To(Succeed())
			Expect(k8sClient.Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "external-pg", Namespace: targetNs},
				StringData: map[string]string{
					"host": "pg.example.com", "port": "5432", "database": "authentik",
					"username": "authentik", "password": "s3cret",
				},
			})).To(Succeed())
			topo := &benchv1alpha1.AuthentikTopology{
				ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"},
				Spec: benchv1alpha1.AuthentikTopologySpec{
					Namespace: targetNs,
					Server:    benchv1alpha1.WorkloadSpec{Replicas: 1},
					Worker:    benchv1alpha1.WorkloadSpec{Replicas: 1},
					Postgres: benchv1alpha1.PostgresSpec{
						Mode:     benchv1alpha1.PostgresExternal,
						External: &benchv1alpha1.ExternalPostgresSpec{SecretRef: corev1.LocalObjectReference{Name: "external-pg"}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, topo)).To(Succeed())
		})

		AfterEach(func() {
			forceDelete(ctx, &benchv1alpha1.AuthentikTopology{ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"}})
			forceDelete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "authentik-" + resourceName}})
		})

		It("wires the referenced secret's connection into the chart values without touching CNPG", func() {
			r := &AuthentikTopologyReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(100)}
			_, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			var hr helmv2.HelmRelease
			Expect(k8sClient.Get(context.Background(), typeNamespacedName, &hr)).To(Succeed())
			var values render.AuthentikTopologyHelmValues
			Expect(json.Unmarshal(hr.Spec.Values.Raw, &values)).To(Succeed())
			Expect(values.Postgres["enabled"]).To(BeFalse())
			pg, _ := values.Authentik["postgresql"].(map[string]any)
			Expect(pg["host"]).To(Equal("pg.example.com"))
			Expect(pg["password"]).To(Equal("s3cret"))

			var topo benchv1alpha1.AuthentikTopology
			Expect(k8sClient.Get(context.Background(), typeNamespacedName, &topo)).To(Succeed())
			postgresReady := meta.FindStatusCondition(topo.Status.Conditions, benchv1alpha1.ConditionPostgresReady)
			Expect(postgresReady).NotTo(BeNil())
			Expect(postgresReady.Status).To(Equal(metav1.ConditionTrue))
		})
	})

	Context("clusterRef set without a multicluster tracker wired up", func() {
		It("reports DependencyNotReady and requeues instead of touching a remote cluster", func() {
			topo := &benchv1alpha1.AuthentikTopology{
				ObjectMeta: metav1.ObjectMeta{Name: "topology-remote", Namespace: "default"},
				Spec: benchv1alpha1.AuthentikTopologySpec{
					ClusterRef: corev1.LocalObjectReference{Name: "some-workload-cluster"},
					Namespace:  "authentik-topology-remote",
					Server:     benchv1alpha1.WorkloadSpec{Replicas: 1},
					Worker:     benchv1alpha1.WorkloadSpec{Replicas: 1},
					Postgres:   benchv1alpha1.PostgresSpec{Mode: benchv1alpha1.PostgresEmbedded},
				},
			}
			Expect(k8sClient.Create(context.Background(), topo)).To(Succeed())
			defer forceDelete(context.Background(), topo)

			// No Tracker configured (this suite only unit-tests reconcile
			// logic directly, without a running manager/ClusterCache) -
			// resolveClient must report this as "not ready yet", not panic.
			r := &AuthentikTopologyReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Recorder: record.NewFakeRecorder(100)}
			key := types.NamespacedName{Name: topo.Name, Namespace: topo.Namespace}
			result, err := r.Reconcile(context.Background(), reconcile.Request{NamespacedName: key})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))

			Expect(k8sClient.Get(context.Background(), key, topo)).To(Succeed())
			ready := meta.FindStatusCondition(topo.Status.Conditions, benchv1alpha1.ConditionReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(benchv1alpha1.ReasonDependencyNotReady))
		})
	})
})
