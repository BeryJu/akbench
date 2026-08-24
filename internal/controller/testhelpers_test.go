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
	"fmt"

	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// forceDelete deletes obj and, since no controller manager runs in these
// envtest specs to process the delete and clear the finalizer our
// controllers add, strips any finalizers directly so the object doesn't
// linger "Terminating" and collide with the next spec's fixture of the same
// name.
func forceDelete(ctx context.Context, obj client.Object) {
	key := client.ObjectKeyFromObject(obj)
	if err := k8sClient.Delete(ctx, obj); err != nil {
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "unexpected delete error: %v", err)
		return
	}
	Eventually(func() error {
		if err := k8sClient.Get(ctx, key, obj); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return err
		}
		if len(obj.GetFinalizers()) == 0 {
			return nil
		}
		obj.SetFinalizers(nil)
		return k8sClient.Update(ctx, obj)
	}).Should(Succeed())
}

// createReadyTopology creates an AuthentikTopology and directly patches its
// status to Ready, bypassing AuthentikTopologyReconciler. TestCase/TestRun
// reconciliation only needs a Ready topology's targetName/serviceEndpoint -
// exercising the real AuthentikTopologyReconciler (which needs a live
// helm-controller to ever reach Ready) belongs to authentiktopology_controller_test.go
// alone.
func createReadyTopology(ctx context.Context, name, targetName, host string, port int32) *benchv1alpha1.AuthentikTopology {
	topo := &benchv1alpha1.AuthentikTopology{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: benchv1alpha1.AuthentikTopologySpec{
			TargetName: targetName,
			Namespace:  "authentik-" + name,
			Server:     benchv1alpha1.WorkloadSpec{Replicas: 1},
			Worker:     benchv1alpha1.WorkloadSpec{Replicas: 1},
			Postgres:   benchv1alpha1.PostgresSpec{Mode: benchv1alpha1.PostgresEmbedded},
		},
	}
	Expect(k8sClient.Create(ctx, topo)).To(Succeed())

	topo.Status.ServiceEndpoint = fmt.Sprintf("%s:%d", host, port)
	topo.Status.Conditions = []metav1.Condition{{
		Type:               benchv1alpha1.ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             benchv1alpha1.ReasonReady,
		Message:            "faked ready for testing",
		LastTransitionTime: metav1.Now(),
		ObservedGeneration: topo.Generation,
	}}
	Expect(k8sClient.Status().Update(ctx, topo)).To(Succeed())
	return topo
}
