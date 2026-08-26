"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { Suspense } from "react";
import { Button, Content, Flex, FlexItem, Title } from "@patternfly/react-core";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import { api } from "@/lib/api";
import { Age, ConfirmButton, Empty, ErrorBanner, PhaseBadge, useResource } from "@/components/ui";

function TestRuns() {
  const search = useSearchParams();
  const testCase = search.get("testcase") ?? undefined;
  const { data, error, reload } = useResource(() => api.testRuns(testCase), [testCase]);

  const runs = [...(data ?? [])].sort((a, b) =>
    (b.metadata.creationTimestamp ?? "").localeCompare(a.metadata.creationTimestamp ?? ""),
  );

  return (
    <>
      <Title headingLevel="h1">Test runs</Title>
      <Content component="p">
        {testCase ? (
          <>
            Runs of <code>{testCase}</code>. <Link href="/testruns">Show all</Link>
          </>
        ) : (
          "Every bounded execution, kept as its own record so a result stays readable after the test case it came from has moved on."
        )}
      </Content>

      <ErrorBanner error={error} />

      {runs.length ? (
        <Table role="table">
          <Thead>
            <Tr>
              <Th>Run</Th>
              <Th>Test case</Th>
              <Th>Phase</Th>
              <Th>VUs</Th>
              <Th>Iterations</Th>
              <Th>Started</Th>
              <Th>Finished</Th>
              <Th />
            </Tr>
          </Thead>
          <Tbody>
            {runs.map((run) => (
              <Tr key={run.metadata.name}>
                <Td>
                  <code>{run.metadata.name}</code>
                </Td>
                <Td>
                  <Link href={`/testcases/${run.spec.testCaseRef.name}`}>{run.spec.testCaseRef.name}</Link>
                </Td>
                <Td>
                  <PhaseBadge phase={run.status?.phase} />
                </Td>
                <Td>{run.status?.resolvedSpec?.vus ?? run.spec.overrides?.vus ?? "-"}</Td>
                <Td>{run.status?.resolvedSpec?.iterations ?? run.spec.overrides?.iterations ?? "-"}</Td>
                <Td>
                  <Age time={run.status?.startTime} />
                </Td>
                <Td>
                  <Age time={run.status?.completionTime} />
                </Td>
                <Td>
                  <Flex spaceItems={{ default: "spaceItemsSm" }}>
                    <FlexItem>
                      <Button component={Link} href={`/compare?testids=${run.spec.testCaseRef.name}`} variant="secondary">
                        Metrics
                      </Button>
                    </FlexItem>
                    <FlexItem>
                      <ConfirmButton
                        onConfirm={async () => {
                          await api.deleteTestRun(run.metadata.name);
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
        <Empty>No runs yet. Start one from a Quick test case.</Empty>
      )}
    </>
  );
}

export default function TestRunsPage() {
  // useSearchParams needs a Suspense boundary, or the whole route opts out of
  // static rendering with a build-time error.
  return (
    <Suspense fallback={<p>Loading...</p>}>
      <TestRuns />
    </Suspense>
  );
}
