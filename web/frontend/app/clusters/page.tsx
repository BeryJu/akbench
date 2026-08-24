"use client";

import { api } from "@/lib/api";
import { ErrorBanner, useResource } from "@/components/ui";

export default function ClustersPage() {
  const { data, error } = useResource(() => api.clusters(), [], 15000);

  return (
    <>
      <h1>Clusters</h1>
      <p className="subtitle">
        Where topologies can be deployed. Provisioning workload clusters through Cluster
        API is not implemented yet, so this is the management cluster until it is.
      </p>

      <ErrorBanner error={error} />

      <table>
        <thead>
          <tr>
            <th>Name</th>
            <th>Role</th>
            <th>Phase</th>
            <th>Control plane</th>
            <th>Infrastructure</th>
          </tr>
        </thead>
        <tbody>
          {(data ?? []).map((cluster) => (
            <tr key={cluster.name}>
              <td className="mono">{cluster.name}</td>
              <td>{cluster.management ? "Management" : "Workload"}</td>
              <td>{cluster.phase ?? "-"}</td>
              <td>
                <span className={`badge ${cluster.controlPlaneReady ? "ok" : "warn"}`}>
                  {cluster.controlPlaneReady ? "Ready" : "Not ready"}
                </span>
              </td>
              <td>
                <span className={`badge ${cluster.infrastructureReady ? "ok" : "warn"}`}>
                  {cluster.infrastructureReady ? "Ready" : "Not ready"}
                </span>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </>
  );
}
