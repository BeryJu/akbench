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

// Ported from the deleted web/frontend/components/metrics-chart.tsx, on
// Headlamp's own SectionBox/EmptyContent instead of PatternFly's Card/Empty.
import { EmptyContent, SectionBox } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import { Alert, useTheme } from '@mui/material';
import { useMemo } from 'react';
import {
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';
import { api } from './api';
import { colorFor, formatMetricValue } from './format';
import { RangeResponse } from './types';
import { useResource } from './useResource';

function formatTime(seconds: number): string {
  return new Date(seconds * 1000).toLocaleTimeString([], {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}

/**
 * MetricsChart plots one metric over time, one line per test. It's the only
 * place a Burn TestCase's metrics can be seen at all - Burn has no TestRun
 * history to read from (it runs as a self-healing Deployment, not a
 * one-shot Job), so a live/rolling window over Prometheus is the only
 * record of what it's doing.
 */
export function MetricsChart({
  metric,
  testids,
  rangeSeconds = 900,
  step = '15s',
  refreshMs = 15000,
  height = 220,
}: {
  metric: string;
  testids: string[];
  rangeSeconds?: number;
  step?: string;
  refreshMs?: number;
  height?: number;
}) {
  const theme = useTheme();
  const { data, error } = useResource<RangeResponse | undefined>(
    () => {
      if (testids.length === 0) {
        return Promise.resolve(undefined);
      }
      const end = Math.floor(Date.now() / 1000);
      return api.metricsRange(testids, metric, end - rangeSeconds, end, step);
    },
    [testids.join(','), metric, rangeSeconds, step],
    refreshMs
  );

  const { rows, unit, label } = useMemo(() => {
    if (!data) {
      return { rows: [] as Record<string, number>[], unit: '', label: metric };
    }
    const byTime = new Map<number, Record<string, number>>();
    for (const series of data.series) {
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
    <SectionBox title={label}>
      {error && <Alert severity="error">{error}</Alert>}
      {rows.length === 0 ? (
        <EmptyContent>No data in this window yet.</EmptyContent>
      ) : (
        <ResponsiveContainer width="100%" height={height}>
          <LineChart data={rows} margin={{ top: 4, right: 12, left: 0, bottom: 0 }}>
            <CartesianGrid strokeDasharray="3 3" stroke={theme.palette.divider} />
            <XAxis
              dataKey="time"
              tickFormatter={value => formatTime(value as number)}
              stroke={theme.palette.text.secondary}
              tick={{ fontSize: 11 }}
              minTickGap={40}
            />
            <YAxis
              stroke={theme.palette.text.secondary}
              tick={{ fontSize: 11 }}
              tickFormatter={value => formatMetricValue(value as number, unit)}
              width={64}
              // Zero-based: a rate/duration/count chart that doesn't start
              // at 0 exaggerates its own trend. Ratios are additionally
              // capped at 1 (100%) rather than left to auto-scale off a
              // flat-zero series.
              domain={unit === 'ratio' ? [0, 1] : [0, 'auto']}
            />
            <Tooltip
              labelFormatter={value => formatTime(value as number)}
              formatter={value => formatMetricValue(value as number, unit)}
              contentStyle={{
                background: theme.palette.background.paper,
                border: `1px solid ${theme.palette.divider}`,
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
    </SectionBox>
  );
}
