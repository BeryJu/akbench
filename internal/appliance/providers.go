package appliance

import "strings"

// The contract between the appliance and the web UI (internal/api's
// infrastructure endpoints) for choosing infrastructure providers: the UI
// writes the desired set into ProvidersConfigMap, the appliance's provider
// loop installs it with clusterctl and reports back in StatusConfigMap.
// Both live in SystemNamespace. Outside the appliance neither exists and the
// UI only reports what is installed (clusterctl's own inventory).
const (
	SystemNamespace = "authentik-bench"

	ProvidersConfigMap = "appliance-providers"
	// ProvidersKeyInfrastructure is a comma-separated list of clusterctl
	// infrastructure provider names, e.g. "incus".
	ProvidersKeyInfrastructure = "infrastructure"

	StatusConfigMap = "appliance-status"
	// StatusKeyShipped is a JSON object mapping each infrastructure provider
	// this image can install to its version.
	StatusKeyShipped = "shipped"
	// StatusKeyError is the last provider sync's error, empty on success.
	StatusKeyError = "error"
	// StatusKeySyncedAt is when the last provider sync finished (RFC 3339).
	StatusKeySyncedAt = "syncedAt"
)

// ParseProviderList splits a ProvidersKeyInfrastructure value.
func ParseProviderList(v string) []string {
	var out []string
	for p := range strings.SplitSeq(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
