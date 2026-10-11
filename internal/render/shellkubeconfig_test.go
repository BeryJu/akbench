package render

import (
	"testing"

	"k8s.io/client-go/tools/clientcmd"
)

// fakeWorkloadKubeconfig mirrors the shape a real CAPI kubeconfig Secret
// decodes to (verified against a live one): one cluster/context/user, all
// keyed by the workload cluster's own name.
func fakeWorkloadKubeconfig(t *testing.T, name string) []byte {
	t.Helper()
	yaml := `
apiVersion: v1
kind: Config
clusters:
- name: ` + name + `
  cluster:
    server: https://` + name + `.example:6443
contexts:
- name: ` + name + `-admin@` + name + `
  context:
    cluster: ` + name + `
    user: ` + name + `-admin
current-context: ` + name + `-admin@` + name + `
users:
- name: ` + name + `-admin
  user:
    token: fake-token-` + name + `
`
	return []byte(yaml)
}

func TestShellKubeconfigIncludesManagementContext(t *testing.T) {
	raw, err := ShellKubeconfig(nil)
	if err != nil {
		t.Fatalf("ShellKubeconfig: %v", err)
	}
	cfg, err := clientcmd.Load(raw)
	if err != nil {
		t.Fatalf("load merged kubeconfig: %v", err)
	}
	if cfg.CurrentContext != managementContextName {
		t.Errorf("current-context = %q, want %q", cfg.CurrentContext, managementContextName)
	}
	cluster, ok := cfg.Clusters[managementContextName]
	if !ok {
		t.Fatalf("clusters[%q] missing", managementContextName)
	}
	if cluster.Server != inClusterAPIServer {
		t.Errorf("management cluster server = %q, want %q", cluster.Server, inClusterAPIServer)
	}
	authInfo, ok := cfg.AuthInfos[managementContextName]
	if !ok {
		t.Fatalf("users[%q] missing", managementContextName)
	}
	if authInfo.TokenFile != inClusterTokenPath {
		t.Errorf("management user tokenFile = %q, want %q", authInfo.TokenFile, inClusterTokenPath)
	}
}

func TestShellKubeconfigMergesEveryWorkloadCluster(t *testing.T) {
	raw, err := ShellKubeconfig([]WorkloadClusterKubeconfig{
		{Name: "cluster-a", Bytes: fakeWorkloadKubeconfig(t, "cluster-a")},
		{Name: "cluster-b", Bytes: fakeWorkloadKubeconfig(t, "cluster-b")},
	})
	if err != nil {
		t.Fatalf("ShellKubeconfig: %v", err)
	}
	cfg, err := clientcmd.Load(raw)
	if err != nil {
		t.Fatalf("load merged kubeconfig: %v", err)
	}

	// The management context plus both workload clusters' own, all present
	// at once - this is the point of the merge (the kubectl shell sees every
	// context from one Secret).
	for _, name := range []string{managementContextName, "cluster-a", "cluster-b"} {
		if _, ok := cfg.Clusters[name]; !ok {
			t.Errorf("clusters[%q] missing", name)
		}
	}
	if len(cfg.Contexts) != 3 {
		t.Errorf("contexts = %v, want 3 entries (management + cluster-a + cluster-b)", cfg.Contexts)
	}
	if cfg.Clusters["cluster-a"].Server != "https://cluster-a.example:6443" {
		t.Errorf("cluster-a server = %q, want the workload cluster's own", cfg.Clusters["cluster-a"].Server)
	}
}

func TestShellKubeconfigRejectsUnparsableWorkloadKubeconfig(t *testing.T) {
	_, err := ShellKubeconfig([]WorkloadClusterKubeconfig{
		{Name: "broken", Bytes: []byte("not: valid: yaml: [")},
	})
	if err == nil {
		t.Fatal("expected an error for an unparsable workload cluster kubeconfig, got nil")
	}
}
