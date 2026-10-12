package controller

import (
	"context"
	"fmt"
	"net"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
	"github.com/goauthentik/authentik-bench/internal/incus"
	"github.com/goauthentik/authentik-bench/internal/render"
)

// ipv4EndpointGracePeriod is how long preferIPv4Endpoint waits for the load
// balancer to get an IPv4 address before accepting an IPv6 endpoint - on an
// IPv6-only Incus network it never will.
const ipv4EndpointGracePeriod = 3 * time.Minute

// lookupInstanceAddresses is incus.InstanceAddresses, swappable in tests.
var lookupInstanceAddresses = incus.InstanceAddresses

// preferIPv4Endpoint swaps an IPv6 controlPlaneEndpoint for the load
// balancer's IPv4 address, before the control plane exists. Returns true
// when the caller should requeue instead of creating the control plane.
//
// cluster-api-provider-incus takes whichever address its load balancer
// container reports first (its LXCCluster reconcileNormal: lbIPs[0], with a
// "TODO: care about IPv4 vs IPv6") and returns as soon as there is any - and
// IPv6 SLAAC routinely beats the IPv4 DHCP lease. The endpoint then only
// works from a management cluster that can route IPv6 to the Incus network,
// which a single-stack one (e.g. the appliance's k3s) can't: Cluster API's
// own remote connection probe fails, and the control plane is never seen as
// initialized. It has to be fixed before the control plane is created -
// kubeadm bakes the endpoint into the API server certificate's SANs - and
// both objects are safe to edit then: Cluster API copies the endpoint from
// the LXCCluster only while the Cluster's own is unset, and neither is
// validated against changes. The load balancer's haproxy listens on both
// families (render.dualStackHAProxyConfigTemplate), so either address works
// on the Incus side.
func (r *WorkloadClusterReconciler) preferIPv4Endpoint(ctx context.Context, wc *benchv1alpha1.WorkloadCluster, cluster *clusterv1.Cluster) (bool, error) {
	host := cluster.Spec.ControlPlaneEndpoint.Host
	if ip := net.ParseIP(host); ip == nil || ip.To4() != nil || wc.Spec.Incus == nil {
		return false, nil
	}
	log := logf.FromContext(ctx)

	var secret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Namespace: wc.Namespace, Name: wc.Spec.Incus.LXCSecretRef.Name}, &secret); err != nil {
		return false, fmt.Errorf("get incus credentials secret: %w", err)
	}
	ipv4, _, err := lookupInstanceAddresses(ctx, incus.FromSecret(&secret), render.LoadBalancerInstanceName(wc))
	if err != nil {
		return false, fmt.Errorf("look up the load balancer's addresses: %w", err)
	}
	if len(ipv4) == 0 {
		if time.Since(cluster.CreationTimestamp.Time) > ipv4EndpointGracePeriod {
			log.Info("Load balancer has no IPv4 address, keeping its IPv6 controlPlaneEndpoint", "host", host)
			return false, nil
		}
		return true, nil
	}

	lxcCluster := &unstructured.Unstructured{}
	lxcCluster.SetGroupVersionKind(render.LXCClusterGVK())
	if err := r.Get(ctx, client.ObjectKeyFromObject(cluster), lxcCluster); err != nil {
		return false, fmt.Errorf("get LXCCluster: %w", err)
	}
	lxcPatch := client.MergeFrom(lxcCluster.DeepCopy())
	if err := unstructured.SetNestedField(lxcCluster.Object, ipv4[0], "spec", "controlPlaneEndpoint", "host"); err != nil {
		return false, err
	}
	if err := r.Patch(ctx, lxcCluster, lxcPatch); err != nil {
		return false, fmt.Errorf("patch LXCCluster controlPlaneEndpoint: %w", err)
	}
	clusterPatch := client.MergeFrom(cluster.DeepCopy())
	cluster.Spec.ControlPlaneEndpoint.Host = ipv4[0]
	if err := r.Patch(ctx, cluster, clusterPatch); err != nil {
		return false, fmt.Errorf("patch Cluster controlPlaneEndpoint: %w", err)
	}
	log.Info("Switched controlPlaneEndpoint to the load balancer's IPv4 address", "from", host, "to", ipv4[0])
	return false, nil
}
