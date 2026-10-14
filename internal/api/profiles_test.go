package api

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/pprof/profile"
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/authentik-labs/akbench/internal/render"
)

// fakeCPUProfile is shaped like authentik's: a view calling into Django's
// URL resolver (which recurses) and psycopg. 10s of CPU in all.
func fakeCPUProfile(t *testing.T) []byte {
	t.Helper()
	fn := func(id uint64, name, file string) *profile.Function {
		return &profile.Function{ID: id, Name: name, Filename: file}
	}
	view := fn(1, "FlowExecutorView.get", "/authentik/flows/views/executor.py")
	resolve := fn(2, "URLResolver.resolve", "/ak-root/.venv/lib/python3.14/site-packages/django/urls/resolvers.py")
	execute := fn(3, "Cursor.execute", "/ak-root/.venv/lib/python3.14/site-packages/psycopg/cursor.py")
	run := fn(4, "Thread.run", "/usr/local/lib/python3.14/threading.py")
	loc := func(id uint64, f *profile.Function) *profile.Location {
		return &profile.Location{ID: id, Line: []profile.Line{{Function: f}}}
	}
	lView, lResolve, lExecute, lRun := loc(1, view), loc(2, resolve), loc(3, execute), loc(4, run)
	p := &profile.Profile{
		SampleType: []*profile.ValueType{{Type: "cpu", Unit: "nanoseconds"}},
		PeriodType: &profile.ValueType{Type: "cpu", Unit: "nanoseconds"},
		Function:   []*profile.Function{view, resolve, execute, run},
		Location:   []*profile.Location{lView, lResolve, lExecute, lRun},
		Sample: []*profile.Sample{
			// Leaf first: resolve recursing into itself, under the view.
			{Location: []*profile.Location{lResolve, lResolve, lView, lRun}, Value: []int64{5e9}},
			{Location: []*profile.Location{lExecute, lView, lRun}, Value: []int64{3e9}},
			{Location: []*profile.Location{lView, lRun}, Value: []int64{2e9}},
		},
	}
	var buf bytes.Buffer
	if err := p.WriteUncompressed(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// decodeSelectMergeProfile reads back selectMergeProfile's hand-encoded
// request: field number -> string or varint.
func decodeSelectMergeProfile(t *testing.T, b []byte) map[protowire.Number]any {
	t.Helper()
	fields := map[protowire.Number]any{}
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		b = b[n:]
		switch typ {
		case protowire.BytesType:
			v, n := protowire.ConsumeString(b)
			fields[num], b = v, b[n:]
		case protowire.VarintType:
			v, n := protowire.ConsumeVarint(b)
			fields[num], b = v, b[n:]
		default:
			t.Fatalf("unexpected wire type %v", typ)
		}
		if n < 0 {
			t.Fatal("malformed request")
		}
	}
	return fields
}

func TestProfileBreakdown(t *testing.T) {
	var queries atomic.Int32
	var request map[protowire.Number]any
	cpu := fakeCPUProfile(t)
	s := traceFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/grafana-proxy/bench/api/datasources/proxy/uid/pyroscope/querier.v1.QuerierService/SelectMergeProfile" {
			http.NotFound(w, r)
			return
		}
		if user, pass, ok := r.BasicAuth(); !ok || user != render.GrafanaAdminUser || pass != grafanaTestAdminPassword {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		queries.Add(1)
		body, _ := io.ReadAll(r.Body)
		request = decodeSelectMergeProfile(t, body)
		w.Header().Set("Content-Type", "application/proto")
		_, _ = w.Write(cpu)
	}))

	rec := do(t, s, http.MethodGet, "/api/v1/profiles/breakdown?testid=login&window=15m", nil)
	requireStatus(t, rec, http.StatusOK)
	got := decodeBody[ProfileBreakdown](t, rec)
	if got.Component != "server" || len(got.Targets) != 1 {
		t.Fatalf("breakdown: %+v", got)
	}
	if request[1] != pyroscopeCPUProfileType {
		t.Errorf("profile type = %v", request[1])
	}
	if sel, _ := request[2].(string); !strings.Contains(sel, `hostname=~"ak-a-authentik-server-.+"`) {
		t.Errorf("selector not scoped to the deployment's server pods: %v", request[2])
	}
	if start, end := request[3].(uint64), request[4].(uint64); end-start != 15*60*1000 {
		t.Errorf("window = %dms, want 15m", end-start)
	}

	a := got.Targets[0]
	if a.Error != "" {
		t.Fatalf("target error: %s", a.Error)
	}
	approx(t, "cpuSeconds", a.CPUSeconds, 10)
	approx(t, "cores", a.Cores, 10.0/900)
	wantCategories := []ProfileCategory{{"authentik", 0.2}, {"Django", 0.5}, {"Database driver", 0.3}}
	if len(a.Categories) != len(wantCategories) {
		t.Fatalf("categories: %+v", a.Categories)
	}
	for i, c := range wantCategories {
		if a.Categories[i].Name != c.Name {
			t.Errorf("category %d = %s, want %s", i, a.Categories[i].Name, c.Name)
		}
		approx(t, c.Name, a.Categories[i].Share, c.Share)
	}
	if len(a.Functions) != 3 {
		t.Fatalf("functions (Thread.run never runs on-CPU itself): %+v", a.Functions)
	}
	top := a.Functions[0]
	if top.Name != "URLResolver.resolve" || top.File != "django/urls/resolvers.py" || top.Category != "Django" {
		t.Errorf("top function: %+v", top)
	}
	approx(t, "resolve self", top.Self, 0.5)
	approx(t, "resolve total (recursion counted once)", top.Total, 0.5)
	if view := a.Functions[2]; view.File != "authentik/flows/views/executor.py" {
		t.Errorf("view: %+v", view)
	} else {
		approx(t, "view self", view.Self, 0.2)
		approx(t, "view total", view.Total, 1)
	}

	requireStatus(t, do(t, s, http.MethodGet, "/api/v1/profiles/breakdown?testid=login&window=15m", nil), http.StatusOK)
	if n := queries.Load(); n != 1 {
		t.Errorf("Pyroscope queried %d times, want 1 (cached)", n)
	}
	requireStatus(t, do(t, s, http.MethodGet, "/api/v1/profiles/breakdown?testid=login&window=15m&component=worker", nil), http.StatusOK)
	if sel, _ := request[2].(string); !strings.Contains(sel, "ak-a-authentik-worker-") {
		t.Errorf("worker selector: %v", request[2])
	}
}

func TestProfileBreakdownEmpty(t *testing.T) {
	s := traceFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/proto")
	}))
	rec := do(t, s, http.MethodGet, "/api/v1/profiles/breakdown?testid=login&window=1h", nil)
	requireStatus(t, rec, http.StatusOK)
	got := decodeBody[ProfileBreakdown](t, rec)
	if a := got.Targets[0]; a.Error != "" || a.CPUSeconds != 0 || len(a.Functions) != 0 {
		t.Errorf("no profiles should be an empty, error-free target: %+v", a)
	}
}

func TestProfileBreakdownValidates(t *testing.T) {
	s, _ := newServer(t, Config{})
	requireStatus(t, do(t, s, http.MethodGet, "/api/v1/profiles/breakdown?testid=x&window=7d", nil), http.StatusBadRequest)
	requireStatus(t, do(t, s, http.MethodGet, "/api/v1/profiles/breakdown?testid=x&component=postgres", nil), http.StatusBadRequest)
	requireStatus(t, do(t, s, http.MethodGet, "/api/v1/profiles/breakdown", nil), http.StatusBadRequest)
}

func TestProfileCategory(t *testing.T) {
	for file, want := range map[string]string{
		"/authentik/core/models.py":   "authentik",
		"/lifecycle/gunicorn.conf.py": "authentik",
		"/ak-root/.venv/lib/python3.14/site-packages/django/db/models/query.py": "Django",
		"/ak-root/.venv/lib/python3.14/site-packages/psycopg_pool/pool.py":      "Database driver",
		"/ak-root/.venv/lib/python3.14/site-packages/sentry_sdk/hub.py":         "Other libraries",
		"/ak-root/.venv/lib/python3.14/site-packages/django_prometheus/db.py":   "Other libraries",
		"/usr/local/lib/python3.14/threading.py":                                "Python runtime",
		"":                                                                      "Python runtime",
	} {
		if got := profileCategory(file); got != want {
			t.Errorf("profileCategory(%q) = %s, want %s", file, got, want)
		}
	}
}
