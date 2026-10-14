package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"os"
	"time"
)

// tlsConfig loads the serving certificate from certFile/keyFile, or, with
// neither set, generates a self-signed one in memory. The self-signed
// certificate is regenerated on every start, so browsers show a warning each
// time; mount a real certificate for anything long-lived.
func tlsConfig(certFile, keyFile string) (*tls.Config, bool, error) {
	var (
		cert       tls.Certificate
		err        error
		selfSigned bool
	)
	switch {
	case certFile != "" && keyFile != "":
		cert, err = tls.LoadX509KeyPair(certFile, keyFile)
	case certFile != "" || keyFile != "":
		return nil, false, errors.New("set both --tls-cert-file and --tls-key-file, or neither")
	default:
		cert, err = selfSignedCert()
		selfSigned = true
	}
	if err != nil {
		return nil, false, fmt.Errorf("load TLS certificate: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, selfSigned, nil
}

// selfSignedCert generates a one-year ECDSA certificate valid for localhost
// and this host's name.
func selfSignedCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}
	names := []string{"localhost"}
	if hostname, err := os.Hostname(); err == nil && hostname != "" && hostname != "localhost" {
		names = append(names, hostname)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "authentik-bench", Organization: []string{"authentik-bench"}},
		DNSNames:              names,
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(1, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}
