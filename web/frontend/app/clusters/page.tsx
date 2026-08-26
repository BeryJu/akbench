"use client";

import Link from "next/link";
import { Button, Content, Flex, FlexItem, Label, Title } from "@patternfly/react-core";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import { api } from "@/lib/api";
import { ConfirmButton, ErrorBanner, useResource } from "@/components/ui";

export default function ClustersPage() {
  const { data, error, reload } = useResource(() => api.clusters(), [], 15000);

  return (
    <>
      <Flex justifyContent={{ default: "justifyContentSpaceBetween" }} alignItems={{ default: "alignItemsFlexStart" }}>
        <FlexItem>
          <Title headingLevel="h1">Clusters</Title>
          <Content component="p">
            Where topologies can be deployed. Workload clusters are provisioned via Cluster
            API - registering one here creates a <code>WorkloadCluster</code> the operator
            provisions from.
          </Content>
        </FlexItem>
        <FlexItem>
          <Button component={Link} href="/clusters/new">
            Register cluster
          </Button>
        </FlexItem>
      </Flex>

      <ErrorBanner error={error} />

      <Table role="table">
        <Thead>
          <Tr>
            <Th>Name</Th>
            <Th>Role</Th>
            <Th>Phase</Th>
            <Th>Control plane</Th>
            <Th>Infrastructure</Th>
            <Th />
          </Tr>
        </Thead>
        <Tbody>
          {(data ?? []).map((cluster) => (
            <Tr key={cluster.name}>
              <Td>
                {cluster.management ? (
                  <code>{cluster.name}</code>
                ) : (
                  <Link href={`/clusters/${cluster.name}`}>
                    <code>{cluster.name}</code>
                  </Link>
                )}
              </Td>
              <Td>{cluster.management ? "Management" : "Workload"}</Td>
              <Td>{cluster.phase ?? "-"}</Td>
              <Td>
                <Label color={cluster.controlPlaneReady ? "green" : "orange"}>
                  {cluster.controlPlaneReady ? "Ready" : "Not ready"}
                </Label>
              </Td>
              <Td>
                <Label color={cluster.infrastructureReady ? "green" : "orange"}>
                  {cluster.infrastructureReady ? "Ready" : "Not ready"}
                </Label>
              </Td>
              <Td>
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
              </Td>
            </Tr>
          ))}
        </Tbody>
      </Table>
    </>
  );
}
