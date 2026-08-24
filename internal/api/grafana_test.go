/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package api

import (
	"net/http"
	"net/url"
	"slices"
	"testing"
)

func TestEmbedURL(t *testing.T) {
	s, _ := newServer(t, Config{GrafanaURL: "https://grafana.bench.example/"})
	rec := do(t, s, http.MethodGet,
		"/api/v1/grafana/embed-url?dashboard=compare-tests&vars=testid:login-a,testid:login-b&from=now-6h&theme=dark", nil)
	requireStatus(t, rec, http.StatusOK)

	body := decodeBody[map[string]string](t, rec)
	parsed, err := url.Parse(body["url"])
	if err != nil {
		t.Fatalf("embed URL is not a URL: %v", err)
	}
	if parsed.Host != "grafana.bench.example" {
		t.Fatalf("embed URL points at %q", parsed.Host)
	}
	if parsed.Path != "/d/akbench-compare/authentik-bench-compare-tests" {
		t.Fatalf("embed URL path is %q", parsed.Path)
	}
	q := parsed.Query()
	// A repeated variable is how the compare dashboard overlays two tests.
	if got := q["var-testid"]; !slices.Equal(got, []string{"login-a", "login-b"}) {
		t.Fatalf("var-testid is %v, want both tests", got)
	}
	if q.Get("from") != "now-6h" || q.Get("to") != "now" {
		t.Fatalf("time range is %q..%q", q.Get("from"), q.Get("to"))
	}
	if _, ok := q["kiosk"]; !ok {
		t.Fatalf("embed URL is missing kiosk mode: %v", q)
	}
	if q.Get("theme") != "dark" {
		t.Fatalf("theme is %q", q.Get("theme"))
	}
}

// The URL goes straight into an iframe, so only known dashboards may be named.
func TestEmbedURLRejectsUnknownDashboard(t *testing.T) {
	s, _ := newServer(t, Config{GrafanaURL: "https://grafana.bench.example"})
	rec := do(t, s, http.MethodGet, "/api/v1/grafana/embed-url?dashboard=../../evil", nil)
	requireStatus(t, rec, http.StatusBadRequest)
}

func TestEmbedURLRejectsMalformedVars(t *testing.T) {
	s, _ := newServer(t, Config{GrafanaURL: "https://grafana.bench.example"})
	for _, vars := range []string{"testid", "testid:a b", "te st:a"} {
		rec := do(t, s, http.MethodGet, "/api/v1/grafana/embed-url?vars="+url.QueryEscape(vars), nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("vars %q got status %d, want 400", vars, rec.Code)
		}
	}
}

// An unparseable time range falls back to the default rather than being
// passed through to Grafana.
func TestEmbedURLSanitisesTimeRange(t *testing.T) {
	s, _ := newServer(t, Config{GrafanaURL: "https://grafana.bench.example"})
	rec := do(t, s, http.MethodGet, "/api/v1/grafana/embed-url?from=javascript:alert(1)", nil)
	requireStatus(t, rec, http.StatusOK)

	parsed, err := url.Parse(decodeBody[map[string]string](t, rec)["url"])
	if err != nil {
		t.Fatalf("embed URL is not a URL: %v", err)
	}
	if got := parsed.Query().Get("from"); got != "now-1h" {
		t.Fatalf("from is %q, want the default", got)
	}
}

func TestEmbedURLWithoutGrafana(t *testing.T) {
	s, _ := newServer(t, Config{})
	requireStatus(t, do(t, s, http.MethodGet, "/api/v1/grafana/embed-url", nil), http.StatusServiceUnavailable)
}
