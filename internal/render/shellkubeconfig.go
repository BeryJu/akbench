package render

import (
	"fmt"
	"maps"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// managementContextName names the synthesized management-cluster entry
// ShellKubeconfig adds alongside every workload cluster's own.
const managementContextName = "management"

// The management cluster's own API server/CA/token, referenced by path
// rather than embedded: every pod (including whichever one actually reads
// this kubeconfig - cmd/apiserver's) has its own copy of these at the same,
// well-known, standard paths, kept fresh by kubelet as the token rotates.
// Synthesizing them into the merged kubeconfig means the management
// cluster is just another entry, alongside every WorkloadCluster.
const (
	inClusterAPIServer  = "https://kubernetes.default.svc"
	inClusterCACertPath = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	inClusterTokenPath  = "/var/run/secrets/kubernetes.io/serviceaccount/token"
)

// WorkloadClusterKubeconfig pairs a WorkloadCluster's name with its raw,
// CAPI-produced kubeconfig bytes (see capikubeconfig.FromSecret).
type WorkloadClusterKubeconfig struct {
	Name  string
	Bytes []byte
}

// ShellKubeconfig merges the management cluster's own (synthesized,
// see above) context with every given WorkloadCluster's own kubeconfig into
// one kubeconfig the in-browser kubectl shell is pointed at via KUBECONFIG,
// so every context is available without per-cluster Deployment changes as
// WorkloadClusters come and go - only this rendered Secret's content does.
//
// No renaming/prefixing of cluster/context/user names: CAPI's own
// kubeconfig Secrets already key everything by the WorkloadCluster's own
// object name (verified against a live one), which is unique within its
// namespace - the same uniqueness this merge relies on.
func ShellKubeconfig(workloadClusters []WorkloadClusterKubeconfig) ([]byte, error) {
	merged := clientcmdapi.NewConfig()
	merged.Clusters[managementContextName] = &clientcmdapi.Cluster{
		Server:               inClusterAPIServer,
		CertificateAuthority: inClusterCACertPath,
	}
	merged.AuthInfos[managementContextName] = &clientcmdapi.AuthInfo{
		TokenFile: inClusterTokenPath,
	}
	merged.Contexts[managementContextName] = &clientcmdapi.Context{
		Cluster:  managementContextName,
		AuthInfo: managementContextName,
	}
	merged.CurrentContext = managementContextName

	for _, wc := range workloadClusters {
		cfg, err := clientcmd.Load(wc.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse kubeconfig for %q: %w", wc.Name, err)
		}
		maps.Copy(merged.Clusters, cfg.Clusters)
		maps.Copy(merged.AuthInfos, cfg.AuthInfos)
		maps.Copy(merged.Contexts, cfg.Contexts)
	}

	return clientcmd.Write(*merged)
}
