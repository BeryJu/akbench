"use client";

import Link from "next/link";
import { api } from "@/lib/api";
import { Age, ConfirmButton, Empty, ErrorBanner, ReadyBadge, useResource } from "@/components/ui";

export default function TopologiesPage() {
  const { data, error, reload } = useResource(() => api.topologies());

  return (
    <>
      <div className="spread">
        <div>
          <h1>Topologies</h1>
          <p className="subtitle">
            One authentik deployment shape each: replica counts, node placement and
            Postgres backend.
          </p>
        </div>
        <Link className="button primary" href="/topologies/new">
          New topology
        </Link>
      </div>

      <ErrorBanner error={error} />

      {data?.length ? (
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>Target</th>
              <th>Namespace</th>
              <th>Server</th>
              <th>Worker</th>
              <th>Postgres</th>
              <th>Placement</th>
              <th>Status</th>
              <th>Age</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {data.map((topology) => (
              <tr key={topology.metadata.name}>
                <td>
                  <Link href={`/topologies/${topology.metadata.name}`}>
                    {topology.metadata.name}
                  </Link>
                </td>
                <td className="mono">{topology.spec.targetName ?? topology.metadata.name}</td>
                <td className="mono">{topology.spec.namespace}</td>
                <td>{topology.spec.server.replicas}</td>
                <td>{topology.spec.worker.replicas}</td>
                <td>{topology.spec.postgres.mode}</td>
                <td>{topology.spec.placement?.mode ?? "Default"}</td>
                <td>
                  <ReadyBadge conditions={topology.status?.conditions} />
                </td>
                <td>
                  <Age time={topology.metadata.creationTimestamp} />
                </td>
                <td>
                  <ConfirmButton
                    onConfirm={async () => {
                      await api.deleteTopology(topology.metadata.name);
                      reload();
                    }}
                  >
                    Delete
                  </ConfirmButton>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <Empty>
          No topologies yet. <Link href="/topologies/new">Define one</Link>.
        </Empty>
      )}
    </>
  );
}
