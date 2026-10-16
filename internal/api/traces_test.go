package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"

	benchv1alpha1 "github.com/authentik-labs/akbench/api/v1alpha1"
	"github.com/authentik-labs/akbench/internal/render"
)

// A Tempo search response shaped like authentik's real traces: per trace,
// two server spans (1s and 3s), one stage span, and three queries.
const fakeTempoSearch = `{"traces":[{"spanSets":[{"spans":[
 {"durationNanos":"1000000000","attributes":[{"key":"kind","value":{"stringValue":"server"}}]},
 {"durationNanos":"3000000000","attributes":[{"key":"kind","value":{"stringValue":"server"}}]},
 {"durationNanos":"500000000","attributes":[{"key":"kind","value":{"stringValue":"internal"}},{"key":"name","value":{"stringValue":"authentik.stages.password.stage.PasswordStageView"}}]},
 {"durationNanos":"200000000","attributes":[{"key":"kind","value":{"stringValue":"client"}},{"key":"db.statement","value":{"stringValue":"SELECT 1"}}]},
 {"durationNanos":"200000000","attributes":[{"key":"kind","value":{"stringValue":"client"}},{"key":"db.statement","value":{"stringValue":"SELECT 1"}}]},
 {"durationNanos":"600000000","attributes":[{"key":"kind","value":{"stringValue":"client"}},{"key":"db.statement","value":{"stringValue":"UPDATE session"}}]}
]}]}]}`

func approx(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

func TestTraceBreakdown(t *testing.T) {
	var searches atomic.Int32
	var query string
	grafana := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/grafana-proxy/bench/api/datasources/proxy/uid/tempo/api/search" {
			http.NotFound(w, r)
			return
		}
		if user, pass, ok := r.BasicAuth(); !ok || user != render.GrafanaAdminUser || pass != grafanaTestAdminPassword {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		searches.Add(1)
		query = r.URL.Query().Get("q")
		_, _ = fmt.Fprint(w, fakeTempoSearch)
	}))
	t.Cleanup(grafana.Close)

	wc := &benchv1alpha1.WorkloadCluster{Name: "bench", Namespace: testNamespace}
	wc.Status.GrafanaURL = grafana.URL
	adminSecret := &corev1.Secret{
		Name: render.GrafanaAdminSecretName(wc), Namespace: testNamespace,
		Data: map[string][]byte{"password": []byte(grafanaTestAdminPassword)},
	}
	onCluster := &benchv1alpha1.AuthentikDeployment{Name: "ak-a", Namespace: testNamespace}
	onCluster.Spec.ClusterRef = corev1.LocalObjectReference{Name: "bench"}
	onCluster.Spec.TargetName = "a"
	onManagement := &benchv1alpha1.AuthentikDeployment{Name: "ak-b", Namespace: testNamespace}
	tc := &benchv1alpha1.TestCase{Name: "login", Namespace: testNamespace}
	tc.Spec.DeploymentRefs = []corev1.LocalObjectReference{{Name: "ak-a"}, {Name: "ak-b"}}
	s, _ := newServer(t, Config{}, wc, adminSecret, onCluster, onManagement, tc)

	rec := do(t, s, http.MethodGet, "/api/v1/traces/breakdown?testid=login&window=1h", nil)
	requireStatus(t, rec, http.StatusOK)
	got := decodeBody[TraceBreakdown](t, rec)
	if len(got.Targets) != 2 {
		t.Fatalf("targets = %d, want 2", len(got.Targets))
	}
	if !strings.Contains(query, `x_benchmark_test = "login"`) || !strings.Contains(query, `x_benchmark_target = "a"`) {
		t.Errorf("query not scoped to the test and target: %s", query)
	}

	a := got.Targets[0]
	if a.Error != "" || a.Target != "a" || a.Traces != 1 || a.Requests != 2 {
		t.Fatalf("target a: %+v", a)
	}
	approx(t, "requestP50", a.RequestP50, 1)
	approx(t, "requestP95", a.RequestP95, 3)
	approx(t, "queriesPerRequest", a.QueriesPerRequest, 1.5)
	approx(t, "dbTimePerRequest", a.DBTimePerRequest, 0.5)
	approx(t, "dbShare", a.DBShare, 0.25)
	if len(a.Stages) != 1 || a.Stages[0].Name != "PasswordStageView" {
		t.Errorf("stages: %+v", a.Stages)
	}
	if len(a.TopQueries) != 2 || a.TopQueries[0].Statement != "UPDATE session" || a.TopQueries[1].Count != 2 {
		t.Errorf("top queries not ranked by total time: %+v", a.TopQueries)
	}

	if b := got.Targets[1]; b.Target != "ak-b" || b.Error == "" {
		t.Errorf("a management-cluster target should report why it has no traces: %+v", b)
	}

	// A second request inside the TTL is served from the cache.
	requireStatus(t, do(t, s, http.MethodGet, "/api/v1/traces/breakdown?testid=login&window=1h", nil), http.StatusOK)
	if n := searches.Load(); n != 1 {
		t.Errorf("Tempo searched %d times, want 1 (cached)", n)
	}
}

// traceFixture is one TestCase whose single target's traces live behind
// grafana.
func traceFixture(t *testing.T, grafana http.Handler) *Server {
	t.Helper()
	srv := httptest.NewServer(grafana)
	t.Cleanup(srv.Close)
	wc := &benchv1alpha1.WorkloadCluster{Name: "bench", Namespace: testNamespace}
	wc.Status.GrafanaURL = srv.URL
	adminSecret := &corev1.Secret{
		Name: render.GrafanaAdminSecretName(wc), Namespace: testNamespace,
		Data: map[string][]byte{"password": []byte(grafanaTestAdminPassword)},
	}
	d := &benchv1alpha1.AuthentikDeployment{Name: "ak-a", Namespace: testNamespace}
	d.Spec.ClusterRef = corev1.LocalObjectReference{Name: "bench"}
	tc := &benchv1alpha1.TestCase{Name: "login", Namespace: testNamespace}
	tc.Spec.DeploymentRefs = []corev1.LocalObjectReference{{Name: "ak-a"}}
	s, _ := newServer(t, Config{}, wc, adminSecret, d, tc)
	return s
}

func TestTraceBreakdownSurvivesCancelledRequest(t *testing.T) {
	var searches atomic.Int32
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	s := traceFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		searches.Add(1)
		started <- struct{}{}
		<-release
		_, _ = fmt.Fprint(w, fakeTempoSearch)
	}))

	// The browser goes away while Tempo is still searching...
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/traces/breakdown?testid=login&window=1h", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+testToken)
	done := make(chan struct{})
	go func() {
		s.Router().ServeHTTP(httptest.NewRecorder(), req)
		close(done)
	}()
	<-started
	cancel()
	<-done

	// ...the search still completes, and the next request gets its result
	// rather than a cached "context canceled".
	close(release)
	rec := do(t, s, http.MethodGet, "/api/v1/traces/breakdown?testid=login&window=1h", nil)
	requireStatus(t, rec, http.StatusOK)
	got := decodeBody[TraceBreakdown](t, rec)
	if len(got.Targets) != 1 || got.Targets[0].Error != "" || got.Targets[0].Requests != 2 {
		t.Fatalf("expected the completed search's result, got %+v", got.Targets)
	}
	if n := searches.Load(); n != 1 {
		t.Errorf("Tempo searched %d times, want 1 (shared with the cancelled request)", n)
	}
}

func TestTraceBreakdownDoesNotCacheFailures(t *testing.T) {
	var searches atomic.Int32
	s := traceFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if searches.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = fmt.Fprint(w, fakeTempoSearch)
	}))

	rec := do(t, s, http.MethodGet, "/api/v1/traces/breakdown?testid=login&window=1h", nil)
	requireStatus(t, rec, http.StatusOK)
	if got := decodeBody[TraceBreakdown](t, rec); got.Targets[0].Error == "" {
		t.Fatalf("expected the first search's error, got %+v", got.Targets[0])
	}
	rec = do(t, s, http.MethodGet, "/api/v1/traces/breakdown?testid=login&window=1h", nil)
	if got := decodeBody[TraceBreakdown](t, rec); got.Targets[0].Error != "" {
		t.Errorf("a failed search must not be cached: %+v", got.Targets[0])
	}
}

func TestTraceBreakdownValidatesWindow(t *testing.T) {
	s, _ := newServer(t, Config{})
	requireStatus(t, do(t, s, http.MethodGet, "/api/v1/traces/breakdown?testid=x&window=7d", nil), http.StatusBadRequest)
	requireStatus(t, do(t, s, http.MethodGet, "/api/v1/traces/breakdown", nil), http.StatusBadRequest)
}

// TestTraceBreakdownRejectsUnsafeTargetName is a regression test for a
// TraceQL-injection risk: spec.targetName is a free-form field (unlike the
// object name, which the apiserver already restricts to a DNS subdomain),
// and traceBreakdownQuery interpolates it directly into a TraceQL search.
// A value with TraceQL metacharacters must never reach Tempo's query API at
// all - it should surface as a per-target error instead.
func TestTraceBreakdownRejectsUnsafeTargetName(t *testing.T) {
	var searched atomic.Bool
	grafana := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		searched.Store(true)
		_, _ = fmt.Fprint(w, fakeTempoSearch)
	}))
	t.Cleanup(grafana.Close)

	wc := &benchv1alpha1.WorkloadCluster{Name: "bench", Namespace: testNamespace}
	wc.Status.GrafanaURL = grafana.URL
	malicious := &benchv1alpha1.AuthentikDeployment{Name: "ak-evil", Namespace: testNamespace}
	malicious.Spec.ClusterRef = corev1.LocalObjectReference{Name: "bench"}
	malicious.Spec.TargetName = `a" } || { true } //`
	tc := &benchv1alpha1.TestCase{Name: "login", Namespace: testNamespace}
	tc.Spec.DeploymentRefs = []corev1.LocalObjectReference{{Name: "ak-evil"}}
	s, _ := newServer(t, Config{}, wc, malicious, tc)

	rec := do(t, s, http.MethodGet, "/api/v1/traces/breakdown?testid=login&window=1h", nil)
	requireStatus(t, rec, http.StatusOK)
	got := decodeBody[TraceBreakdown](t, rec)
	if len(got.Targets) != 1 || got.Targets[0].Error == "" {
		t.Fatalf("expected one target with an error, got: %+v", got.Targets)
	}
	if searched.Load() {
		t.Error("Tempo was queried with an unvalidated target name")
	}
}

// fakeSpan is one span of a Tempo search response, its attributes given as
// key, value pairs.
func fakeSpan(startMillis, durationMillis int64, attrs ...string) string {
	var kv []string
	for i := 0; i+1 < len(attrs); i += 2 {
		kv = append(kv, fmt.Sprintf(`{"key":%q,"value":{"stringValue":%q}}`, attrs[i], attrs[i+1]))
	}
	return fmt.Sprintf(`{"startTimeUnixNano":"%d","durationNanos":"%d","attributes":[%s]}`,
		startMillis*1_000_000, durationMillis*1_000_000, strings.Join(kv, ","))
}

func fakeQuery(startMillis, durationMillis int64, statement string) string {
	return fakeSpan(startMillis, durationMillis, "kind", "client", "db.statement", statement)
}

// fakeLoginSearch is one trace of two requests, [0s, 1s] and [2s, 3s]. The
// cache lookup runs three times in the first, under two different
// sqlcommenter comments and traceparents, and once in the second; an IN list
// runs once in each, at different lengths.
var fakeLoginSearch = `{"traces":[{"spanSets":[{"spans":[` + strings.Join([]string{
	fakeSpan(0, 1000, "kind", "server"),
	fakeSpan(2000, 1000, "kind", "server"),
	fakeQuery(50, 1, `SELECT set_config('TimeZone', %s, false)`),
	fakeQuery(100, 30, cacheLookup+` /*app_name='authentik_api',controller='authentik_api%%3Aflow-executor',db_driver='psqlextra.backend',framework='django%%3A5.2.17',route='api/v3/flows/executor/%%3Cslug%%3Aflow_slug%%3E/',traceparent='00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01'*/`),
	fakeQuery(200, 10, cacheLookup+` /*app_name='authentik_api',controller='authentik_api%%3Aflow-executor',db_driver='psqlextra.backend',framework='django%%3A5.2.17',route='api/v3/flows/executor/%%3Cslug%%3Aflow_slug%%3E/',traceparent='00-4bf92f3577b34da6a3ce929d0e0e4736-b7ad6b7169203331-01'*/`),
	fakeQuery(300, 20, cacheLookup+` /*db_driver='psqlextra.backend',framework='django%%3A5.2.17',traceparent='00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01'*/`),
	fakeQuery(400, 5, `SELECT "u"."id" FROM "u" WHERE "u"."id" IN (%s, %s)`),
	fakeQuery(2100, 40, cacheLookup+` /*traceparent='00-4bf92f3577b34da6a3ce929d0e0e4736-0123456789abcdef-01'*/`),
	fakeQuery(2200, 5, `SELECT "u"."id" FROM "u" WHERE "u"."id" IN (%s, %s, %s)`),
}, ",") + `]}]}]}`

func TestSummarizeTraceSpansPerStatement(t *testing.T) {
	var body tempoSearchResponse
	if err := json.Unmarshal([]byte(fakeLoginSearch), &body); err != nil {
		t.Fatal(err)
	}
	got := summarizeTraceSpans("a", &body)
	if got.Requests != 2 || len(got.TopQueries) != 3 {
		t.Fatalf("requests = %d, top queries = %+v; want 2 requests and the cache lookup, the IN list and set_config", got.Requests, got.TopQueries)
	}
	approx(t, "queryFloor", got.QueryFloor, 0.001)

	cache := got.TopQueries[0]
	if !strings.HasPrefix(cache.Statement, `SELECT … FROM "django_postgres_cache_cacheentry" WHERE`) || strings.Contains(cache.Statement, "/*") {
		t.Errorf("cache lookup summary = %q, want its column list and comment gone", cache.Statement)
	}
	if cache.ID == "" || cache.Count != 4 || cache.MaxPerRequest != 3 {
		t.Errorf("cache lookup = %+v, want its 4 calls in one row, 3 of them in one request", cache)
	}
	approx(t, "perRequest", cache.PerRequest, 2)
	approx(t, "total", cache.Total, 0.1)
	approx(t, "timePerRequest", cache.TimePerRequest, 0.05)
	approx(t, "share", cache.Share, 0.05)
	approx(t, "mean", cache.Mean, 0.025)
	approx(t, "p50", cache.P50, 0.02)
	approx(t, "p95", cache.P95, 0.04)
	approx(t, "max", cache.Max, 0.04)
	approx(t, "overFloor", cache.OverFloor, 0.019)

	in := got.TopQueries[1]
	if in.Statement != `SELECT "u"."id" FROM "u" WHERE "u"."id" IN (%s, …)` || in.Count != 2 || in.MaxPerRequest != 1 {
		t.Errorf("IN list = %+v, want both lengths in one row, once per request", in)
	}
}

// postgresTraceFixture is TestCase "login" driving deployment ak-a on
// workload cluster wc1, with Tempo answering fakeLoginSearch and pod exec
// answering with exec.
func postgresTraceFixture(t *testing.T, exec podExecFunc) *Server {
	t.Helper()
	grafana := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, fakeLoginSearch)
	}))
	t.Cleanup(grafana.Close)
	wc := &benchv1alpha1.WorkloadCluster{Name: "wc1", Namespace: testNamespace}
	wc.Status.GrafanaURL = grafana.URL
	adminSecret := &corev1.Secret{
		Name: render.GrafanaAdminSecretName(wc), Namespace: testNamespace,
		Data: map[string][]byte{"password": []byte(grafanaTestAdminPassword)},
	}
	d := sqlDeployment("ak-a", benchv1alpha1.PostgresEmbedded, "wc1")
	tc := &benchv1alpha1.TestCase{Name: "login", Namespace: testNamespace}
	tc.Spec.DeploymentRefs = []corev1.LocalObjectReference{{Name: "ak-a"}}
	s, _ := newServer(t, Config{Shell: &ShellConfig{
		Kubeconfig: func(context.Context) ([]byte, error) { return []byte(testKubeconfig), nil },
		Shell:      "/bin/sh",
	}}, wc, adminSecret, d, tc)
	s.podExec = exec
	return s
}

func TestTraceBreakdownMatchesPostgresStatements(t *testing.T) {
	recorded := strings.NewReplacer(`"`, `""`, "= %s", "= $1", ">= %s", ">= $2::timestamptz", "LIMIT 1", "LIMIT $3").Replace(cacheLookup)
	csvOut := "query,calls,total_exec_time,rows,shared_blks_hit,shared_blks_read,?column?,plans,total_plan_time,extract\n" +
		`"` + recorded + ` /*app_name='authentik_api',traceparent='00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01'*/",1000,50,1000,3000,0,0,1000,20,1792234567.500000` + "\n" +
		`"SELECT ""u"".""id"" FROM ""u""` + "\n" + `WHERE ""u"".""id"" IN ($1, $2)",10,1,20,40,10,0,0,0,1792234567.500000` + "\n" +
		`"SELECT ""u"".""id"" FROM ""u"" WHERE ""u"".""id"" IN ($1, $2, $3)",30,3,90,110,40,8,0,0,1792234567.500000` + "\n"
	var got podExecRequest
	var stdin string
	s := postgresTraceFixture(t, func(_ context.Context, req podExecRequest) (podExecResult, error) {
		got = req
		raw, _ := io.ReadAll(req.Stdin)
		stdin = string(raw)
		return podExecResult{Pod: "ak-a-postgresql-0", Stdout: []byte(csvOut)}, nil
	})

	rec := do(t, s, http.MethodGet, "/api/v1/traces/breakdown?testid=login&window=1h", nil)
	requireStatus(t, rec, http.StatusOK)
	a := decodeBody[TraceBreakdown](t, rec).Targets[0]

	if got.Context != "wc1-admin@wc1" || got.Pod != "ak-a-postgresql-0" || got.Command[len(got.Command)-1] != "--csv" ||
		got.StdoutLimit != pgStatStatementsOutputLimit || stdin != pgStatStatementsQuery {
		t.Errorf("exec = %+v with stdin %q, want pgStatStatementsQuery as CSV in the deployment's Postgres", got, stdin)
	}
	if a.Postgres == nil || a.Postgres.Error != "" || a.Postgres.Since == nil || !a.Postgres.Since.Equal(time.Unix(1792234567, 500_000_000)) {
		t.Fatalf("postgres = %+v, want the stats reset time", a.Postgres)
	}
	cache, in, setConfig := a.TopQueries[0].Postgres, a.TopQueries[1].Postgres, a.TopQueries[2].Postgres
	if cache == nil || cache.Calls != 1000 {
		t.Fatalf("cache lookup's postgres = %+v, want its pg_stat_statements entry", cache)
	}
	approx(t, "cache meanExec", cache.MeanExec, 0.00005)
	approx(t, "cache meanPlan", cache.MeanPlan, 0.00002)
	if cache.Plans != 1000 {
		t.Errorf("cache plans = %d, want 1000", cache.Plans)
	}
	approx(t, "cache rowsPerCall", cache.RowsPerCall, 1)
	approx(t, "cache blocksPerCall", cache.BlocksPerCall, 3)
	approx(t, "cache readShare", cache.ReadShare, 0)
	if in == nil || in.Calls != 40 {
		t.Fatalf("IN list's postgres = %+v, want both its entries summed", in)
	}
	approx(t, "IN meanExec", in.MeanExec, 0.0001)
	approx(t, "IN rowsPerCall", in.RowsPerCall, 2.75)
	approx(t, "IN blocksPerCall", in.BlocksPerCall, 5)
	approx(t, "IN readShare", in.ReadShare, 0.25)
	approx(t, "IN tempBlocksPerCall", in.TempBlocksPerCall, 0.2)
	if setConfig != nil {
		t.Errorf("set_config's postgres = %+v, want none: pg_stat_statements has no entry for it", setConfig)
	}
}

func TestTraceBreakdownExplainsMissingPostgresStatements(t *testing.T) {
	s := postgresTraceFixture(t, func(context.Context, podExecRequest) (podExecResult, error) {
		return podExecResult{ExitCode: 3, Stderr: []byte(`psql:<stdin>:7: ERROR:  pg_stat_statements must be loaded via "shared_preload_libraries"`)}, nil
	})
	rec := do(t, s, http.MethodGet, "/api/v1/traces/breakdown?testid=login&window=1h", nil)
	requireStatus(t, rec, http.StatusOK)
	a := decodeBody[TraceBreakdown](t, rec).Targets[0]
	if a.Error != "" || len(a.TopQueries) == 0 {
		t.Fatalf("target = %+v, want its traces regardless", a)
	}
	if a.Postgres == nil || !strings.Contains(a.Postgres.Error, "shared_preload_libraries") || a.TopQueries[0].Postgres != nil {
		t.Errorf("postgres = %+v, want why pg_stat_statements can't be read", a.Postgres)
	}
}
