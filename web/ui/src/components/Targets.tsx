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

// One row per deployment a TestCase drives ("target"), side by side - the
// core view of a multi-deployment Burn test: same script, same load, so any
// difference between rows is the deployments' own.
import { ReactNode } from 'react';
import { Link } from 'react-router';
import { colorFor, formatDuration, formatMetricValue, relativeDelta, targetNameOf } from '../format';
import {
  AuthentikDeployment,
  CompareMetric,
  CompareResponse,
  RangePoint,
  readyCondition,
  TestCase,
  testCaseDeployments,
} from '../types';
import { Badge } from './Status';

export interface TargetRow {
  /** Position in the TestCase's own deployment list - also the series
   * index every chart of that TestCase colors this target by. */
  index: number;
  deploymentName: string;
  /** The k6 `target` tag this deployment's metrics carry. */
  target: string;
  deployment?: AuthentikDeployment;
  /** From TestCase status.deployments - undefined until first reconciled. */
  ready?: boolean;
  burnWorkload?: string;
  values: Record<string, number>;
}

/** buildTargetRows joins a TestCase's deployments with their compare rows
 * (matched on the k6 `target` tag) and their own reconcile status, in the
 * TestCase's own deployment order. */
export function buildTargetRows(
  testCase: TestCase,
  results: CompareResponse['results'],
  deployments: AuthentikDeployment[]
): TargetRow[] {
  return testCaseDeployments(testCase.spec).map((ref, index) => {
    const deployment = deployments.find(d => d.metadata.name === ref.name);
    const target = deployment ? targetNameOf(deployment) : ref.name;
    const status = testCase.status?.deployments?.find(d => d.name === ref.name);
    return {
      index,
      deploymentName: ref.name,
      target,
      deployment,
      ready: status?.ready,
      burnWorkload: status?.burnWorkloadRef?.name,
      values: results.find(r => r.testid === testCase.metadata.name && r.target === target)?.values ?? {},
    };
  });
}

/** burningFor is how long a Burn test has been Ready - its Ready
 * condition's last transition, so a restart or a target change resets it. */
export function burningFor(tc: TestCase, now = Date.now()): string | undefined {
  const ready = readyCondition(tc.status?.conditions);
  if (ready?.status !== 'True' || !ready.lastTransitionTime) {
    return undefined;
  }
  return formatDuration((now - Date.parse(ready.lastTransitionTime)) / 1000);
}

/** targetLetter is a short, stable handle for a target (A, B, C...) - the
 * same letter and color on the table and the legend of every chart. */
export function targetLetter(index: number): string {
  return index < 26 ? String.fromCharCode(65 + index) : String(index + 1);
}

export function TargetKey({ index }: { index: number }) {
  return (
    <span className="key" style={{ background: colorFor(index) }} aria-hidden="true">
      {targetLetter(index)}
    </span>
  );
}

export const TARGET_METRICS = ['iterations', 'iteration_duration_p95', 'http_req_duration_p99', 'http_req_failed', 'checks'];
const DELTA_METRIC = 'iteration_duration_p95';

export function TargetTable({
  rows,
  metrics,
  metricKeys = TARGET_METRICS,
  baseline,
  onBaseline,
  sparklines,
  actions,
}: {
  rows: TargetRow[];
  metrics: CompareMetric[];
  metricKeys?: string[];
  /** Target the Δ column is relative to; a Δ column only shows with onBaseline. */
  baseline?: string;
  onBaseline?: (target: string) => void;
  /** p95 trend per target, keyed by target. */
  sparklines?: Map<string, RangePoint[]>;
  actions?: (row: TargetRow) => ReactNode;
}) {
  const columns = metricKeys
    .map(key => metrics.find(m => m.key === key))
    .filter((m): m is CompareMetric => m !== undefined);
  const deltaMetric = onBaseline && rows.length > 1 ? metrics.find(m => m.key === DELTA_METRIC) : undefined;
  const baseRow = rows.find(r => r.target === baseline) ?? rows[0];

  return (
    <div className="table-scroll">
      <table className="table">
        <thead>
          <tr>
            <th>Target</th>
            <th>authentik</th>
            {columns.map(m => (
              <th key={m.key} className="num">
                {m.label}
              </th>
            ))}
            {deltaMetric && <th className="num">Δ {deltaMetric.label}</th>}
            {sparklines && <th>p95 trend</th>}
            <th>Runner</th>
            {(actions || deltaMetric) && <th />}
          </tr>
        </thead>
        <tbody>
          {rows.map(row => (
            <tr key={row.deploymentName} className={row.ready === false ? 'attention' : undefined}>
              <td>
                <span className="row" style={{ flexWrap: 'nowrap' }}>
                  <TargetKey index={row.index} />
                  <Link to={`/deployments/${row.deploymentName}`}>{row.deploymentName}</Link>
                </span>
              </td>
              <td className="mono">{row.deployment?.spec.authentik?.tag ?? <span className="muted">chart default</span>}</td>
              {columns.map(m => (
                <td key={m.key} className="mono num">
                  {formatMetricValue(row.values[m.key], m.unit)}
                </td>
              ))}
              {deltaMetric && (
                <td className="mono num">
                  {row === baseRow ? (
                    <span className="muted">baseline</span>
                  ) : (
                    <Delta
                      value={relativeDelta(row.values[deltaMetric.key], baseRow.values[deltaMetric.key])}
                      better={deltaMetric.better}
                    />
                  )}
                </td>
              )}
              {sparklines && (
                <td>
                  <Sparkline points={sparklines.get(row.target) ?? []} color={colorFor(row.index)} />
                </td>
              )}
              <td>{row.ready === undefined ? <span className="muted">-</span> : <Badge value={row.ready ? 'Ready' : 'Not ready'} />}</td>
              {(actions || deltaMetric) && (
                <td className="actions">
                  <span className="row" style={{ justifyContent: 'flex-end', flexWrap: 'nowrap' }}>
                    {deltaMetric && row !== baseRow && (
                      <button type="button" className="small" onClick={() => onBaseline?.(row.target)}>
                        Use as baseline
                      </button>
                    )}
                    {actions?.(row)}
                  </span>
                </td>
              )}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** Delta renders a relative change, colored by whether it's an improvement
 * for a metric where `better` is the good direction. */
export function Delta({ value, better }: { value: number | undefined; better: 'higher' | 'lower' }) {
  if (value === undefined) {
    return <span className="muted">-</span>;
  }
  const improved = better === 'lower' ? value < 0 : value > 0;
  const arrow = value > 0 ? '▲' : value < 0 ? '▼' : '';
  return (
    <span className={Math.abs(value) < 0.005 ? 'muted' : improved ? 'delta better' : 'delta worse'}>
      {arrow} {Math.abs(value * 100).toFixed(1)}%
    </span>
  );
}

/** Sparkline is a tiny unlabeled trend line - the shape only, scaled from
 * zero to the series' own maximum. */
export function Sparkline({ points, color, width = 120, height = 28 }: { points: RangePoint[]; color: string; width?: number; height?: number }) {
  if (points.length < 2) {
    return <span className="muted">-</span>;
  }
  const first = points[0].time;
  const span = points[points.length - 1].time - first || 1;
  const max = Math.max(...points.map(p => p.value)) || 1;
  const path = points
    .map(p => `${(((p.time - first) / span) * width).toFixed(1)},${(height - 2 - (p.value / max) * (height - 4)).toFixed(1)}`)
    .join(' ');
  return (
    <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} aria-hidden="true" className="sparkline">
      <polyline fill="none" stroke={color} strokeWidth={1.6} points={path} />
    </svg>
  );
}
