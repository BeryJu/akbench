"use client";

import Link from "next/link";
import { use, useState } from "react";
import { Content, FormGroup, Grid, GridItem, Title } from "@patternfly/react-core";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import { api } from "@/lib/api";
import { TestCaseForm } from "@/components/testcase-form";
import { MetricsChart } from "@/components/metrics-chart";
import { DEFAULT_RANGE, TimeRangePicker } from "@/components/time-range-picker";
import { Age, Empty, ErrorBanner, PhaseBadge, ReadyBadge, useResource } from "@/components/ui";

export default function TestCasePage({ params }: { params: Promise<{ name: string }> }) {
  const { name } = use(params);
  const testCase = useResource(() => api.testCase(name), [name]);
  const runs = useResource(() => api.testRuns(name), [name]);
  // Metrics don't come from testCase itself, only the catalog of what
  // metrics exist to chart - compare's response happens to carry that
  // catalog alongside its (unused here) per-test summary values.
  const catalog = useResource(() => api.compare([name], "5m"), [name], 60000);
  const [range, setRange] = useState(DEFAULT_RANGE.seconds);

  return (
    <>
      <Title headingLevel="h1">{name}</Title>
      <Content component="p">
        <ReadyBadge conditions={testCase.data?.status?.conditions} />{" "}
        {testCase.data?.status?.burnWorkloadRef ? (
          <code>{testCase.data.status.burnWorkloadRef.name}</code>
        ) : null}
      </Content>

      <ErrorBanner error={testCase.error ?? runs.error} />

      <Title headingLevel="h2">Metrics</Title>
      <ErrorBanner error={catalog.error} />
      <FormGroup label="Range" fieldId="range" style={{ maxWidth: "260px" }}>
        <TimeRangePicker seconds={range} onChange={setRange} />
      </FormGroup>
      {catalog.data?.metrics.length ? (
        <Grid hasGutter>
          {catalog.data.metrics.map((metric) => (
            <GridItem key={metric.key} span={6}>
              <MetricsChart metric={metric.key} testids={[name]} rangeSeconds={range} />
            </GridItem>
          ))}
        </Grid>
      ) : (
        !catalog.error && <Empty>No metrics available.</Empty>
      )}

      <Title headingLevel="h2">Runs</Title>
      {runs.data?.length ? (
        <Table role="table">
          <Thead>
            <Tr>
              <Th>Run</Th>
              <Th>Phase</Th>
              <Th>VUs</Th>
              <Th>Started</Th>
              <Th>Finished</Th>
            </Tr>
          </Thead>
          <Tbody>
            {runs.data.map((run) => (
              <Tr key={run.metadata.name}>
                <Td>
                  <code>{run.metadata.name}</code>
                </Td>
                <Td>
                  <PhaseBadge phase={run.status?.phase} />
                </Td>
                <Td>{run.status?.resolvedSpec?.vus ?? run.spec.overrides?.vus ?? "-"}</Td>
                <Td>
                  <Age time={run.status?.startTime} />
                </Td>
                <Td>
                  <Age time={run.status?.completionTime} />
                </Td>
              </Tr>
            ))}
          </Tbody>
        </Table>
      ) : (
        <Empty>No runs yet.</Empty>
      )}

      <Title headingLevel="h2">Specification</Title>
      {testCase.data ? <TestCaseForm existing={testCase.data} /> : <p>Loading...</p>}

      <Content component="small">
        <Link href="/testcases">Back to test cases</Link>
      </Content>
    </>
  );
}
