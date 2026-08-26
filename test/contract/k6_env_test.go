// Package contract guards the exact k6 tag/env contract the Ansible-based
// benchmark harness established (see /roles/runner/templates/compose.yml.j2
// and /roles/runner/files/tests/login.ts in the reference repo). Every
// Grafana dashboard this project ports splits on testid/target/profile, and
// login.ts reads a fixed set of env vars by name - a silent rename here would
// break dashboards and scripts without any Go compiler or envtest catching
// it, which is exactly what this golden-file comparison is for.
package contract

import (
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
	"github.com/goauthentik/authentik-bench/internal/render"
)

func exampleTestCase(profile benchv1alpha1.TestProfile) *benchv1alpha1.TestCase {
	return &benchv1alpha1.TestCase{
		ObjectMeta: metav1.ObjectMeta{Name: "login-baseline", Namespace: "default"},
		Spec: benchv1alpha1.TestCaseSpec{
			Script:          "login.ts",
			Profile:         profile,
			VUs:             8,
			Iterations:      1000,
			Flow:            "default-authentication-flow",
			QuickDuration:   "150s",
			BurnDuration:    "8760h",
			TraceSampleRate: "0.01",
			ExtraTags:       map[string]string{"suite": "smoke"},
		},
	}
}

func params(tc *benchv1alpha1.TestCase) render.K6WorkloadParams {
	return render.K6WorkloadParams{
		TestCase:            tc,
		TargetName:          "authentik-1",
		BenchHost:           "authentik-1.example.internal",
		BenchPort:           9000,
		RemoteWriteURL:      "http://prometheus.bench-system.svc.cluster.local/api/v1/write",
		ScriptConfigMapName: "k6-script-login-baseline",
		FixturesSecretName:  "bench-fixtures-users",
	}
}

// wantEnv is the exact env var contract login.ts reads, ported from
// roles/runner/templates/compose.yml.j2. Any rename here is a breaking
// change to the ability to run existing k6 scripts unmodified.
var wantEnvNames = []string{
	"TEST_ID",
	"TARGET",
	"PROFILE",
	"BENCH_HOST",
	"BENCH_PORT",
	"VUS",
	"ITERATIONS",
	"FLOW",
	"QUICK_DURATION",
	"BURN_DURATION",
	"TRACE_SAMPLE_RATE",
	"USERS_FILE",
	"K6_PROMETHEUS_RW_SERVER_URL",
	"K6_PROMETHEUS_RW_TREND_AS_NATIVE_HISTOGRAM",
	"K6_PROMETHEUS_RW_PUSH_INTERVAL",
}

func TestK6EnvVarNamesMatchAnsibleContract(t *testing.T) {
	p := params(exampleTestCase(benchv1alpha1.ProfileQuick))
	got := make([]string, 0, len(p.EnvVars()))
	for _, e := range p.EnvVars() {
		got = append(got, e.Name)
	}
	assertSameSet(t, "env var names", wantEnvNames, got)
}

func TestK6EnvVarValues(t *testing.T) {
	p := params(exampleTestCase(benchv1alpha1.ProfileBurn))
	values := envValueMap(p.EnvVars())

	want := map[string]string{
		"TEST_ID":           "login-baseline",
		"TARGET":            "authentik-1",
		"PROFILE":           "burn",
		"BENCH_HOST":        "authentik-1.example.internal",
		"BENCH_PORT":        "9000",
		"VUS":               "8",
		"ITERATIONS":        "1000",
		"FLOW":              "default-authentication-flow",
		"QUICK_DURATION":    "150s",
		"BURN_DURATION":     "8760h",
		"TRACE_SAMPLE_RATE": "0.01",
		"USERS_FILE":        "/data/fixtures/users.json",
	}
	for k, v := range want {
		if values[k] != v {
			t.Errorf("env %s = %q, want %q", k, values[k], v)
		}
	}
}

// TestK6TagScheme guards the --tag testid=/target=/profile= scheme every
// Grafana dashboard this project ports (compare-tests, k6, summary) splits
// on. Losing one of these tags silently breaks every dashboard's selector.
func TestK6TagScheme(t *testing.T) {
	for _, profile := range []benchv1alpha1.TestProfile{benchv1alpha1.ProfileQuick, benchv1alpha1.ProfileBurn} {
		p := params(exampleTestCase(profile))
		args := p.Args()
		wantProfile := "quick"
		if profile == benchv1alpha1.ProfileBurn {
			wantProfile = "burn"
		}
		for _, want := range []string{
			"testid=login-baseline",
			"target=authentik-1",
			"profile=" + wantProfile,
			"suite=smoke", // extraTags must reach the command line too
		} {
			if !slices.Contains(args, want) {
				t.Errorf("k6 args %v missing --tag %q", args, want)
			}
		}
		if args[0] != "run" || args[1] != "--out" || args[2] != "experimental-prometheus-rw" {
			t.Errorf("k6 args %v must start with `run --out experimental-prometheus-rw`", args)
		}
		last := args[len(args)-1]
		if last != "/data/tests/login.ts" {
			t.Errorf("k6 args end with %q, want the script path /data/tests/login.ts", last)
		}
	}
}

func envValueMap(vars []corev1.EnvVar) map[string]string {
	m := make(map[string]string, len(vars))
	for _, v := range vars {
		m[v.Name] = v.Value
	}
	return m
}

func assertSameSet(t *testing.T, label string, want, got []string) {
	t.Helper()
	w := append([]string(nil), want...)
	g := append([]string(nil), got...)
	slices.Sort(w)
	slices.Sort(g)
	if len(w) != len(g) {
		t.Fatalf("%s: got %v, want %v", label, g, w)
	}
	for i := range w {
		if w[i] != g[i] {
			t.Fatalf("%s: got %v, want %v", label, g, w)
		}
	}
}
