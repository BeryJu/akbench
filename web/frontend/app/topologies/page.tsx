"use client";

import Link from "next/link";
import { Button, Content, Flex, FlexItem, Title } from "@patternfly/react-core";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import { api } from "@/lib/api";
import { Age, ConfirmButton, Empty, ErrorBanner, ReadyBadge, useResource } from "@/components/ui";

export default function TopologiesPage() {
  const { data, error, reload } = useResource(() => api.topologies());

  return (
    <>
      <Flex justifyContent={{ default: "justifyContentSpaceBetween" }} alignItems={{ default: "alignItemsFlexStart" }}>
        <FlexItem>
          <Title headingLevel="h1">Topologies</Title>
          <Content component="p">
            One authentik deployment shape each: replica counts, node placement and
            Postgres backend.
          </Content>
        </FlexItem>
        <FlexItem>
          <Button component={Link} href="/topologies/new">
            New topology
          </Button>
        </FlexItem>
      </Flex>

      <ErrorBanner error={error} />

      {data?.length ? (
        <Table role="table">
          <Thead>
            <Tr>
              <Th>Name</Th>
              <Th>Target</Th>
              <Th>Namespace</Th>
              <Th>Server</Th>
              <Th>Worker</Th>
              <Th>Postgres</Th>
              <Th>Placement</Th>
              <Th>Status</Th>
              <Th>Age</Th>
              <Th />
            </Tr>
          </Thead>
          <Tbody>
            {data.map((topology) => (
              <Tr key={topology.metadata.name}>
                <Td>
                  <Link href={`/topologies/${topology.metadata.name}`}>{topology.metadata.name}</Link>
                </Td>
                <Td>
                  <code>{topology.spec.targetName ?? topology.metadata.name}</code>
                </Td>
                <Td>
                  <code>{topology.spec.namespace}</code>
                </Td>
                <Td>{topology.spec.server.replicas}</Td>
                <Td>{topology.spec.worker.replicas}</Td>
                <Td>{topology.spec.postgres.mode}</Td>
                <Td>{topology.spec.placement?.mode ?? "Default"}</Td>
                <Td>
                  <ReadyBadge conditions={topology.status?.conditions} />
                </Td>
                <Td>
                  <Age time={topology.metadata.creationTimestamp} />
                </Td>
                <Td>
                  <ConfirmButton
                    onConfirm={async () => {
                      await api.deleteTopology(topology.metadata.name);
                      reload();
                    }}
                  >
                    Delete
                  </ConfirmButton>
                </Td>
              </Tr>
            ))}
          </Tbody>
        </Table>
      ) : (
        <Empty>
          No topologies yet. <Link href="/topologies/new">Define one</Link>.
        </Empty>
      )}
    </>
  );
}
