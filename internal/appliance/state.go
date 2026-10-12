package appliance

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	tokenFile      = "api-token"
	incusCertFile  = "incus-client.crt"
	incusKeyFile   = "incus-client.key"
	incusCertValid = 10 * 365 * 24 * time.Hour
)

// State is what the appliance generates once and then keeps for the
// lifetime of its volume.
type State struct {
	APIToken string
	// IncusClientCert/Key are the certificate the appliance authenticates to
	// Incus with - either the user-supplied INCUS_CLIENT_CERT/KEY or one
	// generated on first boot.
	IncusClientCert string
	IncusClientKey  string
	// IncusCertGenerated is true when IncusClientCert was generated here, so
	// the user still has to trust it on the Incus side.
	IncusCertGenerated bool
}

// EnsureState loads the persisted state from cfg.StateDir, generating any
// missing piece. Safe to call on every boot.
func EnsureState(cfg *Config) (*State, error) {
	if err := os.MkdirAll(cfg.StateDir, 0o700); err != nil {
		return nil, err
	}
	st := &State{APIToken: cfg.APIToken}
	if st.APIToken == "" {
		token, err := readOrCreate(filepath.Join(cfg.StateDir, tokenFile), func() ([]byte, error) {
			b := make([]byte, 32)
			if _, err := rand.Read(b); err != nil {
				return nil, err
			}
			return []byte(hex.EncodeToString(b)), nil
		})
		if err != nil {
			return nil, fmt.Errorf("api token: %w", err)
		}
		st.APIToken = strings.TrimSpace(string(token))
	}

	if cfg.Incus.ClientCert != "" {
		st.IncusClientCert, st.IncusClientKey = cfg.Incus.ClientCert, cfg.Incus.ClientKey
		return st, nil
	}
	certPath, keyPath := filepath.Join(cfg.StateDir, incusCertFile), filepath.Join(cfg.StateDir, incusKeyFile)
	cert, certErr := os.ReadFile(certPath)
	key, keyErr := os.ReadFile(keyPath)
	if certErr != nil || keyErr != nil {
		if (certErr != nil && !errors.Is(certErr, fs.ErrNotExist)) || (keyErr != nil && !errors.Is(keyErr, fs.ErrNotExist)) {
			return nil, fmt.Errorf("read incus client certificate: %w", errors.Join(certErr, keyErr))
		}
		var err error
		if cert, key, err = generateClientCert(); err != nil {
			return nil, fmt.Errorf("generate incus client certificate: %w", err)
		}
		if err := os.WriteFile(keyPath, key, 0o600); err != nil {
			return nil, err
		}
		if err := os.WriteFile(certPath, cert, 0o644); err != nil {
			return nil, err
		}
	}
	st.IncusClientCert, st.IncusClientKey, st.IncusCertGenerated = string(cert), string(key), true
	return st, nil
}

// IncusCertPath is where a generated Incus client certificate is kept, for
// `akbench-appliance incus-cert`.
func IncusCertPath(cfg *Config) string {
	return filepath.Join(cfg.StateDir, incusCertFile)
}

// CertFingerprint returns the SHA-256 fingerprint Incus lists a trusted
// certificate under (`incus config trust list`).
func CertFingerprint(certPEM string) (string, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return "", fmt.Errorf("no PEM block found")
	}
	sum := sha256.Sum256(block.Bytes)
	return hex.EncodeToString(sum[:]), nil
}

func readOrCreate(path string, gen func() ([]byte, error)) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err == nil {
		return b, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if b, err = gen(); err != nil {
		return nil, err
	}
	return b, os.WriteFile(path, b, 0o600)
}

// generateClientCert creates the self-signed TLS client certificate Incus
// expects for `incus config trust add-certificate`.
func generateClientCert() (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	hostname, _ := os.Hostname()
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "akbench-appliance", Organization: []string{"authentik-bench"}, OrganizationalUnit: []string{hostname}},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(incusCertValid),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), nil
}
