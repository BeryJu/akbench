package incus

import (
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

// trustToken is the only token fakeIncus accepts for POST /1.0/certificates.
const trustToken = "tok"

// writeSync/writeError answer in the Incus REST API's own envelopes, which
// the official client decodes.
func writeSync(w http.ResponseWriter, metadata any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "sync", "status": "Success", "status_code": 200, "metadata": metadata,
	})
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "error", "error": msg, "error_code": code})
}

// fakeIncus serves enough of the Incus API for these tests: /1.0 reports
// "trusted" only for a client certificate whose fingerprint has been added
// to its trust store, which POST /1.0/certificates with the right token
// does; instance state comes from states, keyed "<name>@<project>".
func fakeIncus(t *testing.T, states map[string]any) *httptest.Server {
	t.Helper()
	trusted := map[string]bool{}
	clientFingerprint := func(r *http.Request) string {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			return ""
		}
		return Fingerprint(string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: r.TLS.PeerCertificates[0].Raw})))
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/1.0":
			auth := "untrusted"
			if trusted[clientFingerprint(r)] {
				auth = "trusted"
			}
			writeSync(w, map[string]any{
				"api_version": "1.0", "auth": auth,
				"environment": map[string]any{"server_version": "7.5.1"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/1.0/certificates":
			var body struct {
				TrustToken string `json:"trust_token"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.TrustToken != trustToken {
				writeError(w, http.StatusForbidden, "Invalid trust token")
				return
			}
			trusted[clientFingerprint(r)] = true
			writeSync(w, nil)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/1.0/instances/") && strings.HasSuffix(r.URL.Path, "/state"):
			name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/1.0/instances/"), "/state")
			state, ok := states[name+"@"+r.URL.Query().Get("project")]
			if !ok {
				writeError(w, http.StatusNotFound, "Instance not found")
				return
			}
			writeSync(w, state)
		default:
			writeError(w, http.StatusNotFound, "not found")
		}
	}))
	srv.TLS = &tls.Config{ClientAuth: tls.RequestClientCert}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func newConnection(t *testing.T, server string) Connection {
	t.Helper()
	cert, key, err := GenerateClientCert()
	if err != nil {
		t.Fatal(err)
	}
	return Connection{Server: server, ClientCert: cert, ClientKey: key}
}

func serverCertPEM(srv *httptest.Server) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))
}

func TestProbeUnverifiedServerReturnsItsCertificate(t *testing.T) {
	srv := fakeIncus(t, nil)
	res := Probe(t.Context(), newConnection(t, srv.URL))
	if res.Reachable || res.ServerCert == "" || res.ServerCertFingerprint != Fingerprint(serverCertPEM(srv)) {
		t.Fatalf("unexpected result: %+v", res)
	}
	if !strings.Contains(res.Message, "could not be verified") {
		t.Errorf("message = %q", res.Message)
	}
}

func TestProbePinnedThenTrustWithToken(t *testing.T) {
	srv := fakeIncus(t, nil)
	c := newConnection(t, srv.URL)
	c.ServerCert = serverCertPEM(srv)

	res := Probe(t.Context(), c)
	if !res.Reachable || res.Trusted || res.ServerVersion != "7.5.1" || res.ServerCert != "" {
		t.Fatalf("before trust: %+v", res)
	}
	if err := TrustWithToken(t.Context(), c, "wrong"); err == nil || !strings.Contains(err.Error(), "Invalid trust token") {
		t.Fatalf("wrong token: err = %v", err)
	}
	if err := TrustWithToken(t.Context(), c, " tok\n"); err != nil {
		t.Fatal(err)
	}
	if res := Probe(t.Context(), c); !res.Trusted || res.Message != "" {
		t.Fatalf("after trust: %+v", res)
	}
}

func TestProbeRejectsMismatchedPin(t *testing.T) {
	srv := fakeIncus(t, nil)
	c := newConnection(t, srv.URL)
	// Every httptest server shares one built-in certificate, so pin an
	// unrelated one instead of another server's.
	c.ServerCert = newConnection(t, "").ClientCert
	res := Probe(t.Context(), c)
	if res.Reachable || !strings.Contains(res.Message, "could not be verified") || res.ServerCertFingerprint == "" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestProbeInsecureSkipVerify(t *testing.T) {
	srv := fakeIncus(t, nil)
	c := newConnection(t, srv.URL)
	c.InsecureSkipVerify = true
	if res := Probe(t.Context(), c); !res.Reachable || res.ServerCertFingerprint == "" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestProbeUnreachable(t *testing.T) {
	srv := fakeIncus(t, nil)
	url := srv.URL
	srv.Close()
	res := Probe(t.Context(), newConnection(t, url))
	if res.Reachable || res.ServerCert != "" || !strings.Contains(res.Message, "could not reach") {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestInstanceAddresses(t *testing.T) {
	srv := fakeIncus(t, map[string]any{
		"c1-abcde-lb@bench": map[string]any{"network": map[string]any{
			"lo":   map[string]any{"type": "loopback", "addresses": []any{map[string]any{"family": "inet", "address": "127.0.0.1", "netmask": "8", "scope": "local"}}},
			"cni0": map[string]any{"type": "broadcast", "addresses": []any{map[string]any{"family": "inet", "address": "10.244.0.1", "netmask": "24", "scope": "global"}}},
			"eth0": map[string]any{"type": "broadcast", "host_name": "veth1", "addresses": []any{
				map[string]any{"family": "inet6", "address": "fd42::5", "netmask": "64", "scope": "global"},
				map[string]any{"family": "inet6", "address": "fe80::5", "netmask": "64", "scope": "link"},
				map[string]any{"family": "inet", "address": "10.28.104.7", "netmask": "24", "scope": "global"},
				map[string]any{"family": "inet", "address": "10.28.104.200", "netmask": "32", "scope": "global"},
			}},
		}},
	})
	c := newConnection(t, srv.URL)
	c.Project = "bench"
	c.ServerCert = serverCertPEM(srv)
	v4, v6, err := InstanceAddresses(t.Context(), c, "c1-abcde-lb")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(v4, ",") != "10.28.104.7" || strings.Join(v6, ",") != "fd42::5" {
		t.Errorf("v4 %v v6 %v", v4, v6)
	}
	if _, _, err := InstanceAddresses(t.Context(), c, "missing"); err == nil {
		t.Error("expected an error for a missing instance")
	}
}

func TestConnectionRoundTripAndValidate(t *testing.T) {
	c := newConnection(t, "https://incus.example:8443")
	c.Project = "bench"
	c.InsecureSkipVerify = true
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	got := FromSecret(&corev1.Secret{Data: c.SecretData()})
	if got != c {
		t.Errorf("round trip: got %+v, want %+v", got, c)
	}
	if _, ok := (Connection{Server: "x"}).SecretData()[KeyServerCert]; ok {
		t.Error("unset server certificate encoded")
	}

	for name, bad := range map[string]Connection{
		"http URL":     {Server: "http://incus:8443", ClientCert: c.ClientCert, ClientKey: c.ClientKey},
		"no client":    {Server: "https://incus:8443"},
		"key mismatch": {Server: "https://incus:8443", ClientCert: c.ClientCert, ClientKey: newConnection(t, "").ClientKey},
		"bad pin":      {Server: "https://incus:8443", ClientCert: c.ClientCert, ClientKey: c.ClientKey, ServerCert: "nope"},
	} {
		if err := bad.Validate(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
