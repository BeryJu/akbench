package appliance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// Clusterctl provider-label prefixes of the local repository layout
// (<repo>/<label>/<version>/...), as written by
// hack/update-appliance-assets.sh.
const (
	coreLabel         = "cluster-api"
	bootstrapLabel    = "bootstrap-kubeadm"
	controlPlaneLabel = "control-plane-kubeadm"
	infraLabelPrefix  = "infrastructure-"
)

// Shipped is the provider set baked into this appliance image's clusterctl
// local repository - every infrastructure provider it *can* install, not
// the ones it has been asked to.
type Shipped struct {
	// CAPIVersion is the core/kubeadm Cluster API version, always installed.
	CAPIVersion string
	// Infrastructure maps an infrastructure provider's clusterctl name
	// (e.g. "incus") to the version this image ships.
	Infrastructure map[string]string
}

// DiscoverShipped reads the shipped provider set from the local repository
// under repoDir. Versions come from the image's own contents rather than
// separate configuration, so adding a provider (e.g. AWS) is purely a
// matter of adding it to appliance/assets.lock.
func DiscoverShipped(repoDir string) (*Shipped, error) {
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return nil, err
	}
	shipped := &Shipped{Infrastructure: map[string]string{}}
	versions := map[string]string{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		v, err := singleVersion(filepath.Join(repoDir, e.Name()))
		if err != nil {
			return nil, err
		}
		versions[e.Name()] = v
		if name, ok := strings.CutPrefix(e.Name(), infraLabelPrefix); ok {
			shipped.Infrastructure[name] = v
		}
	}
	shipped.CAPIVersion = versions[coreLabel]
	if shipped.CAPIVersion == "" || versions[bootstrapLabel] != shipped.CAPIVersion || versions[controlPlaneLabel] != shipped.CAPIVersion {
		return nil, fmt.Errorf("%s must ship %s, %s and %s at one version", repoDir, coreLabel, bootstrapLabel, controlPlaneLabel)
	}
	return shipped, nil
}

func singleVersion(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var versions []string
	for _, e := range entries {
		if e.IsDir() {
			versions = append(versions, e.Name())
		}
	}
	if len(versions) != 1 {
		return "", fmt.Errorf("%s: want exactly one version directory, found %v", dir, versions)
	}
	return versions[0], nil
}

// Desired is the provider set to install: the core Cluster API providers,
// plus the infrastructure providers enabled in the web UI. Enabled names
// this image doesn't ship are returned in unknown rather than failing the
// whole set.
func (s *Shipped) Desired(enabled []string) (providers []Provider, unknown []string) {
	providers = []Provider{
		{Type: "CoreProvider", Name: "cluster-api", Version: s.CAPIVersion},
		{Type: "BootstrapProvider", Name: "kubeadm", Version: s.CAPIVersion},
		{Type: "ControlPlaneProvider", Name: "kubeadm", Version: s.CAPIVersion},
	}
	seen := map[string]bool{}
	for _, name := range enabled {
		if seen[name] {
			continue
		}
		seen[name] = true
		v, ok := s.Infrastructure[name]
		if !ok {
			unknown = append(unknown, name)
			continue
		}
		providers = append(providers, Provider{Type: "InfrastructureProvider", Name: name, Version: v})
	}
	return providers, unknown
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
// --config/--kubeconfig flags) that bring installed up to desired: an `init`
// for providers that are missing entirely and an `upgrade apply` for ones
// older than desired. Providers already newer than desired are left alone -
// e.g. a management cluster migrated from a setup that was ahead of this
// image - and reported in skipped so the caller can warn.
func PlanClusterctl(installed, desired []Provider) (cmds [][]string, skipped []string, err error) {
	var initArgs, upgradeArgs []string
	for _, want := range desired {
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
