"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { Button, Content, Flex, FlexItem, Label, Title } from "@patternfly/react-core";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
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
      <Flex justifyContent={{ default: "justifyContentSpaceBetween" }} alignItems={{ default: "alignItemsFlexStart" }}>
        <FlexItem>
          <Title headingLevel="h1">Test cases</Title>
          <Content component="p">
            A Quick test case is executed by starting a run; a Burn test case runs
            continuously for as long as it exists.
          </Content>
        </FlexItem>
        <FlexItem>
          <Button component={Link} href="/testcases/new">
            New test case
          </Button>
        </FlexItem>
      </Flex>

      <ErrorBanner error={actionError ?? error} />

      {data?.length ? (
        <Table role="table">
          <Thead>
            <Tr>
              <Th>Name</Th>
              <Th>Topology</Th>
              <Th>Profile</Th>
              <Th>VUs</Th>
              <Th>Iterations</Th>
              <Th>Status</Th>
              <Th>Age</Th>
              <Th />
            </Tr>
          </Thead>
          <Tbody>
            {data.map((testCase) => (
              <Tr key={testCase.metadata.name}>
                <Td>
                  <Link href={`/testcases/${testCase.metadata.name}`}>{testCase.metadata.name}</Link>
                </Td>
                <Td>
                  <Link href={`/topologies/${testCase.spec.topologyRef.name}`}>
                    {testCase.spec.topologyRef.name}
                  </Link>
                </Td>
                <Td>{testCase.spec.profile}</Td>
                <Td>{testCase.spec.vus}</Td>
                <Td>{testCase.spec.profile === "Quick" ? testCase.spec.iterations : "-"}</Td>
                <Td>
                  <ReadyBadge conditions={testCase.status?.conditions} />
                </Td>
                <Td>
                  <Age time={testCase.metadata.creationTimestamp} />
                </Td>
                <Td>
                  <Flex spaceItems={{ default: "spaceItemsSm" }}>
                    <FlexItem>
                      {testCase.spec.profile === "Quick" ? (
                        <Button
                          variant="secondary"
                          name={`run-${testCase.metadata.name}`}
                          onClick={() => void run(testCase.metadata.name)}
                        >
                          Run now
                        </Button>
                      ) : (
                        <Label color="orange">Burning</Label>
                      )}
                    </FlexItem>
                    <FlexItem>
                      <ConfirmButton
                        onConfirm={async () => {
                          await api.deleteTestCase(testCase.metadata.name);
                          reload();
                        }}
                      >
                        Delete
                      </ConfirmButton>
                    </FlexItem>
                  </Flex>
                </Td>
              </Tr>
            ))}
          </Tbody>
        </Table>
      ) : (
        <Empty>
          No test cases yet. <Link href="/testcases/new">Define one</Link>.
        </Empty>
      )}
    </>
  );
}
