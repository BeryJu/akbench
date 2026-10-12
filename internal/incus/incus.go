// Package incus holds everything akbench knows about talking to an Incus
// daemon directly - as opposed to through cluster-api-provider-incus, which
// does the actual provisioning: the connection Secret's shape, generating a
// client certificate for it, checking/establishing that the daemon trusts
// that certificate, and reading instance addresses. All daemon access goes
// through the official client (github.com/lxc/incus/v7/client), so TLS and
// certificate handling behave exactly like the incus CLI's.
// Infrastructure-provider-specific code lives in a package like this one per
// provider, so the web UI's provider-agnostic surface
// (internal/api/infrastructure.go) can grow e.g. an AWS sibling.
package incus

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	incusclient "github.com/lxc/incus/v7/client"
	"github.com/lxc/incus/v7/shared/api"
	localtls "github.com/lxc/incus/v7/shared/tls"
	corev1 "k8s.io/api/core/v1"
)

// Keys of the connection Secret a WorkloadCluster's spec.incus.lxcSecretRef
// names - the shape cluster-api-provider-incus itself reads, which csi-incus
// is configured from as well (internal/render.CSIIncusHelmValues).
const (
	KeyServer             = "server"
	KeyProject            = "project"
	KeyServerCert         = "server-crt"
	KeyClientCert         = "client-crt"
	KeyClientKey          = "client-key"
	KeyInsecureSkipVerify = "insecure-skip-verify"
)

const userAgent = "authentik-bench"

// Connection is a decoded connection Secret.
type Connection struct {
	Server             string
	Project            string
	ServerCert         string
	ClientCert         string
	ClientKey          string
	InsecureSkipVerify bool
}

// FromSecret decodes a connection Secret.
func FromSecret(s *corev1.Secret) Connection {
	return Connection{
		Server:             string(s.Data[KeyServer]),
		Project:            string(s.Data[KeyProject]),
		ServerCert:         string(s.Data[KeyServerCert]),
		ClientCert:         string(s.Data[KeyClientCert]),
		ClientKey:          string(s.Data[KeyClientKey]),
		InsecureSkipVerify: string(s.Data[KeyInsecureSkipVerify]) == "true",
	}
}

// SecretData encodes c as connection Secret data, leaving optional keys out
// when unset.
func (c Connection) SecretData() map[string][]byte {
	data := map[string][]byte{
		KeyServer:     []byte(c.Server),
		KeyClientCert: []byte(c.ClientCert),
		KeyClientKey:  []byte(c.ClientKey),
	}
	if c.Project != "" {
		data[KeyProject] = []byte(c.Project)
	}
	if c.ServerCert != "" {
		data[KeyServerCert] = []byte(c.ServerCert)
	}
	if c.InsecureSkipVerify {
		data[KeyInsecureSkipVerify] = []byte("true")
	}
	return data
}

// Validate checks c is usable for provisioning.
func (c Connection) Validate() error {
	u, err := url.Parse(c.Server)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("server must be an https:// URL, e.g. https://incus.example.com:8443")
	}
	if c.ClientCert == "" || c.ClientKey == "" {
		return errors.New("a client certificate and key are required")
	}
	if _, err := tls.X509KeyPair([]byte(c.ClientCert), []byte(c.ClientKey)); err != nil {
		return fmt.Errorf("client certificate and key don't form a valid pair: %w", err)
	}
	if c.ServerCert != "" {
		if _, err := parseCert(c.ServerCert); err != nil {
			return fmt.Errorf("server certificate: %w", err)
		}
	}
	return nil
}

// connect opens a client for c, scoped to its project.
func (c Connection) connect(ctx context.Context) (incusclient.InstanceServer, error) {
	server, err := incusclient.ConnectIncusWithContext(ctx, c.Server, &incusclient.ConnectionArgs{
		TLSClientCert:      c.ClientCert,
		TLSClientKey:       c.ClientKey,
		TLSServerCert:      c.ServerCert,
		InsecureSkipVerify: c.InsecureSkipVerify,
		UserAgent:          userAgent,
	})
	if err != nil {
		return nil, err
	}
	if c.Project != "" {
		server = server.UseProject(c.Project)
	}
	return server, nil
}

// GenerateClientCert creates a client certificate the way the incus CLI does
// for itself - the kind a daemon trusts with `incus config trust add`.
func GenerateClientCert() (certPEM, keyPEM string, err error) {
	cert, key, err := localtls.GenerateMemCert(true, false)
	if err != nil {
		return "", "", err
	}
	return string(cert), string(key), nil
}

// Fingerprint returns the SHA-256 fingerprint Incus lists a certificate under
// (`incus config trust list`), or "" for anything that isn't a PEM
// certificate.
func Fingerprint(certPEM string) string {
	fp, err := localtls.CertFingerprintStr(certPEM)
	if err != nil {
		return ""
	}
	return fp
}

func parseCert(certPEM string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("not a PEM certificate")
	}
	return x509.ParseCertificate(block.Bytes)
}

// ProbeResult is what Probe found out about a connection.
type ProbeResult struct {
	// Reachable is true once a connection to the daemon succeeded.
	Reachable bool `json:"reachable"`
	// Trusted is true when the daemon accepted the client certificate.
	Trusted bool `json:"trusted"`
	// ServerVersion is the daemon's own version, when Reachable.
	ServerVersion string `json:"serverVersion,omitempty"`
	// ServerCert/ServerCertFingerprint are the certificate the daemon
	// presents - returned whenever it couldn't be verified (or verification
	// is off), so the UI can offer to pin it after the user has checked the
	// fingerprint (`incus info` on the server).
	ServerCert            string `json:"serverCert,omitempty"`
	ServerCertFingerprint string `json:"serverCertFingerprint,omitempty"`
	// Message explains a failure in plain words.
	Message string `json:"message,omitempty"`
}

// Probe connects to the daemon and reports whether it is reachable and
// trusts c's client certificate. Errors are reported in the result rather
// than returned: every failure mode is something to show the user.
func Probe(ctx context.Context, c Connection) ProbeResult {
	var res ProbeResult
	server, err := c.connect(ctx)
	if err == nil {
		var info *api.Server
		if info, _, err = server.GetServer(); err == nil {
			res.Reachable = true
			res.Trusted = info.Auth == "trusted"
			res.ServerVersion = info.Environment.ServerVersion
		}
	}

	certErr := err != nil && isCertificateError(err)
	if certErr || (res.Reachable && c.InsecureSkipVerify) {
		// The same fetch `incus remote add` uses to show a server's
		// certificate before accepting it.
		if cert, fetchErr := localtls.GetRemoteCertificate(c.Server, userAgent); fetchErr == nil {
			res.ServerCert = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}))
			res.ServerCertFingerprint = localtls.CertFingerprint(cert)
		}
	}

	switch {
	case certErr:
		res.Message = "the server's certificate could not be verified (" + err.Error() +
			") - pin the certificate below after checking its fingerprint against the server's own"
	case err != nil:
		res.Message = fmt.Sprintf("could not reach %s: %v", c.Server, err)
	case !res.Trusted:
		res.Message = "the server doesn't trust this client certificate yet - add it with " +
			"`incus config trust add-certificate`, or use a trust token"
	}
	return res
}

// isCertificateError reports a TLS verification failure, as opposed to the
// server being unreachable altogether.
func isCertificateError(err error) bool {
	var (
		unknownAuthority x509.UnknownAuthorityError
		hostname         x509.HostnameError
		invalid          x509.CertificateInvalidError
		verification     *tls.CertificateVerificationError
	)
	return errors.As(err, &unknownAuthority) || errors.As(err, &hostname) || errors.As(err, &invalid) ||
		errors.As(err, &verification) || strings.Contains(err.Error(), "certificate")
}

// TrustWithToken adds c's client certificate to the daemon's trust store
// using a token from `incus config trust add <name>` on the server - the
// way `incus remote add` does it, so nobody has to copy a certificate file
// onto the server.
func TrustWithToken(ctx context.Context, c Connection, token string) error {
	server, err := c.connect(ctx)
	if err != nil {
		return err
	}
	if err := server.CreateCertificate(api.CertificatesPost{
		Type:       api.CertificateTypeClient,
		TrustToken: strings.TrimSpace(token),
	}); err != nil {
		return fmt.Errorf("the server rejected the trust token: %w", err)
	}
	return nil
}

// InstanceAddresses returns an instance's host-facing addresses split by
// family, filtered the way cluster-api-provider-incus itself picks an
// instance's addresses (internal/lxc.ParseHostAddresses there): only NICs
// with a host-side interface, only global scope, no /32 or /128 VIPs.
func InstanceAddresses(ctx context.Context, c Connection, name string) (ipv4, ipv6 []string, err error) {
	server, err := c.connect(ctx)
	if err != nil {
		return nil, nil, err
	}
	state, _, err := server.GetInstanceState(name)
	if err != nil {
		return nil, nil, fmt.Errorf("get state of instance %s: %w", name, err)
	}
	for _, nic := range state.Network {
		if nic.Type == "loopback" || nic.HostName == "" {
			continue
		}
		for _, a := range nic.Addresses {
			switch {
			case a.Scope != "global":
			case a.Family == "inet" && a.Netmask != "32":
				ipv4 = append(ipv4, a.Address)
			case a.Family == "inet6" && a.Netmask != "128":
				ipv6 = append(ipv6, a.Address)
			}
		}
	}
	slices.Sort(ipv4)
	slices.Sort(ipv6)
	return ipv4, ipv6, nil
}
