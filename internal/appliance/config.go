// Package appliance bootstraps the single-container akbench appliance: a k3s
// server (the container's PID 1) acting as the Cluster API management
// cluster, with Flux, the Cluster API providers and akbench itself installed
// on top by cmd/appliance. Incus is never part of the appliance - it is
// always an external daemon the management cluster reaches over the network.
package appliance

import (
	"fmt"
	"os"
	"strconv"
	"strings"
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
	// StateDir holds everything generated on first boot (API token, Incus
	// client certificate). Lives inside k3s's own data dir so the single
	// volume users mount for k3s persists it too.
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
	// AuthentikDeployments, ...) and the Incus credentials Secret live. Kept
	// at "default" to match what a `main`-era setup used, so migrated
	// objects (and `clusterctl move`, which can't rename namespaces) land in
	// the same place.
	Namespace string
	UIPort    string
	// APIToken, if set, pins the web UI's bearer token instead of the
	// generated one.
	APIToken string

	OIDCIssuer        string
	OIDCClientID      string
	OIDCScopes        string
	OIDCRequiredGroup string

	// CAPIVersion/CAPNVersion are the Cluster API and Incus provider
	// versions this image ships in its clusterctl local repository
	// (Dockerfile.appliance bakes them in from appliance/assets.lock).
	CAPIVersion string
	CAPNVersion string

	Incus IncusConfig
}

// IncusConfig is the external Incus daemon WorkloadClusters are provisioned
// on, written into the Secret every WorkloadCluster's spec.incus.lxcSecretRef
// names. Left entirely unset, the appliance doesn't manage that Secret at
// all (e.g. a migrated one is kept as-is).
type IncusConfig struct {
	SecretName         string
	Server             string
	Project            string
	ServerCert         string
	ClientCert         string
	ClientKey          string
	InsecureSkipVerify bool
}

// LoadConfig reads the appliance configuration from getenv (os.Getenv in
// production). PEM values (INCUS_SERVER_CERT, INCUS_CLIENT_CERT,
// INCUS_CLIENT_KEY) may be given inline or as a path to a mounted file.
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
		UIPort:            or("AKBENCH_UI_PORT", "8080"),
		APIToken:          getenv("AKBENCH_API_TOKEN"),
		OIDCIssuer:        getenv("AKBENCH_OIDC_ISSUER"),
		OIDCClientID:      getenv("AKBENCH_OIDC_CLIENT_ID"),
		OIDCScopes:        getenv("AKBENCH_OIDC_SCOPES"),
		OIDCRequiredGroup: getenv("AKBENCH_OIDC_REQUIRED_GROUP"),
		CAPIVersion:       getenv("AKBENCH_CAPI_VERSION"),
		CAPNVersion:       getenv("AKBENCH_CAPN_VERSION"),
		Incus: IncusConfig{
			SecretName:         or("INCUS_SECRET_NAME", "lxc-secret"),
			Server:             getenv("INCUS_SERVER"),
			Project:            or("INCUS_PROJECT", "default"),
			InsecureSkipVerify: getenv("INCUS_INSECURE_SKIP_VERIFY") == "true",
		},
	}
	var err error
	for _, f := range []struct {
		key string
		dst *string
	}{
		{"INCUS_SERVER_CERT", &cfg.Incus.ServerCert},
		{"INCUS_CLIENT_CERT", &cfg.Incus.ClientCert},
		{"INCUS_CLIENT_KEY", &cfg.Incus.ClientKey},
	} {
		if *f.dst, err = pemOrFile(getenv(f.key)); err != nil {
			return nil, fmt.Errorf("%s: %w", f.key, err)
		}
	}
	if (cfg.Incus.ClientCert == "") != (cfg.Incus.ClientKey == "") {
		return nil, fmt.Errorf("INCUS_CLIENT_CERT and INCUS_CLIENT_KEY must be set together")
	}
	if port, err := strconv.Atoi(cfg.UIPort); err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("AKBENCH_UI_PORT %q is not a valid port", cfg.UIPort)
	}
	if cfg.CAPIVersion == "" || cfg.CAPNVersion == "" {
		return nil, fmt.Errorf("AKBENCH_CAPI_VERSION and AKBENCH_CAPN_VERSION must be set (baked in by Dockerfile.appliance)")
	}
	return cfg, nil
}

// pemOrFile returns v itself when it is inline PEM, otherwise the contents
// of the file it names - so both `-e INCUS_CLIENT_CERT="$(cat c.crt)"` and
// `-e INCUS_CLIENT_CERT=/run/secrets/c.crt` work.
func pemOrFile(v string) (string, error) {
	if v == "" || strings.HasPrefix(strings.TrimSpace(v), "-----BEGIN") {
		return v, nil
	}
	b, err := os.ReadFile(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
