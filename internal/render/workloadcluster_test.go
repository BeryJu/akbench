package render

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

func TestKubeadmControlPlaneBindsDualStack(t *testing.T) {
	wc := &benchv1alpha1.WorkloadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "wc-a", Namespace: "default"},
		Spec: benchv1alpha1.WorkloadClusterSpec{
			KubernetesVersion: "v1.31.0",
			Provider:          benchv1alpha1.InfrastructureIncus,
			ControlPlane:      benchv1alpha1.MachineSpec{Replicas: 1},
			Workers:           benchv1alpha1.MachineSpec{Replicas: 2},
		},
	}

	kcp := KubeadmControlPlane(wc)

	var bindAddress *string
	for _, arg := range kcp.Spec.KubeadmConfigSpec.ClusterConfiguration.APIServer.ExtraArgs {
		if arg.Name == "bind-address" {
			bindAddress = arg.Value
		}
	}
	if bindAddress == nil || *bindAddress != "::" {
		t.Fatalf("apiServer.extraArgs bind-address = %v, want \"::\" - kube-apiserver must listen on "+
			"both address families since Incus's load balancer address family isn't under our control",
			bindAddress)
	}
}

func TestLXCClusterOverridesHAProxyForDualStack(t *testing.T) {
	wc := &benchv1alpha1.WorkloadCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "wc-a", Namespace: "default"},
		Spec: benchv1alpha1.WorkloadClusterSpec{
			KubernetesVersion: "v1.31.0",
			Provider:          benchv1alpha1.InfrastructureIncus,
			ControlPlane:      benchv1alpha1.MachineSpec{Replicas: 1},
			Workers:           benchv1alpha1.MachineSpec{Replicas: 2},
			Incus: &benchv1alpha1.IncusProviderSpec{
				LXCSecretRef: corev1.LocalObjectReference{Name: "lxc-secret"},
			},
		},
	}

	lxcCluster := LXCCluster(wc)

	tmpl, found, err := unstructured.NestedString(lxcCluster.Object, "spec", "loadBalancer", "lxc", "customHAProxyConfigTemplate")
	if err != nil || !found {
		t.Fatalf("customHAProxyConfigTemplate not set: found=%v err=%v", found, err)
	}
	// cluster-api-provider-incus's own default template only adds this line
	// when a configData.IPv6 flag is set, which its own code never sets - see
	// dualStackHAProxyConfigTemplate's doc comment. Our override must not
	// depend on that flag at all.
	if !strings.Contains(tmpl, "bind :::{{ .FrontendControlPlanePort }}") {
		t.Fatalf("customHAProxyConfigTemplate missing the unconditional IPv6 bind line:\n%s", tmpl)
	}
	if !strings.Contains(tmpl, "bind *:{{ .FrontendControlPlanePort }}") {
		t.Fatalf("customHAProxyConfigTemplate missing the IPv4 bind line:\n%s", tmpl)
	}
	// Must stay compatible with the exact fields/funcs
	// internal/loadbalancer.renderHaproxyConfiguration feeds into the
	// template today.
	for _, want := range []string{"{{ if .EnableHealthzCheck", "{{range $server, $backend := .BackendServers}}", "JoinHostPort"} {
		if !strings.Contains(tmpl, want) {
			t.Fatalf("customHAProxyConfigTemplate missing expected upstream template fragment %q:\n%s", want, tmpl)
		}
	}
}
