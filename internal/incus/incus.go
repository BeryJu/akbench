// Package incus holds everything akbench knows about talking to an Incus
// daemon directly - as opposed to through cluster-api-provider-incus, which
// does the actual provisioning: the connection Secret's shape, generating a
// client certificate for it, and checking/establishing that the daemon
// trusts that certificate. Infrastructure-provider-specific code lives in a
// package like this one per provider, so the web UI's provider-agnostic
// surface (internal/api/infrastructure.go) can grow e.g. an AWS sibling.
package incus

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

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

const clientCertValidity = 10 * 365 * 24 * time.Hour

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

// GenerateClientCert creates the self-signed TLS client certificate Incus
// expects a client to authenticate with.
func GenerateClientCert(name string) (certPEM, keyPEM string, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: name, Organization: []string{"authentik-bench"}},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(clientCertValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})), nil
}

// Fingerprint returns the SHA-256 fingerprint Incus lists a certificate under
// (`incus config trust list`), or "" for anything that isn't a PEM
// certificate.
func Fingerprint(certPEM string) string {
	cert, err := parseCert(certPEM)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
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
	// Reachable is true once a TLS connection to the daemon succeeded.
	Reachable bool `json:"reachable"`
	// Trusted is true when the daemon accepted the client certificate.
	Trusted bool `json:"trusted"`
	// ServerVersion is the daemon's own version, when Reachable.
	ServerVersion string `json:"serverVersion,omitempty"`
	// ServerCert/ServerCertFingerprint are the certificate the daemon
	// presented - returned whenever it failed verification, so the UI can
	// offer to pin it after the user has checked the fingerprint
	// (`incus info | grep fingerprint` on the server).
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
	body, presented, err := c.do(ctx, http.MethodGet, "/1.0", nil)
	if presented != nil && (err != nil || c.InsecureSkipVerify) {
		res.ServerCert = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: presented.Raw}))
		res.ServerCertFingerprint = Fingerprint(res.ServerCert)
	}
	if err != nil {
		if certErr, ok := errors.AsType[*certificateError](err); ok {
			res.Message = "the server's certificate could not be verified: " + certErr.Error() +
				" - pin the certificate below after checking its fingerprint against the server's own"
		} else {
			res.Message = err.Error()
		}
		return res
	}
	res.Reachable = true
	var info struct {
		Metadata struct {
			Auth        string `json:"auth"`
			Environment struct {
				ServerVersion string `json:"server_version"`
			} `json:"environment"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		res.Message = fmt.Sprintf("unexpected response from %s - is this an Incus server? (%v)", c.Server, err)
		return res
	}
	res.Trusted = info.Metadata.Auth == "trusted"
	res.ServerVersion = info.Metadata.Environment.ServerVersion
	if !res.Trusted {
		res.Message = "the server doesn't trust this client certificate yet - add it with " +
			"`incus config trust add-certificate`, or use a trust token"
	}
	return res
}

// TrustWithToken adds c's client certificate to the daemon's trust store
// using a token from `incus config trust add <name>` on the server - the
// way `incus remote add` does it, so nobody has to copy a certificate file
// onto the server.
func TrustWithToken(ctx context.Context, c Connection, token string) error {
	payload, err := json.Marshal(map[string]any{"type": "client", "trust_token": strings.TrimSpace(token)})
	if err != nil {
		return err
	}
	if _, _, err := c.do(ctx, http.MethodPost, "/1.0/certificates", payload); err != nil {
		if apiErr, ok := errors.AsType[*apiError](err); ok {
			return fmt.Errorf("the server rejected the trust token: %s", apiErr.message)
		}
		return err
	}
	return nil
}

type certificateError struct{ err error }

func (e *certificateError) Error() string { return e.err.Error() }

type apiError struct {
	status  int
	message string
}

func (e *apiError) Error() string { return fmt.Sprintf("%d: %s", e.status, e.message) }

// do performs one request against the daemon. presented is the leaf
// certificate the server presented, whenever a TLS handshake got that far.
func (c Connection) do(ctx context.Context, method, path string, payload []byte) (body []byte, presented *x509.Certificate, err error) {
	base, err := url.Parse(c.Server)
	if err != nil || base.Scheme != "https" || base.Host == "" {
		return nil, nil, fmt.Errorf("server must be an https:// URL")
	}
	keyPair, err := tls.X509KeyPair([]byte(c.ClientCert), []byte(c.ClientKey))
	if err != nil {
		return nil, nil, fmt.Errorf("client certificate: %w", err)
	}
	var pinned *x509.Certificate
	if c.ServerCert != "" {
		if pinned, err = parseCert(c.ServerCert); err != nil {
			return nil, nil, fmt.Errorf("server certificate: %w", err)
		}
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{keyPair},
		MinVersion:   tls.VersionTLS12,
		// Verification is done below, by hand, so the presented certificate
		// is always captured - and so a pinned certificate is matched exactly,
		// the way the incus client itself treats a remote's certificate,
		// rather than as a CA (Incus' self-signed server certificates rarely
		// carry a SAN for the address they're reached at).
		InsecureSkipVerify: true, //nolint:gosec // verified in VerifyConnection
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return &certificateError{errors.New("no certificate presented")}
			}
			presented = cs.PeerCertificates[0]
			switch {
			case c.InsecureSkipVerify:
				return nil
			case pinned != nil:
				if !bytes.Equal(presented.Raw, pinned.Raw) {
					return &certificateError{errors.New("it doesn't match the pinned server certificate")}
				}
				return nil
			default:
				intermediates := x509.NewCertPool()
				for _, cert := range cs.PeerCertificates[1:] {
					intermediates.AddCert(cert)
				}
				if _, err := presented.Verify(x509.VerifyOptions{DNSName: cs.ServerName, Intermediates: intermediates}); err != nil {
					return &certificateError{err}
				}
				return nil
			}
		},
	}
	httpClient := &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{TLSClientConfig: tlsConfig, Proxy: http.ProxyFromEnvironment},
	}

	target := base.JoinPath(path)
	if c.Project != "" {
		q := target.Query()
		q.Set("project", c.Project)
		target.RawQuery = q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		if certErr, ok := errors.AsType[*certificateError](err); ok {
			return nil, presented, certErr
		}
		return nil, presented, fmt.Errorf("could not reach %s: %w", c.Server, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, presented, err
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return body, presented, &apiError{status: resp.StatusCode, message: e.Error}
	}
	return body, presented, nil
}
