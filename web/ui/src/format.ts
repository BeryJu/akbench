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

/** formatMetricValue renders a CompareMetric value the same way in the
 * compare table and every chart tooltip/axis, so a number means the same
 * thing wherever it appears. */
export function formatMetricValue(value: number | undefined, unit: string): string {
  if (value === undefined) {
    return '-';
  }
  if (unit === 'ratio') {
    return `${(value * 100).toFixed(1)}%`;
  }
  if (unit === 's') {
    return value < 1 ? `${(value * 1000).toFixed(0)} ms` : `${value.toFixed(2)} s`;
  }
  if (unit === 'cores') {
    return value < 1 ? `${Math.round(value * 1000)}m` : `${value.toFixed(2)} cores`;
  }
  if (unit === 'bytes') {
    return formatBytes(value);
  }
  return value >= 100 ? value.toFixed(0) : value.toFixed(2);
}

const BYTE_UNITS = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];

function formatBytes(value: number): string {
  let scaled = value;
  let unitIndex = 0;
  while (Math.abs(scaled) >= 1024 && unitIndex < BYTE_UNITS.length - 1) {
    scaled /= 1024;
    unitIndex++;
  }
  return `${scaled.toFixed(unitIndex === 0 ? 0 : 1)} ${BYTE_UNITS[unitIndex]}`;
}

// A small qualitative palette, cycled by index so the same test keeps the
// same color across every chart on a page.
const palette = [
  '#0066cc',
  '#4cb140',
  '#f0ab00',
  '#c9190b',
  '#8476d1',
  '#009596',
  '#ec7a08',
  '#6ca100',
];

export function colorFor(index: number): string {
  return palette[index % palette.length];
}

/**
 * metricsAnchorFromRuns resolves a fixed-in-the-past metrics window from a
 * Quick TestCase's own run history, instead of the usual "trailing window
 * ending at now" - a Quick TestCase's k6 Job exits once its run completes,
 * so a window anchored to "now" only ever shows data for as long as the
 * run's own samples stay within that trailing window (Prometheus' own
 * ~5-minute staleness lookback for an instant query, or metricsRange's own
 * rangeSeconds), then silently goes empty even though the run itself
 * succeeded and its data is still in Prometheus.
 *
 * Returns undefined (meaning "use now", MetricsChart's own default) when the
 * latest run is still in flight (no completionTime yet) or there's no run
 * history at all - both cases where "now" is already the right anchor.
 */
export function metricsAnchorFromRuns(
  runs: { status?: { startTime?: string; completionTime?: string } }[]
): { endSeconds: number; rangeSeconds: number } | undefined {
  const completed = runs
    .filter(r => r.status?.completionTime)
    .sort((a, b) => Date.parse(b.status!.completionTime!) - Date.parse(a.status!.completionTime!));
  const latest = completed[0];
  if (!latest?.status?.completionTime) {
    return undefined;
  }
  // A short buffer past completion, so the run's own last few samples
  // (k6's final Prometheus remote-write push can trail completion slightly)
  // are still inside the window.
  const endSeconds = Math.floor(Date.parse(latest.status.completionTime) / 1000) + 60;
  const startSeconds = latest.status.startTime
    ? Math.floor(Date.parse(latest.status.startTime) / 1000)
    : endSeconds - 900;
  const rangeSeconds = Math.max(900, endSeconds - startSeconds + 120);
  return { endSeconds, rangeSeconds };
}
