"use client";

import Link from "next/link";
import { use } from "react";
import { api } from "@/lib/api";
import { TestCaseForm } from "@/components/testcase-form";
import { Age, Empty, ErrorBanner, PhaseBadge, ReadyBadge, useResource } from "@/components/ui";

export default function TestCasePage({ params }: { params: Promise<{ name: string }> }) {
  const { name } = use(params);
  const testCase = useResource(() => api.testCase(name), [name]);
  const runs = useResource(() => api.testRuns(name), [name]);

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
