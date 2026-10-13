package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
)

func sqlDeployment(name string, mode benchv1alpha1.PostgresMode, cluster string) *benchv1alpha1.AuthentikDeployment {
	d := &benchv1alpha1.AuthentikDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec: benchv1alpha1.AuthentikDeploymentSpec{
			Namespace: name + "-ns",
		},
	}
	d.Spec.ClusterRef.Name = cluster
	d.Spec.Postgres.Mode = mode
	return d
}

// sqlServer wires a fake podExec that records its request and answers with
// the given result.
func sqlServer(t *testing.T, res podExecResult, execErr error, objects ...*benchv1alpha1.AuthentikDeployment) (*Server, *podExecRequest, *string) {
	t.Helper()
	objs := make([]client.Object, 0, len(objects))
	for _, o := range objects {
		objs = append(objs, o)
	}
	s, _ := newServer(t, Config{Shell: &ShellConfig{
		Kubeconfig: func(context.Context) ([]byte, error) { return []byte(testKubeconfig), nil },
		Shell:      "/bin/sh",
	}}, objs...)
	got := &podExecRequest{}
	stdin := new(string)
	s.podExec = func(_ context.Context, req podExecRequest) (podExecResult, error) {
		*got = req
		raw, _ := io.ReadAll(req.Stdin)
		*stdin = string(raw)
		return res, execErr
	}
	return s, got, stdin
}

func TestSQLEmbeddedRunsInStatefulSetPod(t *testing.T) {
	s, got, stdin := sqlServer(t, podExecResult{Pod: "d1-postgresql-0", Stdout: []byte(" ?column? \n----------\n        1\n")}, nil,
		sqlDeployment("d1", benchv1alpha1.PostgresEmbedded, "wc1"))

	rec := do(t, s, http.MethodPost, "/api/v1/deployments/d1/sql", map[string]string{"query": "SELECT 1"})
	requireStatus(t, rec, http.StatusOK)
	resp := decodeBody[sqlResponse](t, rec)

	if got.Context != "wc1-admin@wc1" || got.Namespace != "d1-ns" || got.Pod != "d1-postgresql-0" || got.Container != "postgresql" {
		t.Fatalf("exec target = %+v", got)
	}
	if *stdin != "SELECT 1" {
		t.Fatalf("stdin = %q, want the query", *stdin)
	}
	// The query must only ever reach psql through stdin.
	if strings.Contains(strings.Join(got.Command, " "), "SELECT") {
		t.Fatalf("query leaked into argv: %q", got.Command)
	}
	if resp.Target != "d1-ns/d1-postgresql-0" || !strings.Contains(resp.Output, "1") || resp.ExitCode != 0 {
		t.Fatalf("response = %+v", resp)
	}
}

func TestSQLCNPGSelectsPrimaryOnManagementCluster(t *testing.T) {
	s, got, _ := sqlServer(t, podExecResult{Pod: "d2-postgres-1"}, nil,
		sqlDeployment("d2", benchv1alpha1.PostgresCNPG, ""))

	rec := do(t, s, http.MethodPost, "/api/v1/deployments/d2/sql", map[string]string{"query": "SELECT 1", "format": "csv"})
	requireStatus(t, rec, http.StatusOK)
	if got.Context != ManagementClusterName || got.Pod != "" || got.Container != "postgres" ||
		got.Selector != "cnpg.io/cluster=d2-postgres,cnpg.io/instanceRole=primary" {
		t.Fatalf("exec target = %+v", got)
	}
	if got.Command[len(got.Command)-1] != "--csv" {
		t.Fatalf("command = %q, want --csv appended", got.Command)
	}
}

func TestSQLPassesPsqlFailureThrough(t *testing.T) {
	s, _, _ := sqlServer(t, podExecResult{Pod: "d1-postgresql-0", Stderr: []byte("ERROR:  syntax error"), ExitCode: 3}, nil,
		sqlDeployment("d1", benchv1alpha1.PostgresEmbedded, "wc1"))

	rec := do(t, s, http.MethodPost, "/api/v1/deployments/d1/sql", map[string]string{"query": "SELEC 1"})
	requireStatus(t, rec, http.StatusOK)
	if resp := decodeBody[sqlResponse](t, rec); resp.ExitCode != 3 || !strings.Contains(resp.Stderr, "syntax error") {
		t.Fatalf("response = %+v", resp)
	}
}

func TestSQLRejects(t *testing.T) {
	cases := []struct {
		name    string
		deploy  *benchv1alpha1.AuthentikDeployment
		target  string
		body    map[string]string
		execErr error
		want    int
	}{
		{"empty query", sqlDeployment("d1", benchv1alpha1.PostgresEmbedded, "wc1"), "d1", map[string]string{"query": "  "}, nil, http.StatusBadRequest},
		{"unknown format", sqlDeployment("d1", benchv1alpha1.PostgresEmbedded, "wc1"), "d1", map[string]string{"query": "SELECT 1", "format": "html"}, nil, http.StatusBadRequest},
		{"external postgres", sqlDeployment("d1", benchv1alpha1.PostgresExternal, "wc1"), "d1", map[string]string{"query": "SELECT 1"}, nil, http.StatusBadRequest},
		{"missing deployment", sqlDeployment("d1", benchv1alpha1.PostgresEmbedded, "wc1"), "nope", map[string]string{"query": "SELECT 1"}, nil, http.StatusNotFound},
		{"unknown cluster", sqlDeployment("d1", benchv1alpha1.PostgresEmbedded, "wc9"), "d1", map[string]string{"query": "SELECT 1"}, nil, http.StatusConflict},
		{"exec failure", sqlDeployment("d1", benchv1alpha1.PostgresEmbedded, "wc1"), "d1", map[string]string{"query": "SELECT 1"}, errors.New("pod gone"), http.StatusBadGateway},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _ := sqlServer(t, podExecResult{}, tc.execErr, tc.deploy)
			rec := do(t, s, http.MethodPost, "/api/v1/deployments/"+tc.target+"/sql", tc.body)
			requireStatus(t, rec, tc.want)
		})
	}
}

func TestSQLNeedsShellKubeconfig(t *testing.T) {
	s, _ := newServer(t, Config{}, sqlDeployment("d1", benchv1alpha1.PostgresEmbedded, "wc1"))
	rec := do(t, s, http.MethodPost, "/api/v1/deployments/d1/sql", map[string]string{"query": "SELECT 1"})
	requireStatus(t, rec, http.StatusNotFound)
}

func TestCappedBufferTruncates(t *testing.T) {
	b := &cappedBuffer{limit: 4}
	_, _ = b.Write([]byte("abc"))
	_, _ = b.Write([]byte("defg"))
	if got := string(b.Bytes()); got != "abcd\n[output truncated]\n" {
		t.Fatalf("Bytes() = %q", got)
	}
}
