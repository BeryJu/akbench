"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Suspense } from "react";
import { api } from "@/lib/api";
import { Age, ConfirmButton, Empty, ErrorBanner, PhaseBadge, useResource } from "@/components/ui";

function TestRuns() {
  const search = useSearchParams();
  const testCase = search.get("testcase") ?? undefined;
  const { data, error, reload } = useResource(() => api.testRuns(testCase), [testCase]);

  const runs = [...(data ?? [])].sort((a, b) =>
    (b.metadata.creationTimestamp ?? "").localeCompare(a.metadata.creationTimestamp ?? ""),
  );

  return (
    <>
      <h1>Test runs</h1>
      <p className="subtitle">
        {testCase ? (
          <>
            Runs of <span className="mono">{testCase}</span>.{" "}
            <Link href="/testruns">Show all</Link>
          </>
        ) : (
          "Every bounded execution, kept as its own record so a result stays readable after the test case it came from has moved on."
        )}
      </p>

      <ErrorBanner error={error} />

      {runs.length ? (
        <table>
          <thead>
            <tr>
              <th>Run</th>
              <th>Test case</th>
              <th>Phase</th>
              <th>VUs</th>
              <th>Iterations</th>
              <th>Started</th>
              <th>Finished</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {runs.map((run) => (
              <tr key={run.metadata.name}>
                <td className="mono">{run.metadata.name}</td>
                <td>
                  <Link href={`/testcases/${run.spec.testCaseRef.name}`}>
                    {run.spec.testCaseRef.name}
                  </Link>
                </td>
                <td>
                  <PhaseBadge phase={run.status?.phase} />
                </td>
                <td>{run.status?.resolvedSpec?.vus ?? run.spec.overrides?.vus ?? "-"}</td>
                <td>
                  {run.status?.resolvedSpec?.iterations ?? run.spec.overrides?.iterations ?? "-"}
                </td>
                <td>
                  <Age time={run.status?.startTime} />
                </td>
                <td>
                  <Age time={run.status?.completionTime} />
                </td>
                <td>
                  <div className="row">
                    <Link
                      className="button"
                      href={`/compare?testids=${run.spec.testCaseRef.name}`}
                    >
                      Metrics
                    </Link>
                    <ConfirmButton
                      onConfirm={async () => {
                        await api.deleteTestRun(run.metadata.name);
                        reload();
                      }}
                    >
                      Delete
                    </ConfirmButton>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <Empty>No runs yet. Start one from a Quick test case.</Empty>
      )}
    </>
  );
}

export default function TestRunsPage() {
  // useSearchParams needs a Suspense boundary, or the whole route opts out of
  // static rendering with a build-time error.
  return (
    <Suspense fallback={<p className="muted">Loading...</p>}>
      <TestRuns />
    </Suspense>
  );
}
