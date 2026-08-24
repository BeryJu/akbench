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
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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

// The embed endpoint hands the browser a /d/<uid>/ URL, so a UID that drifts
// from the dashboard it names produces a 404 inside an iframe - the least
// debuggable failure this UI can have.
func TestDashboardsMatchEmbedAllowlist(t *testing.T) {
	files := loadDashboards(t)

	for name, dash := range dashboards {
		file, ok := files[name]
		if !ok {
			t.Errorf("dashboard %q is embeddable but has no %s/%s.json", name, dashboardDir, name)
			continue
		}
		if file.UID != dash.UID {
			t.Errorf("dashboard %q has uid %q in its JSON, %q in the allowlist", name, file.UID, dash.UID)
		}
		if got := slugify(file.Title); got != dash.Slug {
			t.Errorf("dashboard %q title %q slugifies to %q, allowlist says %q", name, file.Title, got, dash.Slug)
		}
	}
	for name := range files {
		if _, ok := dashboards[name]; !ok {
			t.Errorf("%s.json exists but is not embeddable; add it to the allowlist", name)
		}
	}
}

// slugify reproduces how Grafana derives a dashboard's URL slug from its title.
func slugify(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	slug := regexp.MustCompile(`-+`).ReplaceAllString(b.String(), "-")
	return strings.Trim(slug, "-")
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
	sort.Strings(keys)
	return keys
}
