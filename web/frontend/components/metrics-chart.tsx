"use client";

import { useMemo } from "react";
import {
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { api } from "@/lib/api";
import { colorFor, formatMetricValue } from "@/lib/format";
import type { RangeResponse } from "@/lib/types";
import { Empty, ErrorBanner, useResource } from "./ui";

function formatTime(seconds: number): string {
  return new Date(seconds * 1000).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

/**
 * MetricsChart plots one CompareMetric over time, one line per test. It is
 * the only place a Burn TestCase's metrics can be seen at all - Burn has no
 * TestRun history to read from, since it runs as a self-healing Deployment
 * rather than a one-shot Job, so a live/rolling window over Prometheus is
 * the only record of what it is doing.
 */
export function MetricsChart({
  metric,
  testids,
  rangeSeconds = 900,
  step = "15s",
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
  const { data, error } = useResource<RangeResponse | undefined>(
    () => {
      if (testids.length === 0) {
        return Promise.resolve(undefined);
      }
      const end = Math.floor(Date.now() / 1000);
      return api.metricsRange(testids, metric, end - rangeSeconds, end, step);
    },
    [testids.join(","), metric, rangeSeconds, step],
    refreshMs,
  );

  const { rows, unit, label } = useMemo(() => {
    if (!data) {
      return { rows: [] as Record<string, number>[], unit: "", label: metric };
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
    <div className="chart">
      <h3>{label}</h3>
      <ErrorBanner error={error} />
      {rows.length === 0 ? (
        <Empty>No data in this window yet.</Empty>
      ) : (
        <ResponsiveContainer width="100%" height={height}>
          <LineChart data={rows} margin={{ top: 4, right: 12, left: 0, bottom: 0 }}>
            <CartesianGrid strokeDasharray="3 3" stroke="var(--border)" />
            <XAxis
              dataKey="time"
              tickFormatter={(value) => formatTime(value as number)}
              stroke="var(--text-dim)"
              tick={{ fontSize: 11 }}
              minTickGap={40}
            />
            <YAxis
              stroke="var(--text-dim)"
              tick={{ fontSize: 11 }}
              tickFormatter={(value) => formatMetricValue(value as number, unit)}
              width={64}
              // Zero-based: a rate/duration/count chart that doesn't start at
              // 0 exaggerates its own trend. Ratios are additionally capped
              // at 1 (100%) rather than left to auto-scale off a flat-zero
              // series, which recharts otherwise pads to an arbitrary range.
              domain={unit === "ratio" ? [0, 1] : [0, "auto"]}
            />
            <Tooltip
              labelFormatter={(value) => formatTime(value as number)}
              formatter={(value) => formatMetricValue(value as number, unit)}
              contentStyle={{
                background: "var(--bg-raised)",
                border: "1px solid var(--border)",
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
