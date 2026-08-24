"use client";

import Link from "next/link";
import { api } from "@/lib/api";
import { Age, Empty, ErrorBanner, PhaseBadge, ReadyBadge, useResource } from "@/components/ui";

export default function Home() {
  const topologies = useResource(() => api.topologies());
  const testCases = useResource(() => api.testCases());
  const runs = useResource(() => api.testRuns());
  const clusters = useResource(() => api.clusters(), [], 30000);

  const burning = (testCases.data ?? []).filter((tc) => tc.spec.profile === "Burn");
  const recent = [...(runs.data ?? [])]
    .sort((a, b) => (b.metadata.creationTimestamp ?? "").localeCompare(a.metadata.creationTimestamp ?? ""))
    .slice(0, 8);

  return (
    <>
      <h1>Overview</h1>
      <p className="subtitle">authentik load-test topologies, test cases and runs.</p>

      <ErrorBanner error={topologies.error ?? testCases.error ?? runs.error ?? clusters.error} />

      <div className="cards">
        <div className="card">
          <div className="metric">{clusters.data?.length ?? "-"}</div>
          <div className="muted">Clusters</div>
        </div>
        <div className="card">
          <div className="metric">{topologies.data?.length ?? "-"}</div>
          <div className="muted">Topologies</div>
        </div>
        <div className="card">
          <div className="metric">{testCases.data?.length ?? "-"}</div>
          <div className="muted">Test cases</div>
        </div>
        <div className="card">
          <div className="metric">{burning.length}</div>
          <div className="muted">Burn tests running</div>
        </div>
      </div>

      <h2>Topologies</h2>
      {topologies.data?.length ? (
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Target</th>
              <th>Postgres</th>
              <th>Placement</th>
              <th>Status</th>
            </tr>
          </thead>
          <tbody>
            {topologies.data.map((topology) => (
              <tr key={topology.metadata.name}>
                <td>
                  <Link href={`/topologies/${topology.metadata.name}`}>{topology.metadata.name}</Link>
                </td>
                <td className="mono">{topology.spec.targetName ?? topology.metadata.name}</td>
                <td>{topology.spec.postgres.mode}</td>
                <td>{topology.spec.placement?.mode ?? "Default"}</td>
                <td>
                  <ReadyBadge conditions={topology.status?.conditions} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <Empty>
          No topologies yet. <Link href="/topologies/new">Define one</Link> to get an authentik
          install to benchmark.
        </Empty>
      )}

      <h2>Recent runs</h2>
      {recent.length ? (
        <table>
          <thead>
            <tr>
              <th>Run</th>
              <th>Test case</th>
              <th>Phase</th>
              <th>Started</th>
            </tr>
          </thead>
          <tbody>
            {recent.map((run) => (
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
                <td>
                  <Age time={run.status?.startTime ?? run.metadata.creationTimestamp} /> ago
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <Empty>No runs yet.</Empty>
      )}
    </>
  );
}
