"use client";

import Link from "next/link";
import { use } from "react";
import { Content, Title } from "@patternfly/react-core";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import { api } from "@/lib/api";
import { TopologyForm } from "@/components/topology-form";
import { ErrorBanner, ReadyBadge, useResource } from "@/components/ui";

export default function TopologyPage({ params }: { params: Promise<{ name: string }> }) {
  const { name } = use(params);
  const { data, error } = useResource(() => api.topology(name), [name]);

  return (
    <>
      <Title headingLevel="h1">{name}</Title>
      <Content component="p">
        <ReadyBadge conditions={data?.status?.conditions} />{" "}
        {data?.status?.serviceEndpoint ? (
          <code>{data.status.serviceEndpoint}</code>
        ) : (
          <Content component="small">no service endpoint yet</Content>
        )}
      </Content>

      <ErrorBanner error={error} />

      {data?.status?.conditions?.length ? (
        <>
          <Title headingLevel="h2">Conditions</Title>
          <Table role="table">
            <Thead>
              <Tr>
                <Th>Type</Th>
                <Th>Status</Th>
                <Th>Reason</Th>
                <Th>Message</Th>
              </Tr>
            </Thead>
            <Tbody>
              {data.status.conditions.map((condition) => (
                <Tr key={condition.type}>
                  <Td>{condition.type}</Td>
                  <Td>{condition.status}</Td>
                  <Td>{condition.reason}</Td>
                  <Td>
                    <Content component="small">{condition.message}</Content>
                  </Td>
                </Tr>
              ))}
            </Tbody>
          </Table>
        </>
      ) : null}

      <Title headingLevel="h2">Specification</Title>
      {data ? <TopologyForm existing={data} /> : <p>Loading...</p>}

      <Content component="small">
        <Link href="/topologies">Back to topologies</Link>
      </Content>
    </>
  );
}
