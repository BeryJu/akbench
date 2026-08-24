"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { api, ApiError } from "@/lib/api";
import { Age, ConfirmButton, Empty, ErrorBanner, ReadyBadge, useResource } from "@/components/ui";

export default function TestCasesPage() {
  const router = useRouter();
  const { data, error, reload } = useResource(() => api.testCases());
  const [actionError, setActionError] = useState<string>();

  async function run(name: string) {
    setActionError(undefined);
    try {
      const created = await api.runTestCase(name);
      router.push(`/testruns?testcase=${name}`);
      return created;
    } catch (err) {
      setActionError(err instanceof ApiError ? err.message : String(err));
    }
  }

  return (
    <>
      <div className="spread">
        <div>
          <h1>Test cases</h1>
          <p className="subtitle">
            A Quick test case is executed by starting a run; a Burn test case runs
            continuously for as long as it exists.
          </p>
        </div>
        <Link className="button primary" href="/testcases/new">
          New test case
        </Link>
      </div>

      <ErrorBanner error={actionError ?? error} />

      {data?.length ? (
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Topology</th>
              <th>Profile</th>
              <th>VUs</th>
              <th>Iterations</th>
              <th>Status</th>
              <th>Age</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {data.map((testCase) => (
              <tr key={testCase.metadata.name}>
                <td>
                  <Link href={`/testcases/${testCase.metadata.name}`}>
                    {testCase.metadata.name}
                  </Link>
                </td>
                <td>
                  <Link href={`/topologies/${testCase.spec.topologyRef.name}`}>
                    {testCase.spec.topologyRef.name}
                  </Link>
                </td>
                <td>{testCase.spec.profile}</td>
                <td>{testCase.spec.vus}</td>
                <td>{testCase.spec.profile === "Quick" ? testCase.spec.iterations : "-"}</td>
                <td>
                  <ReadyBadge conditions={testCase.status?.conditions} />
                </td>
                <td>
                  <Age time={testCase.metadata.creationTimestamp} />
                </td>
                <td>
                  <div className="row">
                    {testCase.spec.profile === "Quick" ? (
                      <button
                        type="button"
                        name={`run-${testCase.metadata.name}`}
                        onClick={() => void run(testCase.metadata.name)}
                      >
                        Run now
                      </button>
                    ) : (
                      <span className="badge warn">Burning</span>
                    )}
                    <ConfirmButton
                      onConfirm={async () => {
                        await api.deleteTestCase(testCase.metadata.name);
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
        <Empty>
          No test cases yet. <Link href="/testcases/new">Define one</Link>.
        </Empty>
      )}
    </>
  );
}
