"use client";

import Link from "next/link";
import { use } from "react";
import { api } from "@/lib/api";
import { TopologyForm } from "@/components/topology-form";
import { ErrorBanner, ReadyBadge, useResource } from "@/components/ui";

export default function TopologyPage({ params }: { params: Promise<{ name: string }> }) {
  const { name } = use(params);
  const { data, error } = useResource(() => api.topology(name), [name]);

  return (
    <>
      <h1>{name}</h1>
      <p className="subtitle">
        <ReadyBadge conditions={data?.status?.conditions} />{" "}
        {data?.status?.serviceEndpoint ? (
          <span className="mono">{data.status.serviceEndpoint}</span>
        ) : (
          <span className="muted">no service endpoint yet</span>
        )}
      </p>

      <ErrorBanner error={error} />

      {data?.status?.conditions?.length ? (
        <>
          <h2>Conditions</h2>
          <table>
            <thead>
              <tr>
                <th>Type</th>
                <th>Status</th>
                <th>Reason</th>
                <th>Message</th>
              </tr>
            </thead>
            <tbody>
              {data.status.conditions.map((condition) => (
                <tr key={condition.type}>
                  <td>{condition.type}</td>
                  <td>{condition.status}</td>
                  <td>{condition.reason}</td>
                  <td className="muted">{condition.message}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      ) : null}

      <h2>Specification</h2>
      {data ? <TopologyForm existing={data} /> : <p className="muted">Loading...</p>}

      <p className="muted">
        <Link href="/topologies">Back to topologies</Link>
      </p>
    </>
  );
}
