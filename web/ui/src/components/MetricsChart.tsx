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
import { useMemo } from 'react';
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
import { colorFor, formatMetricValue } from '../format';
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

/**
 * rangeChartOptions builds the Chart.js options shared by every line chart
 * on this page - a numeric (unix-seconds) x axis rather than Chart.js' own
 * 'time' scale, which would need the date-fns adapter as an extra
 * dependency for a feature (calendar-aware ticks) nothing here needs.
 */
function rangeChartOptions(unit: string, showLegend: boolean): ChartOptions<'line'> {
  return {
    responsive: true,
    maintainAspectRatio: false,
    animation: false,
    normalized: true,
    interaction: { mode: 'index', intersect: false },
    scales: {
      x: {
        type: 'linear',
        ticks: { callback: v => formatTime(v as number), color: 'var(--muted)', font: { size: 11 }, maxTicksLimit: 8 },
        grid: { color: 'var(--border)' },
        border: { color: 'var(--border)' },
      },
      y: {
        beginAtZero: true,
        // Ratios capped at 100%; everything else auto-scales.
        max: unit === 'ratio' ? 1 : undefined,
        ticks: { callback: v => formatMetricValue(v as number, unit), color: 'var(--muted)', font: { size: 11 } },
        grid: { color: 'var(--border)' },
        border: { color: 'var(--border)' },
      },
    },
    plugins: {
      legend: { display: showLegend, labels: { color: 'var(--text)', font: { size: 12 }, boxHeight: 8 } },
      tooltip: {
        backgroundColor: 'var(--panel)',
        titleColor: 'var(--text)',
        bodyColor: 'var(--text)',
        borderColor: 'var(--border)',
        borderWidth: 1,
        titleFont: { size: 12 },
        bodyFont: { size: 12 },
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
 * series, labeled by whatever the caller wants (a plain testid, or a
 * shortened pod name) - spanGaps true mirrors recharts' own connectNulls. */
function rangeDatasets(data: RangeResponse | undefined, labelFor: (seriesKey: string) => string) {
  return (data?.series ?? []).map((series, i) => ({
    label: labelFor(series.testid),
    data: (series.points ?? []).map(p => ({ x: p.time, y: p.value })),
    borderColor: colorFor(i),
    backgroundColor: colorFor(i),
    spanGaps: true,
  }));
}

export function MetricsChart({
  metric,
  testids,
  rangeSeconds = 900,
  step = '15s',
  refreshMs = 15000,
  height = 220,
  endSeconds,
}: {
  metric: string;
  testids: string[];
  rangeSeconds?: number;
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
      return api.metricsRange(testids, metric, end - rangeSeconds, end, step);
    },
    [testids.join(','), metric, rangeSeconds, step, endSeconds],
    // A fixed-in-the-past window never gains new data - don't poll it.
    endSeconds !== undefined ? 0 : refreshMs
  );

  const unit = data?.metric.unit ?? '';
  const label = data?.metric.label ?? metric;
  const datasets = useMemo(() => rangeDatasets(data, id => id), [data]);
  const hasData = datasets.some(d => d.data.length > 0);
  const options = useMemo(() => rangeChartOptions(unit, testids.length > 1), [unit, testids.length]);

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
  step = '15s',
  refreshMs = 15000,
  height = 320,
  endSeconds,
}: {
  resource: 'cpu' | 'memory';
  testids: string[];
  rangeSeconds?: number;
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
      return api.resourceRange(testids, resource, end - rangeSeconds, end, step);
    },
    [testids.join(','), resource, rangeSeconds, step, endSeconds],
    endSeconds !== undefined ? 0 : refreshMs
  );

  const unit = data?.metric.unit ?? '';
  const label = data?.metric.label ?? resource;
  const datasets = useMemo(() => rangeDatasets(data, id => id), [data]);
  const hasData = datasets.some(d => d.data.length > 0);
  const options = useMemo(() => rangeChartOptions(unit, testids.length > 1), [unit, testids.length]);

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
