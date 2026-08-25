"use client";

import Link from "next/link";
import { api } from "@/lib/api";
import { ConfirmButton, ErrorBanner, useResource } from "@/components/ui";

export default function ClustersPage() {
  const { data, error, reload } = useResource(() => api.clusters(), [], 15000);

  return (
    <>
      <div className="spread">
        <div>
          <h1>Clusters</h1>
          <p className="subtitle">
            Where topologies can be deployed. Workload clusters are provisioned via Cluster
            API - registering one here creates a <span className="mono">WorkloadCluster</span>{" "}
            the operator provisions from.
          </p>
        </div>
        <Link className="button primary" href="/clusters/new">
          Register cluster
        </Link>
      </div>

      <ErrorBanner error={error} />

      <table>
        <thead>
          <tr>
            <th>Name</th>
            <th>Role</th>
            <th>Phase</th>
            <th>Control plane</th>
            <th>Infrastructure</th>
            <th />
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
              <td>
                {!cluster.management && (
                  <ConfirmButton
                    onConfirm={async () => {
                      await api.deprovisionCluster(cluster.name);
                      reload();
                    }}
                  >
                    Deprovision
                  </ConfirmButton>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </>
  );
}
