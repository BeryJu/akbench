"use client";

import Link from "next/link";
import { useState } from "react";
import { Card, CardBody, Content, Flex, FlexItem, Gallery, Grid, GridItem, Title } from "@patternfly/react-core";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import { api } from "@/lib/api";
import { MetricsChart } from "@/components/metrics-chart";
import { DEFAULT_RANGE, TimeRangePicker } from "@/components/time-range-picker";
import { Age, Empty, ErrorBanner, PhaseBadge, ReadyBadge, useResource } from "@/components/ui";

// Metrics charted on the dashboard, a small at-a-glance subset of what
// Compare and a TestCase's own page can chart in full.
const DASHBOARD_METRICS = ["http_reqs", "http_req_duration_p95"];

export default function Home() {
  const topologies = useResource(() => api.topologies());
  const testCases = useResource(() => api.testCases());
  const runs = useResource(() => api.testRuns());
  const clusters = useResource(() => api.clusters(), [], 30000);
  const [range, setRange] = useState(DEFAULT_RANGE.seconds);

  const burning = (testCases.data ?? []).filter((tc) => tc.spec.profile === "Burn");
  const burningNames = burning.map((tc) => tc.metadata.name);
  const recent = [...(runs.data ?? [])]
    .sort((a, b) => (b.metadata.creationTimestamp ?? "").localeCompare(a.metadata.creationTimestamp ?? ""))
    .slice(0, 8);

  return (
    <>
      <Title headingLevel="h1">Overview</Title>
      <Content component="p">authentik load-test topologies, test cases and runs.</Content>

      <ErrorBanner error={topologies.error ?? testCases.error ?? runs.error ?? clusters.error} />

      <Gallery hasGutter minWidths={{ default: "200px" }} style={{ marginBottom: "1.5rem" }}>
        <Card>
          <CardBody>
            <Title headingLevel="h2">{clusters.data?.length ?? "-"}</Title>
            <Content component="small">Clusters</Content>
          </CardBody>
        </Card>
        <Card>
          <CardBody>
            <Title headingLevel="h2">{topologies.data?.length ?? "-"}</Title>
            <Content component="small">Topologies</Content>
          </CardBody>
        </Card>
        <Card>
          <CardBody>
            <Title headingLevel="h2">{testCases.data?.length ?? "-"}</Title>
            <Content component="small">Test cases</Content>
          </CardBody>
        </Card>
        <Card>
          <CardBody>
            <Title headingLevel="h2">{burning.length}</Title>
            <Content component="small">Burn tests running</Content>
          </CardBody>
        </Card>
      </Gallery>

      <Flex justifyContent={{ default: "justifyContentSpaceBetween" }} alignItems={{ default: "alignItemsCenter" }}>
        <FlexItem>
          <Title headingLevel="h2">Live metrics</Title>
        </FlexItem>
        {burningNames.length > 0 && (
          <FlexItem>
            <TimeRangePicker seconds={range} onChange={setRange} />
          </FlexItem>
        )}
      </Flex>
      {burningNames.length ? (
        <Grid hasGutter>
          {DASHBOARD_METRICS.map((metric) => (
            <GridItem key={metric} span={6}>
              <MetricsChart metric={metric} testids={burningNames} rangeSeconds={range} />
            </GridItem>
          ))}
        </Grid>
      ) : (
        <Empty>No Burn tests running - nothing to chart live.</Empty>
      )}

      <Title headingLevel="h2">Topologies</Title>
      {topologies.data?.length ? (
        <Table role="table">
          <Thead>
            <Tr>
              <Th>Name</Th>
              <Th>Target</Th>
              <Th>Postgres</Th>
              <Th>Placement</Th>
              <Th>Status</Th>
            </Tr>
          </Thead>
          <Tbody>
            {topologies.data.map((topology) => (
              <Tr key={topology.metadata.name}>
                <Td>
                  <Link href={`/topologies/${topology.metadata.name}`}>{topology.metadata.name}</Link>
                </Td>
                <Td>
                  <code>{topology.spec.targetName ?? topology.metadata.name}</code>
                </Td>
                <Td>{topology.spec.postgres.mode}</Td>
                <Td>{topology.spec.placement?.mode ?? "Default"}</Td>
                <Td>
                  <ReadyBadge conditions={topology.status?.conditions} />
                </Td>
              </Tr>
            ))}
          </Tbody>
        </Table>
      ) : (
        <Empty>
          No topologies yet. <Link href="/topologies/new">Define one</Link> to get an authentik
          install to benchmark.
        </Empty>
      )}

      <Title headingLevel="h2">Recent runs</Title>
      {recent.length ? (
        <Table role="table">
          <Thead>
            <Tr>
              <Th>Run</Th>
              <Th>Test case</Th>
              <Th>Phase</Th>
              <Th>Started</Th>
            </Tr>
          </Thead>
          <Tbody>
            {recent.map((run) => (
              <Tr key={run.metadata.name}>
                <Td>
                  <code>{run.metadata.name}</code>
                </Td>
                <Td>
                  <Link href={`/testcases/${run.spec.testCaseRef.name}`}>
                    {run.spec.testCaseRef.name}
                  </Link>
                </Td>
                <Td>
                  <PhaseBadge phase={run.status?.phase} />
                </Td>
                <Td>
                  <Age time={run.status?.startTime ?? run.metadata.creationTimestamp} /> ago
                </Td>
              </Tr>
            ))}
          </Tbody>
        </Table>
      ) : (
        <Empty>No runs yet.</Empty>
      )}
    </>
  );
}
