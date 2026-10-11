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

/**
 * shortPodLabels maps each full pod name to a short, readable label for a
 * chart legend - a raw Kubernetes pod name like
 * "main-2026-10-10-py315-authentik-server-6fc6497b6-v8l5w" is unreadable
 * repeated across half a dozen legend entries (confirmed: that's exactly
 * what PodMetricsChart's first version showed). Two steps, both generic
 * (not specific to this project's own naming):
 *  1. Strip whatever dash-aligned prefix every pod in the set shares - for
 *     pods from the same deployment, that's the deployment name itself,
 *     already redundant with the chart's own title.
 *  2. Collapse a trailing Deployment-style "-<replicaset-hash>-<pod-hash>"
 *     down to just "-<pod-hash>" - the ReplicaSet hash segment changes on
 *     every rollout and carries no information a human reads meaning from,
 *     unlike StatefulSet pods ("postgresql-0"), which this leaves alone.
 */
export function shortPodLabels(pods: string[]): Record<string, string> {
  const prefix = commonDashPrefix(pods);
  const out: Record<string, string> = {};
  for (const pod of pods) {
    const stripped = prefix && pod.startsWith(prefix) ? pod.slice(prefix.length) : pod;
    out[pod] = collapseReplicaSetHash(stripped);
  }
  return out;
}

function commonDashPrefix(names: string[]): string {
  if (names.length < 2) {
    return '';
  }
  let prefix = names[0];
  for (const name of names.slice(1)) {
    let i = 0;
    while (i < prefix.length && i < name.length && prefix[i] === name[i]) {
      i++;
    }
    prefix = prefix.slice(0, i);
  }
  // Trim back to the last '-' so a shared word doesn't get cut in half
  // (e.g. two deployments "foo" and "foobar" sharing namesake don't collide).
  const lastDash = prefix.lastIndexOf('-');
  return lastDash >= 0 ? prefix.slice(0, lastDash + 1) : '';
}

function collapseReplicaSetHash(name: string): string {
  const match = /^(.*)-[0-9a-z]{8,10}-([0-9a-z]{5})$/.exec(name);
  return match ? `${match[1]}-${match[2]}` : name;
}

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
