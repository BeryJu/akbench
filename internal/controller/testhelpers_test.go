package controller

import (
	"context"
	"fmt"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	benchv1alpha1 "github.com/authentik-labs/akbench/api/v1alpha1"
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

// createReadyDeployment creates an AuthentikDeployment and directly patches its
// status to Ready, bypassing AuthentikDeploymentReconciler. TestCase/TestRun
// reconciliation only needs a Ready deployment's targetName/serviceEndpoint -
// exercising the real AuthentikDeploymentReconciler (which needs a live
// helm-controller to ever reach Ready) belongs to authentikdeployment_controller_test.go
// alone.
func createReadyDeployment(ctx context.Context, name, targetName, host string) *benchv1alpha1.AuthentikDeployment {
	const port = 80
	topo := &benchv1alpha1.AuthentikDeployment{
		Name: name, Namespace: "default",
		Spec: benchv1alpha1.AuthentikDeploymentSpec{
			ClusterRef: corev1.LocalObjectReference{Name: testClusterName},
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
