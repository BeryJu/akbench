package appliance

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
)

func testEnv(extra map[string]string) func(string) string {
	env := map[string]string{
		"AKBENCH_CAPI_VERSION": "v1.14.3",
		"AKBENCH_CAPN_VERSION": "v0.9.1",
	}
	maps.Copy(env, extra)
	return func(k string) string { return env[k] }
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := LoadConfig(testEnv(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Namespace != "default" || cfg.UIPort != "8080" || cfg.Incus.SecretName != "lxc-secret" || cfg.Incus.Project != "default" {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadConfigErrors(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"cert without key": {"INCUS_CLIENT_CERT": "-----BEGIN CERTIFICATE-----\n"},
		"bad port":         {"AKBENCH_UI_PORT": "http"},
		"missing file":     {"INCUS_SERVER_CERT": "/does/not/exist"},
	} {
		if _, err := LoadConfig(testEnv(env)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if _, err := LoadConfig(func(string) string { return "" }); err == nil {
		t.Error("expected an error without pinned provider versions")
	}
}

func TestLoadConfigPEMFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.crt")
	if err := os.WriteFile(path, []byte("-----BEGIN CERTIFICATE-----\nx\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(testEnv(map[string]string{"INCUS_SERVER_CERT": path}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cfg.Incus.ServerCert, "-----BEGIN CERTIFICATE-----") {
		t.Errorf("server cert not read from file: %q", cfg.Incus.ServerCert)
	}
}

func TestEnsureStateIsStable(t *testing.T) {
	cfg, err := LoadConfig(testEnv(map[string]string{"AKBENCH_STATE_DIR": t.TempDir()}))
	if err != nil {
		t.Fatal(err)
	}
	first, err := EnsureState(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.APIToken) != 64 || !first.IncusCertGenerated {
		t.Fatalf("unexpected first state: token %q generated %v", first.APIToken, first.IncusCertGenerated)
	}
	if _, err := CertFingerprint(first.IncusClientCert); err != nil {
		t.Errorf("generated certificate: %v", err)
	}
	second, err := EnsureState(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Error("state changed between boots")
	}
}

func TestEnsureStateUsesSuppliedCredentials(t *testing.T) {
	cfg, err := LoadConfig(testEnv(map[string]string{
		"AKBENCH_STATE_DIR": t.TempDir(),
		"AKBENCH_API_TOKEN": "fixed",
		"INCUS_CLIENT_CERT": "-----BEGIN CERTIFICATE-----\nc\n",
		"INCUS_CLIENT_KEY":  "-----BEGIN EC PRIVATE KEY-----\nk\n",
	}))
	if err != nil {
		t.Fatal(err)
	}
	st, err := EnsureState(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if st.APIToken != "fixed" || st.IncusCertGenerated || !strings.Contains(st.IncusClientKey, "k") {
		t.Errorf("supplied credentials not used: %+v", st)
	}
}

func renderDocs(t *testing.T, cfg *Config, st *State) map[string]*unstructured.Unstructured {
	t.Helper()
	out, err := RenderStack(cfg, st)
	if err != nil {
		t.Fatal(err)
	}
	docs := map[string]*unstructured.Unstructured{}
	dec := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(out), 4096)
	for {
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("decode rendered stack: %v\n%s", err, out)
		}
		if obj == nil {
			continue
		}
		u := &unstructured.Unstructured{Object: obj}
		docs[u.GetKind()+"/"+u.GetName()] = u
	}
	return docs
}

func TestRenderStack(t *testing.T) {
	cfg, err := LoadConfig(testEnv(map[string]string{
		"AKBENCH_VERSION":     "1.2.3",
		"AKBENCH_NAMESPACE":   "bench",
		"AKBENCH_UI_PORT":     "9000",
		"AKBENCH_OIDC_ISSUER": "https://id.example.com/",
		"INCUS_SERVER":        "https://incus:8443",
		"INCUS_SERVER_CERT":   "-----BEGIN CERTIFICATE-----\n\"quoted\": yes\n",
	}))
	if err != nil {
		t.Fatal(err)
	}
	st := &State{APIToken: "tok", IncusClientCert: "-----BEGIN CERTIFICATE-----\nc\n", IncusClientKey: "-----BEGIN EC PRIVATE KEY-----\nk\n"}
	docs := renderDocs(t, cfg, st)

	secret := docs["Secret/lxc-secret"]
	if secret == nil || secret.GetNamespace() != "bench" {
		t.Fatalf("lxc-secret missing or in wrong namespace: %v", secret)
	}
	if v, _, _ := unstructured.NestedString(secret.Object, "stringData", "server-crt"); v != cfg.Incus.ServerCert {
		t.Errorf("server-crt = %q, want %q", v, cfg.Incus.ServerCert)
	}
	if v, _, _ := unstructured.NestedString(secret.Object, "stringData", "client-key"); v != st.IncusClientKey {
		t.Errorf("client-key = %q", v)
	}

	if tag, _, _ := unstructured.NestedString(docs["OCIRepository/authentik-bench-operator"].Object, "spec", "ref", "tag"); tag != "1.2.3" {
		t.Errorf("chart tag = %q, want 1.2.3", tag)
	}
	if tag, _, _ := unstructured.NestedString(docs["HelmRelease/authentik-bench-operator"].Object, "spec", "values", "controllerManager", "manager", "image", "tag"); tag != "1.2.3" {
		t.Errorf("operator image tag = %q, want 1.2.3", tag)
	}

	containers, _, _ := unstructured.NestedSlice(docs["Deployment/apiserver"].Object, "spec", "template", "spec", "containers")
	c := containers[0].(map[string]any)
	if c["image"] != "ghcr.io/beryju/akbench-apiserver:1.2.3" {
		t.Errorf("apiserver image = %v", c["image"])
	}
	env := map[string]any{}
	for _, e := range c["env"].([]any) {
		m := e.(map[string]any)
		env[m["name"].(string)] = m["value"]
	}
	if env["BENCH_NAMESPACE"] != "bench" || env["BENCH_OIDC_ISSUER"] != "https://id.example.com/" {
		t.Errorf("apiserver env = %v", env)
	}
	if _, ok := env["BENCH_OIDC_CLIENT_ID"]; ok {
		t.Error("unset OIDC client ID rendered")
	}

	ports, _, _ := unstructured.NestedSlice(docs["Service/apiserver"].Object, "spec", "ports")
	if fmt.Sprint(ports[0].(map[string]any)["port"]) != "9000" {
		t.Errorf("service port = %v, want 9000", ports[0])
	}
}

func TestRenderStackWithoutIncusServer(t *testing.T) {
	cfg, err := LoadConfig(testEnv(nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := renderDocs(t, cfg, &State{APIToken: "tok"})["Secret/lxc-secret"]; ok {
		t.Error("lxc-secret rendered without INCUS_SERVER - would clobber a migrated Secret")
	}
}

func TestPlanClusterctl(t *testing.T) {
	cfg, _ := LoadConfig(testEnv(nil))
	pinned := PinnedProviders(cfg)

	cmds, skipped, err := PlanClusterctl(nil, pinned)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"init", "--wait-providers",
		"--core", "cluster-api:v1.14.3", "--bootstrap", "kubeadm:v1.14.3",
		"--control-plane", "kubeadm:v1.14.3", "--infrastructure", "incus:v0.9.1"}}
	if !reflect.DeepEqual(cmds, want) || len(skipped) != 0 {
		t.Errorf("fresh install: cmds %v skipped %v", cmds, skipped)
	}

	installed := []Provider{
		{Type: "CoreProvider", Name: "cluster-api", Version: "v1.13.0"},
		{Type: "BootstrapProvider", Name: "kubeadm", Version: "v1.14.3"},
		{Type: "ControlPlaneProvider", Name: "kubeadm", Version: "v1.15.0"},
	}
	cmds, skipped, err = PlanClusterctl(installed, pinned)
	if err != nil {
		t.Fatal(err)
	}
	want = [][]string{
		{"init", "--wait-providers", "--infrastructure", "incus:v0.9.1"},
		{"upgrade", "apply", "--wait-providers", "--core", "cluster-api:v1.14.3"},
	}
	if !reflect.DeepEqual(cmds, want) {
		t.Errorf("mixed: cmds %v", cmds)
	}
	if len(skipped) != 1 || !strings.Contains(skipped[0], "v1.15.0") {
		t.Errorf("mixed: skipped %v", skipped)
	}

	cmds, _, err = PlanClusterctl(append(installed[:2:2],
		Provider{Type: "ControlPlaneProvider", Name: "kubeadm", Version: "v1.14.3"},
		Provider{Type: "InfrastructureProvider", Name: "incus", Version: "v0.9.1"}), pinned)
	if err != nil || len(cmds) != 1 || cmds[0][0] != "upgrade" {
		t.Errorf("only core outdated: cmds %v err %v", cmds, err)
	}
}

func TestParseInventory(t *testing.T) {
	got, err := ParseInventory([]byte(`{"items":[{"metadata":{"namespace":"capn-system"},"type":"InfrastructureProvider","providerName":"incus","version":"v0.9.1"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []Provider{{Type: "InfrastructureProvider", Name: "incus", Version: "v0.9.1"}}) {
		t.Errorf("got %v", got)
	}
}

func TestBootstrapRunOnceOrder(t *testing.T) {
	dir := t.TempDir()
	kubeconfig := filepath.Join(dir, "k3s.yaml")
	if err := os.WriteFile(kubeconfig, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _ := LoadConfig(testEnv(map[string]string{"AKBENCH_KUBECONFIG": kubeconfig, "AKBENCH_ASSETS_DIR": "/assets"}))
	var calls []string
	var applied []byte
	run := func(_ context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if stdin != nil {
			applied = stdin
		}
		return nil, nil
	}
	b := &Bootstrapper{Config: cfg, State: &State{APIToken: "tok"}, Run: run, Log: io.Discard}
	if err := b.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, c := range calls {
		switch {
		case strings.Contains(c, "/assets/flux/install.yaml"):
			order = append(order, "flux")
		case strings.HasPrefix(c, "clusterctl init"):
			order = append(order, "clusterctl")
		case strings.HasSuffix(c, "-f -"):
			order = append(order, "stack")
		}
	}
	if !reflect.DeepEqual(order, []string{"flux", "clusterctl", "stack"}) {
		t.Errorf("order = %v (calls %v)", order, calls)
	}
	if !bytes.Contains(applied, []byte("kind: HelmRelease")) {
		t.Error("stack not passed on stdin")
	}
}
