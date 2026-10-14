// Package appliance bootstraps the single-container akbench appliance: a k3s
// server (the container's PID 1) acting as the Cluster API management
// cluster, with Flux, Cluster API and akbench itself installed on top by
// cmd/appliance. The appliance itself is infrastructure-agnostic: which
// infrastructure providers get installed (Incus today) - and every
// provider's own connection settings - are chosen in the web UI, never
// baked into the container's configuration.
package appliance

import (
	"fmt"
	"strconv"
)

// Build-time defaults, set via -ldflags -X by Dockerfile.appliance so an
// image always installs the operator/apiserver/chart from the same build it
// was cut from. Every one of them can be overridden at runtime through the
// matching AKBENCH_* environment variable.
var (
	Version        = "dev"
	OperatorImage  = "ghcr.io/beryju/akbench"
	APIServerImage = "ghcr.io/beryju/akbench-apiserver"
	OperatorChart  = "oci://ghcr.io/beryju/akbench-charts/authentik-bench-operator"
)

// Config is the appliance's runtime configuration, read from the container's
// environment.
type Config struct {
	// StateDir holds everything generated on first boot (the API token).
	// Lives inside k3s's own data dir so the single volume users mount for
	// k3s persists it too.
	StateDir string
	// Kubeconfig is the admin kubeconfig k3s writes for its own API.
	Kubeconfig string
	// AssetsDir holds the manifests baked into the image at build time
	// (Flux's install.yaml, the clusterctl local repository + config).
	AssetsDir string

	Version        string
	OperatorImage  string
	APIServerImage string
	OperatorChart  string

	// Namespace is where users' own akbench objects (WorkloadClusters,
	// AuthentikDeployments, infrastructure credentials, ...) live. Kept at
	// "default" to match what a `main`-era setup used, so migrated objects
	// (and `clusterctl move`, which can't rename namespaces) land in the
	// same place.
	Namespace string
	UIPort    string
	// APIToken, if set, pins the web UI's bearer token instead of the
	// generated one.
	APIToken string

	OIDCIssuer        string
	OIDCClientID      string
	OIDCScopes        string
	OIDCRequiredGroup string
}

// LoadConfig reads the appliance configuration from getenv (os.Getenv in
// production).
func LoadConfig(getenv func(string) string) (*Config, error) {
	or := func(key, def string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return def
	}
	cfg := &Config{
		StateDir:          or("AKBENCH_STATE_DIR", "/var/lib/rancher/k3s/akbench"),
		Kubeconfig:        or("AKBENCH_KUBECONFIG", "/etc/rancher/k3s/k3s.yaml"),
		AssetsDir:         or("AKBENCH_ASSETS_DIR", "/opt/akbench"),
		Version:           or("AKBENCH_VERSION", Version),
		OperatorImage:     or("AKBENCH_OPERATOR_IMAGE", OperatorImage),
		APIServerImage:    or("AKBENCH_APISERVER_IMAGE", APIServerImage),
		OperatorChart:     or("AKBENCH_OPERATOR_CHART", OperatorChart),
		Namespace:         or("AKBENCH_NAMESPACE", "default"),
		UIPort:            or("AKBENCH_UI_PORT", "8443"),
		APIToken:          getenv("AKBENCH_API_TOKEN"),
		OIDCIssuer:        getenv("AKBENCH_OIDC_ISSUER"),
		OIDCClientID:      getenv("AKBENCH_OIDC_CLIENT_ID"),
		OIDCScopes:        getenv("AKBENCH_OIDC_SCOPES"),
		OIDCRequiredGroup: getenv("AKBENCH_OIDC_REQUIRED_GROUP"),
	}
	if port, err := strconv.Atoi(cfg.UIPort); err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("AKBENCH_UI_PORT %q is not a valid port", cfg.UIPort)
	}
	return cfg, nil
}
