"use client";

import { useSearchParams } from "next/navigation";
import { Suspense, useEffect, useMemo, useState } from "react";
import { api, ApiError } from "@/lib/api";
import type { CompareResponse } from "@/lib/types";
import { Empty, ErrorBanner, useResource } from "@/components/ui";

const windows = ["1m", "5m", "15m", "1h"];
const ranges = ["now-15m", "now-1h", "now-6h", "now-24h"];

function formatValue(value: number | undefined, unit: string): string {
  if (value === undefined) {
    return "-";
  }
  if (unit === "ratio") {
    return `${(value * 100).toFixed(1)}%`;
  }
  if (unit === "s") {
    return value < 1 ? `${(value * 1000).toFixed(0)} ms` : `${value.toFixed(2)} s`;
  }
  return value >= 100 ? value.toFixed(0) : value.toFixed(2);
}

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
  const [range, setRange] = useState("now-1h");
  const [embed, setEmbed] = useState<string>();
  const [embedError, setEmbedError] = useState<string>();

  const comparison = useResource(
    () =>
      selected.length
        ? api.compare(selected, window)
        : Promise.resolve(undefined as unknown as CompareResponse),
    [selected.join(","), window],
    10000,
  );

  useEffect(() => {
    if (!selected.length) {
      setEmbed(undefined);
      return;
    }
    let cancelled = false;
    api
      .embedURL("compare-tests", selected.map((id) => `testid:${id}`), range)
      .then((result) => {
        if (!cancelled) {
          setEmbed(result.url);
          setEmbedError(undefined);
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setEmbed(undefined);
          setEmbedError(err instanceof ApiError ? err.message : String(err));
        }
      });
    return () => {
      cancelled = true;
    };
  }, [selected.join(","), range]);

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
        the dashboard below plots them over time.
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
            <span>Dashboard range</span>
            <select name="range" value={range} onChange={(e) => setRange(e.target.value)}>
              {ranges.map((option) => (
                <option key={option}>{option}</option>
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
                        {formatValue(result.values[metric.key], metric.unit)}
                        {winner === result.testid ? " ✓" : ""}
                      </td>
                    ))}
                  </tr>
                );
              })}
            </tbody>
          </table>

          <h2>Dashboard</h2>
          <ErrorBanner error={embedError} />
          {embed ? (
            <iframe src={embed} title="Compare tests dashboard" />
          ) : (
            <Empty>No Grafana configured for this backend.</Empty>
          )}
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
