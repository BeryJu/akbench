package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const dashboardDir = "../render/manifests/grafana-dashboards"

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
		Type    string `json:"type"`
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

// validGrafanaPanelTypes are the panel types this project's own dashboards
// actually use - not exhaustive of every type Grafana ships, just enough to
// catch the real bug this guards: eight panels across every one of these
// five dashboards once had their own explanatory text saved into Grafana's
// "type" field instead of "description" (an editing mistake, not a valid
// panel type at all), which silently made Grafana refuse to render them as
// a real timeseries panel regardless of whether their underlying PromQL/data
// was fine - confirmed live. A panel's own "type" should never be prose.
var validGrafanaPanelTypes = map[string]bool{
	"timeseries":     true,
	"stat":           true,
	"table":          true,
	"gauge":          true,
	"bargauge":       true,
	"piechart":       true,
	"row":            true,
	"text":           true,
	"state-timeline": true,
}

func TestDashboardPanelsHaveARealType(t *testing.T) {
	for name, dash := range loadDashboards(t) {
		for _, panel := range dash.Panels {
			if !validGrafanaPanelTypes[panel.Type] {
				t.Errorf("%s.json panel %q has type %q, not a real Grafana panel type - did its description end up in \"type\" by mistake?", name, panel.Title, panel.Type)
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
