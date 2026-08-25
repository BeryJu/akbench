"use client";

import { useSearchParams } from "next/navigation";
import { Suspense, useMemo, useState } from "react";
import { api } from "@/lib/api";
import { formatMetricValue } from "@/lib/format";
import type { CompareResponse } from "@/lib/types";
import { Empty, ErrorBanner, useResource } from "@/components/ui";
import { MetricsChart } from "@/components/metrics-chart";

const windows = ["1m", "5m", "15m", "1h"];
const ranges: { label: string; seconds: number }[] = [
  { label: "Last 15m", seconds: 15 * 60 },
  { label: "Last 1h", seconds: 60 * 60 },
  { label: "Last 6h", seconds: 6 * 60 * 60 },
  { label: "Last 24h", seconds: 24 * 60 * 60 },
];

/** best picks the winning test per metric, honouring the metric's direction. */
function best(response: CompareResponse, key: string, better: string): string | undefined {
  const scored = response.results
    .filter((result) => result.values[key] !== undefined)
    .map((result) => ({ testid: result.testid, value: result.values[key] as number }));
  if (scored.length < 2) {
    return undefined;
  }
  return scored.reduce((a, b) => ((better === "lower" ? b.value < a.value : b.value > a.value) ? b : a))
    .testid;
}

function Compare() {
  const search = useSearchParams();
  const testCases = useResource(() => api.testCases(), [], 0);
  const [selected, setSelected] = useState<string[]>(
    search.get("testids")?.split(",").filter(Boolean) ?? [],
  );
  const [window, setWindow] = useState("5m");
  const [range, setRange] = useState(ranges[1]);

  const comparison = useResource(
    () =>
      selected.length
        ? api.compare(selected, window)
        : Promise.resolve(undefined as unknown as CompareResponse),
    [selected.join(","), window],
    10000,
  );

  const rows = useMemo(() => comparison.data?.metrics ?? [], [comparison.data]);

  function toggle(name: string) {
    setSelected((current) =>
      current.includes(name) ? current.filter((id) => id !== name) : [...current, name],
    );
  }

  return (
    <>
      <h1>Compare</h1>
      <p className="subtitle">
        Pick the tests to put side by side. The table summarises the last {window} of each;
        the charts below plot them over time.
      </p>

      <ErrorBanner error={comparison.error ?? testCases.error} />

      <fieldset>
        <legend>Tests</legend>
        <div className="row">
          {(testCases.data ?? []).map((testCase) => (
            <label key={testCase.metadata.name} style={{ marginBottom: 8, width: "auto" }}>
              <input
                type="checkbox"
                name={`testid-${testCase.metadata.name}`}
                style={{ width: "auto", marginRight: 6 }}
                checked={selected.includes(testCase.metadata.name)}
                onChange={() => toggle(testCase.metadata.name)}
              />
              {testCase.metadata.name}
            </label>
          ))}
          {testCases.data?.length === 0 && <span className="muted">No test cases yet.</span>}
        </div>
        <div className="row">
          <label style={{ width: "auto" }}>
            <span>Summary window</span>
            <select name="window" value={window} onChange={(e) => setWindow(e.target.value)}>
              {windows.map((option) => (
                <option key={option}>{option}</option>
              ))}
            </select>
          </label>
          <label style={{ width: "auto" }}>
            <span>Chart range</span>
            <select
              name="range"
              value={range.label}
              onChange={(e) => setRange(ranges.find((r) => r.label === e.target.value) ?? ranges[1])}
            >
              {ranges.map((option) => (
                <option key={option.label}>{option.label}</option>
              ))}
            </select>
          </label>
        </div>
      </fieldset>

      {selected.length === 0 ? (
        <Empty>Select at least one test.</Empty>
      ) : (
        <>
          <h2>Summary</h2>
          <table>
            <thead>
              <tr>
                <th>Metric</th>
                {(comparison.data?.results ?? []).map((result) => (
                  <th key={result.testid}>{result.testid}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((metric) => {
                const winner = comparison.data ? best(comparison.data, metric.key, metric.better) : undefined;
                return (
                  <tr key={metric.key}>
                    <td>
                      {metric.label}{" "}
                      <span className="muted">({metric.better} is better)</span>
                    </td>
                    {(comparison.data?.results ?? []).map((result) => (
                      <td key={result.testid} className="mono">
                        {formatMetricValue(result.values[metric.key], metric.unit)}
                        {winner === result.testid ? " ✓" : ""}
                      </td>
                    ))}
                  </tr>
                );
              })}
            </tbody>
          </table>

          <h2>Over time</h2>
          <div className="charts">
            {rows.map((metric) => (
              <MetricsChart
                key={metric.key}
                metric={metric.key}
                testids={selected}
                rangeSeconds={range.seconds}
              />
            ))}
          </div>
        </>
      )}
    </>
  );
}

export default function ComparePage() {
  return (
    <Suspense fallback={<p className="muted">Loading...</p>}>
      <Compare />
    </Suspense>
  );
}
