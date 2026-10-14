package main

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTLSConfigSelfSigned(t *testing.T) {
	cfg, selfSigned, err := tlsConfig("", "")
	if err != nil {
		t.Fatal(err)
	}
	if !selfSigned {
		t.Fatal("want a self-signed certificate without cert/key files")
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	srv.TLS = cfg
	srv.StartTLS()
	defer srv.Close()

	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed by design
	}}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if leaf := resp.TLS.PeerCertificates[0]; leaf.DNSNames[0] != "localhost" {
		t.Errorf("DNSNames = %v, want localhost first", leaf.DNSNames)
	}
}

func TestTLSConfigNeedsBothFiles(t *testing.T) {
	if _, _, err := tlsConfig("cert.pem", ""); err == nil {
		t.Error("want an error for a cert file without a key file")
	}
}
