"use client";

import Link from "next/link";
import { use } from "react";
import { Content, Title } from "@patternfly/react-core";
import { api } from "@/lib/api";
import { ClusterForm } from "@/components/cluster-form";
import { ErrorBanner, useResource } from "@/components/ui";

export default function ClusterPage({ params }: { params: Promise<{ name: string }> }) {
  const { name } = use(params);
  const { data, error } = useResource(() => api.workloadCluster(name), [name]);

  return (
    <>
      <Title headingLevel="h1">{name}</Title>
      <Content component="p">
        Only worker pools (replicas, flavor, adding/removing a pool) are editable here - the
        control plane and Incus/LXC-wide settings are set once at registration.
      </Content>

      <ErrorBanner error={error} />

      {data ? <ClusterForm existing={data} /> : <p>Loading...</p>}

      <Content component="small">
        <Link href="/clusters">Back to clusters</Link>
      </Content>
    </>
  );
}
