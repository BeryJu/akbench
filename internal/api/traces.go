package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"

	benchv1alpha1 "github.com/goauthentik/authentik-bench/api/v1alpha1"
	"github.com/goauthentik/authentik-bench/internal/render"
)

// The trace breakdown answers "where does a login's time go, per target":
// request latency, how much of it is the database, how many queries a
// request makes, per-stage latency and the costliest queries - read from
// the traces authentik itself exports to its workload cluster's Tempo.
//
// It's a sample, not an aggregate: one TraceQL search per target returning
// the most recent traceBreakdownLimit traces' server, stage and database
// spans. Tempo's TraceQL metrics (quantile_over_time & co.) would aggregate
// server-side, but can't select these spans per target: authentik records
// the k6 x-benchmark-* headers only on the request's server span, as string
// arrays (the OTel semconv shape for http.request.header.*), and Tempo's
// structural operators (>>) don't match array attributes - so the only way
// to tie a database or stage span to a target is a spanset-level && within
// a search, which TraceQL metrics don't support.
const (
	traceBreakdownLimit    = 15
	traceBreakdownCacheTTL = 2 * time.Minute
	traceTopQueries        = 8
	maxStatementLength     = 400
)

var traceBreakdownWindows = map[string]time.Duration{
	"15m": 15 * time.Minute,
	"1h":  time.Hour,
	"6h":  6 * time.Hour,
}

// TraceStage is one flow stage's latency across the sampled requests.
type TraceStage struct {
	Name  string  `json:"name"`
	Count int     `json:"count"`
	P50   float64 `json:"p50"`
	P95   float64 `json:"p95"`
}

// TraceQuery is one (parameterized) SQL statement's share of the sample.
type TraceQuery struct {
	Statement string `json:"statement"`
	Count     int    `json:"count"`
	// PerRequest is how many times one request runs it, on average.
	PerRequest float64 `json:"perRequest"`
	// Total is its summed duration across the sample, in seconds.
	Total float64 `json:"total"`
	Mean  float64 `json:"mean"`
}

// TraceTarget is one target's breakdown. Durations are seconds.
type TraceTarget struct {
	Target   string `json:"target"`
	Traces   int    `json:"traces"`
	Requests int    `json:"requests"`
	// RequestP50/P95 are per HTTP request to authentik, not per login - a
	// login is several requests, and a sampled trace may be cut short by
	// the search window.
	RequestP50        float64      `json:"requestP50"`
	RequestP95        float64      `json:"requestP95"`
	QueriesPerRequest float64      `json:"queriesPerRequest"`
	DBTimePerRequest  float64      `json:"dbTimePerRequest"`
	DBShare           float64      `json:"dbShare"`
	Stages            []TraceStage `json:"stages"`
	TopQueries        []TraceQuery `json:"topQueries"`
	Error             string       `json:"error,omitempty"`
}

// TraceBreakdown is the response body.
type TraceBreakdown struct {
	TestID      string        `json:"testid"`
	Window      string        `json:"window"`
	GeneratedAt time.Time     `json:"generatedAt"`
	Targets     []TraceTarget `json:"targets"`
}

type traceBreakdownCache struct {
	mu      sync.Mutex
	entries map[string]TraceBreakdown
}

// tempoTarget is one deployment a testid drives, with the Tempo that holds
// its traces: its own workload cluster's, reached through that cluster's
// Grafana datasource proxy.
type tempoTarget struct {
	Target        string
	TempoURL      string
	AdminPassword string
	Error         string
}

func (s *Server) tempoTargetsForTestID(ctx context.Context, tc *benchv1alpha1.TestCase) []tempoTarget {
	var targets []tempoTarget
	for _, ref := range tc.Deployments() {
		var topo benchv1alpha1.AuthentikDeployment
		if err := s.client.Get(ctx, types.NamespacedName{Name: ref.Name, Namespace: s.cfg.Namespace}, &topo); err != nil {
			targets = append(targets, tempoTarget{Target: ref.Name, Error: "deployment not found"})
			continue
		}
		target := tempoTarget{Target: topo.Spec.TargetName}
		if target.Target == "" {
			target.Target = topo.Name
		}
		// TargetName is a free-form spec field (unlike the object name
		// itself, which the apiserver already constrains to a DNS subdomain)
		// and is interpolated directly into a TraceQL search below - reject
		// anything that isn't a safe label value rather than trust it,
		// rather than rely on fmt's %q escaping matching whatever escaping
		// rules Tempo's own TraceQL parser happens to implement.
		if !safeLabelValue.MatchString(target.Target) {
			targets = append(targets, tempoTarget{Target: target.Target, Error: "targetName contains characters that aren't safe to query Tempo with"})
			continue
		}
		var wc benchv1alpha1.WorkloadCluster
		wcKey := types.NamespacedName{Name: topo.Spec.ClusterRef.Name, Namespace: s.cfg.Namespace}
		switch {
		case topo.Spec.ClusterRef.Name == "":
			target.Error = "deployment runs on the management cluster, which has no Tempo"
		case s.client.Get(ctx, wcKey, &wc) != nil:
			target.Error = "workload cluster not found"
		case wc.Status.GrafanaURL == "":
			target.Error = "workload cluster has no Grafana yet"
		default:
			adminPassword, err := s.grafanaAdminPassword(ctx, &wc)
			if err != nil {
				target.Error = err.Error()
				break
			}
			target.TempoURL = strings.TrimSuffix(wc.Status.GrafanaURL, "/") +
				render.GrafanaProxyPathPrefix(&wc) + "api/datasources/proxy/uid/" + render.TempoDatasourceUID
			target.AdminPassword = adminPassword
		}
		targets = append(targets, target)
	}
	return targets
}

// traceBreakdown answers GET /traces/breakdown?testid=&window=15m|1h|6h.
func (s *Server) traceBreakdown(w http.ResponseWriter, r *http.Request) {
	testid := r.URL.Query().Get("testid")
	window := r.URL.Query().Get("window")
	if window == "" {
		window = "1h"
	}
	span, ok := traceBreakdownWindows[window]
	if testid == "" || !ok {
		writeError(w, r, http.StatusBadRequest, "testid and window (15m, 1h or 6h) are required")
		return
	}
	cacheKey := testid + "|" + window
	s.traces.mu.Lock()
	cached, hit := s.traces.entries[cacheKey]
	s.traces.mu.Unlock()
	if hit && time.Since(cached.GeneratedAt) < traceBreakdownCacheTTL {
		writeJSON(w, r, http.StatusOK, cached)
		return
	}

	var tc benchv1alpha1.TestCase
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: testid, Namespace: s.cfg.Namespace}, &tc); err != nil {
		writeAPIError(w, r, err)
		return
	}
	end := time.Now()
	resp := TraceBreakdown{TestID: testid, Window: window, GeneratedAt: end}
	targets := s.tempoTargetsForTestID(r.Context(), &tc)
	resp.Targets = make([]TraceTarget, len(targets))
	var wg sync.WaitGroup
	for i, target := range targets {
		resp.Targets[i] = TraceTarget{Target: target.Target, Error: target.Error}
		if target.Error != "" {
			continue
		}
		wg.Go(func() {
			result, err := s.searchTraceSpans(r.Context(), target.TempoURL, target.AdminPassword, testid, target.Target, end.Add(-span), end)
			if err != nil {
				resp.Targets[i].Error = err.Error()
				return
			}
			resp.Targets[i] = summarizeTraceSpans(target.Target, result)
		})
	}
	wg.Wait()

	s.traces.mu.Lock()
	s.traces.entries[cacheKey] = resp
	s.traces.mu.Unlock()
	writeJSON(w, r, http.StatusOK, resp)
}

// tempoSearchResponse is the subset of Tempo's /api/search response used here.
type tempoSearchResponse struct {
	Traces []struct {
		SpanSets []tempoSpanSet `json:"spanSets"`
		// Older Tempo versions return a single spanSet.
		SpanSet *tempoSpanSet `json:"spanSet"`
	} `json:"traces"`
}

type tempoSpanSet struct {
	Spans []tempoSpan `json:"spans"`
}

type tempoSpan struct {
	DurationNanos string `json:"durationNanos"`
	Attributes    []struct {
		Key   string `json:"key"`
		Value struct {
			StringValue string `json:"stringValue"`
		} `json:"value"`
	} `json:"attributes"`
}

func (sp tempoSpan) attr(key string) string {
	for _, a := range sp.Attributes {
		if a.Key == key {
			return a.Value.StringValue
		}
	}
	return ""
}

func (sp tempoSpan) seconds() float64 {
	nanos, _ := strconv.ParseFloat(sp.DurationNanos, 64)
	return nanos / 1e9
}

// traceBreakdownQuery selects, per trace, the target's server spans (the
// first spanset - both headers are on them) together with every stage and
// database span in the same trace (the second).
func traceBreakdownQuery(testid, target string) string {
	return fmt.Sprintf(`{ span.http.request.header.x_benchmark_test = %q && span.http.request.header.x_benchmark_target = %q }`+
		` && { name = "authentik.flow.executor.stage" || span.db.system = "postgresql" }`+
		` | select(kind, span.name, span.db.statement)`, testid, target)
}

func (s *Server) searchTraceSpans(ctx context.Context, tempoURL, adminPassword, testid, target string, start, end time.Time) (*tempoSearchResponse, error) {
	q := url.Values{}
	q.Set("q", traceBreakdownQuery(testid, target))
	q.Set("limit", strconv.Itoa(traceBreakdownLimit))
	q.Set("spss", strconv.Itoa(render.TempoMaxSpansPerSpanSet))
	q.Set("start", strconv.FormatInt(start.Unix(), 10))
	q.Set("end", strconv.FormatInt(end.Unix(), 10))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tempoURL+"/api/search?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	// This request reaches Grafana's datasource-proxy endpoint directly
	// (Grafana's own NodePort, not through grafanaProxy's handler), so it
	// needs the same real credentials that handler injects for the browser.
	req.SetBasicAuth(render.GrafanaAdminUser, adminPassword)
	res, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query Tempo: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("query Tempo: %s", res.Status)
	}
	var body tempoSearchResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode Tempo response: %w", err)
	}
	return &body, nil
}

// stageName shortens a stage view's dotted class path
// (authentik.stages.password.stage.PasswordStageView) to its class name.
func stageName(path string) string {
	return path[strings.LastIndex(path, ".")+1:]
}

func summarizeTraceSpans(target string, body *tempoSearchResponse) TraceTarget {
	out := TraceTarget{Target: target, Traces: len(body.Traces), Stages: []TraceStage{}, TopQueries: []TraceQuery{}}
	var requests []float64
	var serverTotal, dbTotal float64
	var queries int
	stages := map[string][]float64{}
	statements := map[string]*TraceQuery{}
	for _, trace := range body.Traces {
		sets := trace.SpanSets
		if len(sets) == 0 && trace.SpanSet != nil {
			sets = []tempoSpanSet{*trace.SpanSet}
		}
		for _, set := range sets {
			for _, sp := range set.Spans {
				d := sp.seconds()
				switch {
				case sp.attr("kind") == "server":
					requests = append(requests, d)
					serverTotal += d
				case sp.attr("db.statement") != "":
					queries++
					dbTotal += d
					statement := sp.attr("db.statement")
					if len(statement) > maxStatementLength {
						statement = statement[:maxStatementLength] + "…"
					}
					entry := statements[statement]
					if entry == nil {
						entry = &TraceQuery{Statement: statement}
						statements[statement] = entry
					}
					entry.Count++
					entry.Total += d
				case sp.attr("name") != "":
					name := stageName(sp.attr("name"))
					stages[name] = append(stages[name], d)
				}
			}
		}
	}
	out.Requests = len(requests)
	if out.Requests == 0 {
		return out
	}
	n := float64(out.Requests)
	out.RequestP50, out.RequestP95 = quantile(requests, 0.5), quantile(requests, 0.95)
	out.QueriesPerRequest = float64(queries) / n
	out.DBTimePerRequest = dbTotal / n
	if serverTotal > 0 {
		out.DBShare = dbTotal / serverTotal
	}
	for name, durations := range stages {
		out.Stages = append(out.Stages, TraceStage{
			Name: name, Count: len(durations), P50: quantile(durations, 0.5), P95: quantile(durations, 0.95),
		})
	}
	slices.SortFunc(out.Stages, func(a, b TraceStage) int { return strings.Compare(a.Name, b.Name) })
	for _, q := range statements {
		q.PerRequest = float64(q.Count) / n
		q.Mean = q.Total / float64(q.Count)
		out.TopQueries = append(out.TopQueries, *q)
	}
	slices.SortFunc(out.TopQueries, func(a, b TraceQuery) int {
		switch {
		case a.Total > b.Total:
			return -1
		case a.Total < b.Total:
			return 1
		}
		return strings.Compare(a.Statement, b.Statement)
	})
	if len(out.TopQueries) > traceTopQueries {
		out.TopQueries = out.TopQueries[:traceTopQueries]
	}
	return out
}

// quantile is the nearest-rank quantile of values (which it sorts).
func quantile(values []float64, q float64) float64 {
	if len(values) == 0 {
		return 0
	}
	slices.Sort(values)
	rank := int(q*float64(len(values))+0.5) - 1
	return values[max(0, min(rank, len(values)-1))]
}
