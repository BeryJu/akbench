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

package render

import (
	"fmt"
	"maps"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// managementContextName names the synthesized management-cluster entry
// HeadlampKubeconfig adds alongside every workload cluster's own.
const managementContextName = "management"

// The management cluster's own API server/CA/token, referenced by path
// rather than embedded: every pod (including whichever one actually reads
// this kubeconfig - Headlamp's) has its own copy of these at the same,
// well-known, standard paths, kept fresh by kubelet as the token rotates.
// This is exactly what Headlamp's own config.inCluster: true does
// internally - synthesizing it into the merged kubeconfig instead means
// Headlamp can run in plain kubeconfig mode and still see the management
// cluster as just another entry, alongside every WorkloadCluster.
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

// HeadlampKubeconfig merges the management cluster's own (synthesized,
// see above) context with every given WorkloadCluster's own kubeconfig into
// one kubeconfig Headlamp can be pointed at via KUBECONFIG, so its cluster
// switcher shows all of them without per-cluster Deployment changes as
// WorkloadClusters come and go - only this rendered Secret's content does.
//
// No renaming/prefixing of cluster/context/user names: CAPI's own
// kubeconfig Secrets already key everything by the WorkloadCluster's own
// object name (verified against a live one), which is unique within its
// namespace - the same uniqueness this merge relies on.
func HeadlampKubeconfig(workloadClusters []WorkloadClusterKubeconfig) ([]byte, error) {
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
