/*
 * Copyright 2026
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// One metric over time, one line per test, from /metrics/range or
// /metrics/resources. Natively rendered (Chart.js via react-chartjs-2) - no
// Grafana iframe.
import { useEffect, useMemo, useState } from 'react';
import {
  Chart as ChartJS,
  Legend as ChartJSLegend,
  LinearScale,
  LineElement,
  PointElement,
  Tooltip as ChartJSTooltip,
  type ChartOptions,
  type TooltipItem,
} from 'chart.js';
import { Line } from 'react-chartjs-2';
import { api } from '../api';
import { colorFor, formatMetricValue, stepFor } from '../format';
import { CompareMetric, RangeResponse } from '../types';
import { useResource } from '../useResource';

ChartJS.register(LinearScale, PointElement, LineElement, ChartJSLegend, ChartJSTooltip);

function formatTime(seconds: number): string {
  return new Date(seconds * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

/**
 * useAvailableMetrics discovers the metric definitions cmd/apiserver knows
 * about (internal/api/metrics.go's compareMetrics) from a live
 * /metrics/compare call, rather than hardcoding a list here that would
 * silently go stale whenever the backend adds or renames one. Only the
 * definitions are used; the testid is just to get a real response. Fetched
 * once - they are static for a running backend.
 */
export function useAvailableMetrics(testids: string[]): CompareMetric[] {
  const primary = testids[0];
  const result = useResource(
    () => (primary ? api.compare([primary], '5m') : Promise.resolve(undefined)),
    [primary],
    0
  );
  return result.data?.metrics ?? [];
}

/** ChartTheme is the resolved theme the charts draw with. Chart.js paints
 * to a canvas, which can't resolve CSS custom properties - a literal
 * 'var(--muted)' silently falls back to black - so the values are read off
 * the document instead. */
interface ChartTheme {
  text: string;
  muted: string;
  border: string;
  panel: string;
  font: string;
}

function readChartTheme(): ChartTheme {
  const style = getComputedStyle(document.documentElement);
  const value = (name: string, fallback: string) => style.getPropertyValue(name).trim() || fallback;
  return {
    text: value('--text', '#151515'),
    muted: value('--muted', '#6a6e73'),
    border: value('--border', '#d2d2d2'),
    panel: value('--panel', '#ffffff'),
    font: value('--ak-global--font-family--body', 'sans-serif'),
  };
}

/** useChartTheme re-reads the theme whenever main.tsx flips
 * html[data-theme] (an OS light/dark switch), so open charts follow along. */
function useChartTheme(): ChartTheme {
  const [theme, setTheme] = useState(readChartTheme);
  useEffect(() => {
    const observer = new MutationObserver(() => setTheme(readChartTheme()));
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });
    return () => observer.disconnect();
  }, []);
  return theme;
}

/**
 * rangeChartOptions builds the Chart.js options shared by every line chart
 * on this page - a numeric (unix-seconds) x axis rather than Chart.js' own
 * 'time' scale, which would need the date-fns adapter as an extra
 * dependency for a feature (calendar-aware ticks) nothing here needs.
 */
function rangeChartOptions(unit: string, showLegend: boolean, theme: ChartTheme): ChartOptions<'line'> {
  const font = { family: theme.font, size: 11 };
  return {
    responsive: true,
    maintainAspectRatio: false,
    animation: false,
    normalized: true,
    interaction: { mode: 'index', intersect: false },
    scales: {
      x: {
        type: 'linear',
        ticks: { callback: v => formatTime(v as number), color: theme.muted, font, maxTicksLimit: 8 },
        grid: { color: theme.border },
        border: { color: theme.border },
      },
      y: {
        beginAtZero: true,
        // Ratios capped at 100%; everything else auto-scales.
        max: unit === 'ratio' ? 1 : undefined,
        ticks: { callback: v => formatMetricValue(v as number, unit), color: theme.muted, font },
        grid: { color: theme.border },
        border: { color: theme.border },
      },
    },
    plugins: {
      legend: {
        display: showLegend,
        position: 'bottom',
        labels: { color: theme.text, font: { ...font, size: 12 }, boxWidth: 12, boxHeight: 3, padding: 14 },
      },
      tooltip: {
        backgroundColor: theme.panel,
        titleColor: theme.text,
        bodyColor: theme.text,
        borderColor: theme.border,
        borderWidth: 1,
        titleFont: { ...font, size: 12 },
        bodyFont: { ...font, size: 12 },
        callbacks: {
          title: items => formatTime(items[0]?.parsed.x ?? 0),
          label: (item: TooltipItem<'line'>) =>
            `${item.dataset.label}: ${formatMetricValue(item.parsed.y ?? undefined, unit)}`,
        },
      },
    },
    elements: {
      point: { radius: 0, hoverRadius: 3 },
      line: { tension: 0.25, borderWidth: 2 },
    },
  };
}

/** rangeDatasets turns a RangeResponse into Chart.js datasets, one per
 * (testid, target) series. A series is labeled by its testid alone unless
 * that testid has more than one target in this response (a multi-deployment
 * TestCase), in which case "testid / target" disambiguates - avoids an
 * always-on suffix for the common single-deployment case. spanGaps true
 * mirrors recharts' own connectNulls. */
function rangeDatasets(data: RangeResponse | undefined) {
  const series = data?.series ?? [];
  const targetsByTestID = new Map<string, Set<string>>();
  for (const s of series) {
    const targets = targetsByTestID.get(s.testid) ?? new Set<string>();
    targets.add(s.target);
    targetsByTestID.set(s.testid, targets);
  }
  // A single test's own page needn't repeat its name on every line.
  const singleTest = targetsByTestID.size === 1;
  return series.map((s, i) => ({
    label:
      (targetsByTestID.get(s.testid)?.size ?? 0) > 1
        ? singleTest
          ? s.target
          : `${s.testid} / ${s.target}`
        : s.testid,
    data: (s.points ?? []).map(p => ({ x: p.time, y: p.value })),
    borderColor: colorFor(i),
    backgroundColor: colorFor(i),
    spanGaps: true,
  }));
}

export function MetricsChart({
  metric,
  testids,
  rangeSeconds = 900,
  step,
  refreshMs = 15000,
  height = 220,
  endSeconds,
}: {
  metric: string;
  testids: string[];
  rangeSeconds?: number;
  /** Defaults to stepFor(rangeSeconds). */
  step?: string;
  refreshMs?: number;
  height?: number;
  /** Anchors the window to a unix time instead of "now" - for a Quick
   * TestCase whose k6 Job already exited (format.ts's metricsAnchorFromRuns). */
  endSeconds?: number;
}) {
  const { data, error } = useResource<RangeResponse | undefined>(
    () => {
      if (testids.length === 0) {
        return Promise.resolve(undefined);
      }
      const end = endSeconds ?? Math.floor(Date.now() / 1000);
      return api.metricsRange(testids, metric, end - rangeSeconds, end, step ?? stepFor(rangeSeconds));
    },
    [testids.join(','), metric, rangeSeconds, step, endSeconds],
    // A fixed-in-the-past window never gains new data - don't poll it.
    endSeconds !== undefined ? 0 : refreshMs
  );

  const unit = data?.metric.unit ?? '';
  const label = data?.metric.label ?? metric;
  const datasets = useMemo(() => rangeDatasets(data), [data]);
  const hasData = datasets.some(d => d.data.length > 0);
  const theme = useChartTheme();
  const options = useMemo(() => rangeChartOptions(unit, datasets.length > 1, theme), [unit, datasets.length, theme]);

  return (
    <div className="panel">
      <h3>{label}</h3>
      {error && <div className="alert error">{error}</div>}
      {!hasData ? (
        <div className="muted" style={{ height, display: 'grid', placeItems: 'center' }}>
          No data in this window yet.
        </div>
      ) : (
        <div style={{ height }}>
          <Line data={{ datasets }} options={options} />
        </div>
      )}
    </div>
  );
}

/** MetricsGrid charts every available metric for the given tests. */
export function MetricsGrid({
  testids,
  endSeconds,
  rangeSeconds,
}: {
  testids: string[];
  endSeconds?: number;
  rangeSeconds?: number;
}) {
  const metrics = useAvailableMetrics(testids);
  return (
    <div className="grid2">
      {metrics.map(m => (
        <MetricsChart
          key={m.key}
          metric={m.key}
          testids={testids}
          endSeconds={endSeconds}
          rangeSeconds={rangeSeconds}
        />
      ))}
    </div>
  );
}

/**
 * ResourceChart is one big chart for one resource (cpu/memory), one line per
 * *test* - each line is that test's own deployment summed across its pods
 * (internal/api/podmetrics.go), not broken out per pod: confirmed live that
 * reads as unreadable (a deployment mid-rollout leaves its old ReplicaSet
 * generation's pods in the same time window as the new one, turning a
 * single test into 6+ tangled lines). Matches MetricsChart's own "one line
 * per test" shape above - this is the Compare page's resource-usage
 * equivalent of the k6-metric charts, not a per-pod drill-down.
 */
export function ResourceChart({
  resource,
  testids,
  rangeSeconds = 900,
  step,
  refreshMs = 15000,
  height = 320,
  endSeconds,
}: {
  resource: 'cpu' | 'memory';
  testids: string[];
  rangeSeconds?: number;
  /** Defaults to stepFor(rangeSeconds). */
  step?: string;
  refreshMs?: number;
  height?: number;
  endSeconds?: number;
}) {
  const { data, error } = useResource<RangeResponse | undefined>(
    () => {
      if (testids.length === 0) {
        return Promise.resolve(undefined);
      }
      const end = endSeconds ?? Math.floor(Date.now() / 1000);
      return api.resourceRange(testids, resource, end - rangeSeconds, end, step ?? stepFor(rangeSeconds));
    },
    [testids.join(','), resource, rangeSeconds, step, endSeconds],
    endSeconds !== undefined ? 0 : refreshMs
  );

  const unit = data?.metric.unit ?? '';
  const label = data?.metric.label ?? resource;
  const datasets = useMemo(() => rangeDatasets(data), [data]);
  const hasData = datasets.some(d => d.data.length > 0);
  const theme = useChartTheme();
  const options = useMemo(() => rangeChartOptions(unit, datasets.length > 1, theme), [unit, datasets.length, theme]);

  return (
    <div className="panel">
      <h3>{label}</h3>
      {error && <div className="alert error">{error}</div>}
      {!hasData ? (
        <div className="muted" style={{ height, display: 'grid', placeItems: 'center' }}>
          No data in this window yet.
        </div>
      ) : (
        <div style={{ height }}>
          <Line data={{ datasets }} options={options} />
        </div>
      )}
    </div>
  );
}

/** ResourceCharts renders exactly two big charts - CPU and memory - each
 * with one line per given test. */
export function ResourceCharts({
  testids,
  endSeconds,
  rangeSeconds,
}: {
  testids: string[];
  endSeconds?: number;
  rangeSeconds?: number;
}) {
  return (
    <div className="grid2">
      <ResourceChart resource="cpu" testids={testids} endSeconds={endSeconds} rangeSeconds={rangeSeconds} />
      <ResourceChart resource="memory" testids={testids} endSeconds={endSeconds} rangeSeconds={rangeSeconds} />
    </div>
  );
}
