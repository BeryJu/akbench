"use client";

import Link from "next/link";
import { use, useState } from "react";
import { api } from "@/lib/api";
import { TestCaseForm } from "@/components/testcase-form";
import { MetricsChart } from "@/components/metrics-chart";
import { Age, Empty, ErrorBanner, PhaseBadge, ReadyBadge, useResource } from "@/components/ui";

const ranges: { label: string; seconds: number }[] = [
  { label: "Last 15m", seconds: 15 * 60 },
  { label: "Last 1h", seconds: 60 * 60 },
  { label: "Last 6h", seconds: 6 * 60 * 60 },
  { label: "Last 24h", seconds: 24 * 60 * 60 },
];

export default function TestCasePage({ params }: { params: Promise<{ name: string }> }) {
  const { name } = use(params);
  const testCase = useResource(() => api.testCase(name), [name]);
  const runs = useResource(() => api.testRuns(name), [name]);
  // Metrics don't come from testCase itself, only the catalog of what
  // metrics exist to chart - compare's response happens to carry that
  // catalog alongside its (unused here) per-test summary values.
  const catalog = useResource(() => api.compare([name], "5m"), [name], 60000);
  const [range, setRange] = useState(ranges[1]);

  return (
    <>
      <h1>{name}</h1>
      <p className="subtitle">
        <ReadyBadge conditions={testCase.data?.status?.conditions} />{" "}
        {testCase.data?.status?.burnWorkloadRef ? (
          <span className="mono">{testCase.data.status.burnWorkloadRef.name}</span>
        ) : null}
      </p>

      <ErrorBanner error={testCase.error ?? runs.error} />

      <h2>Metrics</h2>
      <ErrorBanner error={catalog.error} />
      <div className="row">
        <label style={{ width: "auto" }}>
          <span>Range</span>
          <select
            value={range.label}
            onChange={(e) => setRange(ranges.find((r) => r.label === e.target.value) ?? ranges[1])}
          >
            {ranges.map((option) => (
              <option key={option.label}>{option.label}</option>
            ))}
          </select>
        </label>
      </div>
      {catalog.data?.metrics.length ? (
        <div className="charts">
          {catalog.data.metrics.map((metric) => (
            <MetricsChart
              key={metric.key}
              metric={metric.key}
              testids={[name]}
              rangeSeconds={range.seconds}
            />
          ))}
        </div>
      ) : (
        !catalog.error && <Empty>No metrics available.</Empty>
      )}

      <h2>Runs</h2>
      {runs.data?.length ? (
        <table>
          <thead>
            <tr>
              <th>Run</th>
              <th>Phase</th>
              <th>VUs</th>
              <th>Started</th>
              <th>Finished</th>
            </tr>
          </thead>
          <tbody>
            {runs.data.map((run) => (
              <tr key={run.metadata.name}>
                <td className="mono">{run.metadata.name}</td>
                <td>
                  <PhaseBadge phase={run.status?.phase} />
                </td>
                <td>{run.status?.resolvedSpec?.vus ?? run.spec.overrides?.vus ?? "-"}</td>
                <td>
                  <Age time={run.status?.startTime} />
                </td>
                <td>
                  <Age time={run.status?.completionTime} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <Empty>No runs yet.</Empty>
      )}

      <h2>Specification</h2>
      {testCase.data ? <TestCaseForm existing={testCase.data} /> : <p className="muted">Loading...</p>}

      <p className="muted">
        <Link href="/testcases">Back to test cases</Link>
      </p>
    </>
  );
}
