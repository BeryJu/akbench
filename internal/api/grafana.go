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
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// dashboard is one of the dashboards shipped in gitops/dashboards, addressed
// by the slug the UI asks for rather than by UID, so the UI never has to know
// Grafana's identifiers.
type dashboard struct {
	UID  string
	Slug string
}

// dashboardCompareTests is embedURL's default when the caller doesn't name
// one, and a key into the dashboards map below.
const dashboardCompareTests = "compare-tests"

// dashboardPostgres is a key into the dashboards map below.
const dashboardPostgres = "postgres"

// dashboards is the allowlist of embeddable dashboards. An allowlist rather
// than free-form UIDs: the returned URL is fed straight into an iframe, so
// anything a caller can influence there is an open-redirect surface.
var dashboards = map[string]dashboard{
	dashboardCompareTests: {UID: "akbench-compare", Slug: "authentik-bench-compare-tests"},
	"k6":                  {UID: "akbench-k6", Slug: "authentik-bench-k6-load-generator"},
	"authentik":           {UID: "akbench-authentik", Slug: "authentik-bench-authentik-under-load"},
	dashboardPostgres:     {UID: "akbench-postgres", Slug: "authentik-bench-postgres"},
	"nodes":               {UID: "akbench-nodes", Slug: "authentik-bench-nodes"},
}

// embedURL answers GET /grafana/embed-url?dashboard=compare-tests&
// vars=testid:a,testid:b&from=now-1h&to=now with the iframe URL for that
// dashboard, pre-filtered by template variables. Repeating a variable name
// (as testid above) sets a multi-value template variable, which is exactly
// how the compare dashboard overlays several tests.
func (s *Server) embedURL(w http.ResponseWriter, r *http.Request) {
	if s.cfg.GrafanaURL == "" {
		writeError(w, r, http.StatusServiceUnavailable, "no Grafana configured")
		return
	}
	q := r.URL.Query()
	name := q.Get("dashboard")
	if name == "" {
		name = dashboardCompareTests
	}
	dash, ok := dashboards[name]
	if !ok {
		writeError(w, r, http.StatusBadRequest, fmt.Sprintf("unknown dashboard %q", name))
		return
	}

	params := url.Values{}
	params.Set("orgId", "1")
	// kiosk strips Grafana's own chrome, which is what makes the iframe look
	// like part of this UI rather than a window into another app.
	params.Set("kiosk", "")
	params.Set("from", timeRangeOrDefault(q.Get("from"), "now-1h"))
	params.Set("to", timeRangeOrDefault(q.Get("to"), "now"))
	if theme := q.Get("theme"); theme == "light" || theme == "dark" {
		params.Set("theme", theme)
	}

	vars, err := parseTemplateVars(q["vars"])
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	names := make([]string, 0, len(vars))
	for name := range vars {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		for _, value := range vars[name] {
			params.Add("var-"+name, value)
		}
	}

	embed := fmt.Sprintf("%s/d/%s/%s?%s", strings.TrimSuffix(s.cfg.GrafanaURL, "/"), dash.UID, dash.Slug, params.Encode())
	writeJSON(w, r, http.StatusOK, map[string]string{"url": embed})
}

// parseTemplateVars reads the repeatable vars parameter, whose value is a
// comma-separated list of name:value pairs
// (vars=testid:login-a,testid:login-b&vars=cluster:management). Both halves
// are validated as label values, since they end up in a URL the browser
// loads.
func parseTemplateVars(raw []string) (map[string][]string, error) {
	vars := map[string][]string{}
	for _, group := range raw {
		for pair := range strings.SplitSeq(group, ",") {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			name, value, found := strings.Cut(pair, ":")
			if !found {
				return nil, fmt.Errorf("malformed vars entry %q, want name:value", pair)
			}
			if !safeLabelValue.MatchString(name) || !safeLabelValue.MatchString(value) {
				return nil, fmt.Errorf("malformed vars entry %q", pair)
			}
			vars[name] = append(vars[name], value)
		}
	}
	return vars, nil
}

// safeTimeRange matches the Grafana time expressions the UI is allowed to
// pass through: relative (now-1h), now, or an epoch in milliseconds.
var safeTimeRange = regexp.MustCompile(`^(now(-[0-9]+[smhdwMy])?(/[smhdwMy])?|[0-9]{1,15})$`)

func timeRangeOrDefault(value, fallback string) string {
	if value == "" || !safeTimeRange.MatchString(value) {
		return fallback
	}
	return value
}
