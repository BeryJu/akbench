package appliance

import (
	"encoding/json"
	"fmt"

	utilversion "k8s.io/apimachinery/pkg/util/version"
)

// Provider is one entry of clusterctl's own inventory
// (providers.clusterctl.cluster.x-k8s.io), which `clusterctl move` also
// checks the target against - the reason the appliance installs Cluster API
// with clusterctl itself rather than another installer.
type Provider struct {
	Type    string
	Name    string
	Version string
}

// providerFlags maps a clusterctl provider type to its init/upgrade flag.
var providerFlags = map[string]string{
	"CoreProvider":           "--core",
	"BootstrapProvider":      "--bootstrap",
	"ControlPlaneProvider":   "--control-plane",
	"InfrastructureProvider": "--infrastructure",
}

// PinnedProviders is the provider set this appliance image ships (served from
// the clusterctl local repository baked into it - see Dockerfile.appliance).
func PinnedProviders(cfg *Config) []Provider {
	return []Provider{
		{Type: "CoreProvider", Name: "cluster-api", Version: cfg.CAPIVersion},
		{Type: "BootstrapProvider", Name: "kubeadm", Version: cfg.CAPIVersion},
		{Type: "ControlPlaneProvider", Name: "kubeadm", Version: cfg.CAPIVersion},
		{Type: "InfrastructureProvider", Name: "incus", Version: cfg.CAPNVersion},
	}
}

// ParseInventory reads `kubectl get providers.clusterctl.cluster.x-k8s.io -A
// -o json` output.
func ParseInventory(raw []byte) ([]Provider, error) {
	var list struct {
		Items []struct {
			Type         string `json:"type"`
			ProviderName string `json:"providerName"`
			Version      string `json:"version"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	out := make([]Provider, 0, len(list.Items))
	for _, it := range list.Items {
		out = append(out, Provider{Type: it.Type, Name: it.ProviderName, Version: it.Version})
	}
	return out, nil
}

// PlanClusterctl returns the clusterctl invocations (without the global
// --config/--kubeconfig flags) that bring installed up to pinned: an `init`
// for providers that are missing entirely and an `upgrade apply` for ones
// older than pinned. Providers already newer than pinned are left alone -
// e.g. a management cluster migrated from a setup that was ahead of this
// image - and reported in skipped so the caller can warn.
func PlanClusterctl(installed, pinned []Provider) (cmds [][]string, skipped []string, err error) {
	var initArgs, upgradeArgs []string
	for _, want := range pinned {
		flag, ok := providerFlags[want.Type]
		if !ok {
			return nil, nil, fmt.Errorf("unknown provider type %q", want.Type)
		}
		ref := fmt.Sprintf("%s:%s", want.Name, want.Version)
		have := findProvider(installed, want)
		if have == nil {
			initArgs = append(initArgs, flag, ref)
			continue
		}
		cmp, err := compareVersions(have.Version, want.Version)
		if err != nil {
			return nil, nil, fmt.Errorf("%s %s: %w", want.Type, want.Name, err)
		}
		switch {
		case cmp < 0:
			upgradeArgs = append(upgradeArgs, flag, ref)
		case cmp > 0:
			skipped = append(skipped, fmt.Sprintf("%s %s is at %s, newer than this image's %s", want.Type, want.Name, have.Version, want.Version))
		}
	}
	if len(initArgs) > 0 {
		cmds = append(cmds, append([]string{"init", "--wait-providers"}, initArgs...))
	}
	if len(upgradeArgs) > 0 {
		cmds = append(cmds, append([]string{"upgrade", "apply", "--wait-providers"}, upgradeArgs...))
	}
	return cmds, skipped, nil
}

func findProvider(installed []Provider, want Provider) *Provider {
	for i := range installed {
		if installed[i].Type == want.Type && installed[i].Name == want.Name {
			return &installed[i]
		}
	}
	return nil
}

func compareVersions(a, b string) (int, error) {
	va, err := utilversion.ParseSemantic(a)
	if err != nil {
		return 0, err
	}
	return va.Compare(b)
}
