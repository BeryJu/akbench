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
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const dashboardDir = "../../gitops/dashboards"

type dashboardFile struct {
	UID        string `json:"uid"`
	Title      string `json:"title"`
	Templating struct {
		List []struct {
			Name string `json:"name"`
		} `json:"list"`
	} `json:"templating"`
	Panels []struct {
		Title   string `json:"title"`
		Targets []struct {
			Expr string `json:"expr"`
		} `json:"targets"`
	} `json:"panels"`
}

func loadDashboards(t *testing.T) map[string]dashboardFile {
	t.Helper()
	entries, err := os.ReadDir(dashboardDir)
	if err != nil {
		t.Fatalf("read dashboard directory: %v", err)
	}
	out := map[string]dashboardFile{}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dashboardDir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		var dash dashboardFile
		if err := json.Unmarshal(raw, &dash); err != nil {
			t.Fatalf("%s is not valid JSON: %v", entry.Name(), err)
		}
		out[strings.TrimSuffix(entry.Name(), ".json")] = dash
	}
	return out
}

// renderEmbeddedDashboardDir is internal/render's own copy of these exact
// dashboards, vendored via go:embed since go:embed can't reach outside its
// own package's directory tree (see internal/render/grafana.go's own doc
// comment on the embed directives). Nothing enforced the two copies ever
// stayed in sync - TestDashboardsMatchRenderEmbeds does.
const renderEmbeddedDashboardDir = "../render/manifests/grafana-dashboards"

// TestDashboardsMatchRenderEmbeds guards against gitops/dashboards/*.json
// (what a human edits, and what this file's other tests validate) silently
// drifting from internal/render/manifests/grafana-dashboards/*.json (what
// GrafanaObjects actually ships to every workload cluster) - the two are
// meant to be byte-identical copies of the same file, and nothing else in
// this repo would ever have caught them diverging.
func TestDashboardsMatchRenderEmbeds(t *testing.T) {
	entries, err := os.ReadDir(dashboardDir)
	if err != nil {
		t.Fatalf("read %s: %v", dashboardDir, err)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		source, err := os.ReadFile(filepath.Join(dashboardDir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		embedded, err := os.ReadFile(filepath.Join(renderEmbeddedDashboardDir, entry.Name()))
		if err != nil {
			t.Errorf("%s exists in %s but not %s: %v", entry.Name(), dashboardDir, renderEmbeddedDashboardDir, err)
			continue
		}
		if !bytes.Equal(source, embedded) {
			t.Errorf("%s differs between %s and %s - keep the two copies in sync (see internal/render/grafana.go's own doc comment)",
				entry.Name(), dashboardDir, renderEmbeddedDashboardDir)
		}
	}
}

// k6MetricNames is every series k6's experimental-prometheus-rw output can
// produce for the built-in metrics, derived from the metric's own type: a
// Counter gets a _total suffix, a Rate a _rate suffix, a Gauge neither, and a
// Trend - which the runner configures as a native histogram - is named after
// its base unit (seconds for times, bytes for data, nothing otherwise).
//
// This exists because k6's documentation names the metrics without any of
// those suffixes, so a dashboard written from the docs silently graphs
// nothing.
var k6MetricNames = func() map[string]bool {
	names := map[string]bool{}
	for _, name := range []string{"iterations", "dropped_iterations", "http_reqs"} {
		names["k6_"+name+"_total"] = true
	}
	for _, name := range []string{"data_sent", "data_received"} {
		names["k6_"+name+"_total"] = true
	}
	for _, name := range []string{"checks", "http_req_failed"} {
		names["k6_"+name+"_rate"] = true
	}
	for _, name := range []string{"vus", "vus_max"} {
		names["k6_"+name] = true
	}
	for _, name := range []string{
		"iteration_duration", "group_duration",
		"http_req_duration", "http_req_blocked", "http_req_connecting",
		"http_req_tls_handshaking", "http_req_sending", "http_req_waiting",
		"http_req_receiving",
	} {
		names["k6_"+name+"_seconds"] = true
	}
	return names
}()

var k6MetricRef = regexp.MustCompile(`\bk6_[a-z0-9_]+\b`)

func TestK6MetricNamesAreReal(t *testing.T) {
	for name, dash := range loadDashboards(t) {
		for _, panel := range dash.Panels {
			for _, target := range panel.Targets {
				for _, metric := range k6MetricRef.FindAllString(target.Expr, -1) {
					if !k6MetricNames[metric] {
						t.Errorf("%s.json panel %q queries %q, which k6 never emits (have: %s)",
							name, panel.Title, metric, strings.Join(sortedKeys(k6MetricNames), ", "))
					}
				}
			}
		}
	}
}

// Every k6 dashboard has to split on the tags the runner actually sets, or the
// tag scheme ported from the Ansible harness buys nothing.
func TestK6DashboardsSplitOnTheTagScheme(t *testing.T) {
	files := loadDashboards(t)
	for _, name := range []string{"compare-tests", "k6"} {
		dash, ok := files[name]
		if !ok {
			t.Fatalf("%s.json is missing", name)
		}
		vars := map[string]bool{}
		for _, v := range dash.Templating.List {
			vars[v.Name] = true
		}
		for _, want := range []string{"testid", "target", "profile", "cluster"} {
			if !vars[want] {
				t.Errorf("%s.json has no %q template variable", name, want)
			}
		}
	}
}

// Trends arrive as native histograms, which carry no le label; grouping by one
// yields an empty result rather than an error, so nothing but a check like this
// would catch it.
func TestNativeHistogramQuantilesDoNotGroupByLe(t *testing.T) {
	for name, dash := range loadDashboards(t) {
		for _, panel := range dash.Panels {
			for _, target := range panel.Targets {
				if !strings.Contains(target.Expr, "histogram_quantile") {
					continue
				}
				if strings.Contains(target.Expr, "le)") || strings.Contains(target.Expr, "le,") {
					t.Errorf("%s.json panel %q quantiles a native histogram by le: %s", name, panel.Title, target.Expr)
				}
			}
		}
	}
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
