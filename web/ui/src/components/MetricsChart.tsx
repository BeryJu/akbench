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

// One metric over time, one line per test, from /metrics/range. Natively
// rendered (recharts) - no Grafana iframe.
import { useMemo } from 'react';
import { CartesianGrid, Legend, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { api } from '../api';
import { colorFor, formatMetricValue } from '../format';
import { CompareMetric, RangeResponse } from '../types';
import { useResource } from '../useResource';

function formatTime(seconds: number): string {
  return new Date(seconds * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

const axisStyle = { fontSize: 11, fill: 'var(--muted)' };

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

  const { rows, unit, label } = useMemo(() => {
    if (!data) {
      return { rows: [] as Record<string, number>[], unit: '', label: metric };
    }
    const byTime = new Map<number, Record<string, number>>();
    for (const series of data.series ?? []) {
      // points is always [] server-side, but never trust that blindly.
      for (const point of series.points ?? []) {
        const row = byTime.get(point.time) ?? { time: point.time };
        row[series.testid] = point.value;
        byTime.set(point.time, row);
      }
    }
    return {
      rows: Array.from(byTime.values()).sort((a, b) => a.time - b.time),
      unit: data.metric.unit,
      label: data.metric.label,
    };
  }, [data, metric]);

  return (
    <div className="panel">
      <h3>{label}</h3>
      {error && <div className="alert error">{error}</div>}
      {rows.length === 0 ? (
        <div className="muted" style={{ height, display: 'grid', placeItems: 'center' }}>
          No data in this window yet.
        </div>
      ) : (
        <ResponsiveContainer width="100%" height={height}>
          <LineChart data={rows} margin={{ top: 4, right: 12, left: 0, bottom: 0 }}>
            <CartesianGrid strokeDasharray="3 3" stroke="var(--border)" />
            <XAxis
              dataKey="time"
              type="number"
              domain={['dataMin', 'dataMax']}
              tickFormatter={v => formatTime(v as number)}
              tick={axisStyle}
              stroke="var(--border)"
              minTickGap={40}
            />
            <YAxis
              tick={axisStyle}
              stroke="var(--border)"
              tickFormatter={v => formatMetricValue(v as number, unit)}
              width={64}
              // Zero-based, so a flat series doesn't look like a trend;
              // ratios capped at 100%.
              domain={unit === 'ratio' ? [0, 1] : [0, 'auto']}
            />
            <Tooltip
              labelFormatter={v => formatTime(v as number)}
              formatter={v => formatMetricValue(v as number, unit)}
              contentStyle={{
                background: 'var(--panel)',
                border: '1px solid var(--border)',
                borderRadius: 6,
                fontSize: 13,
              }}
            />
            {testids.length > 1 && <Legend wrapperStyle={{ fontSize: 12 }} />}
            {testids.map((testid, i) => (
              <Line
                key={testid}
                type="monotone"
                dataKey={testid}
                name={testid}
                stroke={colorFor(i)}
                strokeWidth={2}
                dot={false}
                isAnimationActive={false}
                connectNulls
              />
            ))}
          </LineChart>
        </ResponsiveContainer>
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
