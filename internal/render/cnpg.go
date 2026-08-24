package render

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

// cnpgAPIVersion and cnpgKind identify the CloudNativePG Cluster CRD. Built
// as unstructured.Unstructured rather than importing CNPG's typed API, which
// would drag the whole cloudnative-pg module (and its own large dependency
// tree) in just for one CRD's shape.
const (
	cnpgAPIVersion = "postgresql.cnpg.io/v1"
	cnpgKind       = "Cluster"

	// defaultCNPGStorageSize is used when CNPGPostgresSpec.StorageSize is
	// unset - CNPG requires spec.storage.size, it has no built-in default.
	defaultCNPGStorageSize = "10Gi"

	// cnpgDatabase/cnpgOwner match the embedded chart's own defaults
	// (postgresql.auth.username/database: authentik), so the generated app
	// user looks the same regardless of which Postgres mode a topology uses.
	cnpgDatabase = "authentik"
	cnpgOwner    = "authentik"
)

// CNPGClusterName is deterministic from the topology name.
func CNPGClusterName(t *benchv1alpha1.AuthentikTopology) string {
	return t.Name + "-postgres"
}

// CNPGAppSecretName is the connection Secret CNPG generates for the
// bootstrapped app user/database, named "<cluster-name>-app" by convention.
func CNPGAppSecretName(t *benchv1alpha1.AuthentikTopology) string {
	return CNPGClusterName(t) + "-app"
}

// CNPGCluster builds the CloudNativePG Cluster CR for a topology in CNPG
// postgres mode: instances (>1 for HA) and storage sized/classed from
// spec.postgres.cnpg, bootstrapped with an "authentik" database/owner so its
// generated app secret matches the embedded mode's own username/database.
func CNPGCluster(t *benchv1alpha1.AuthentikTopology) *unstructured.Unstructured {
	cnpg := t.Spec.Postgres.CNPG
	instances := int64(3)
	if cnpg != nil && cnpg.Instances > 0 {
		instances = int64(cnpg.Instances)
	}
	storageSize := defaultCNPGStorageSize
	if cnpg != nil && cnpg.StorageSize != "" {
		storageSize = cnpg.StorageSize
	}

	storage := map[string]any{"size": storageSize}
	if cnpg != nil && cnpg.StorageClassName != "" {
		storage["storageClass"] = cnpg.StorageClassName
	}

	u := &unstructured.Unstructured{}
	u.SetAPIVersion(cnpgAPIVersion)
	u.SetKind(cnpgKind)
	u.SetName(CNPGClusterName(t))
	u.SetNamespace(t.Spec.Namespace)
	_ = unstructured.SetNestedField(u.Object, instances, "spec", "instances")
	_ = unstructured.SetNestedMap(u.Object, storage, "spec", "storage")
	_ = unstructured.SetNestedField(u.Object, cnpgDatabase, "spec", "bootstrap", "initdb", "database")
	_ = unstructured.SetNestedField(u.Object, cnpgOwner, "spec", "bootstrap", "initdb", "owner")
	return u
}

// CNPGReady reports whether a CNPG Cluster's "Ready" status condition is
// True.
func CNPGReady(u *unstructured.Unstructured) bool {
	conditions, found, err := unstructured.NestedSlice(u.Object, "status", "conditions")
	if err != nil || !found {
		return false
	}
	for _, c := range conditions {
		cond, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if cond["type"] == "Ready" {
			status, _ := cond["status"].(string)
			return status == "True"
		}
	}
	return false
}
