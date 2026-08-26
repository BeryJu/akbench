"use client";

import { useSearchParams } from "next/navigation";
import { Suspense, useMemo, useState } from "react";
import {
  Checkbox,
  Content,
  Flex,
  FlexItem,
  Form,
  FormGroup,
  FormSelect,
  FormSelectOption,
  Grid,
  GridItem,
  Title,
} from "@patternfly/react-core";
import { Table, Tbody, Td, Th, Thead, Tr } from "@patternfly/react-table";
import { api } from "@/lib/api";
import { formatMetricValue } from "@/lib/format";
import type { CompareResponse } from "@/lib/types";
import { Empty, ErrorBanner, useResource } from "@/components/ui";
import { MetricsChart } from "@/components/metrics-chart";
import { DEFAULT_RANGE, TimeRangePicker } from "@/components/time-range-picker";

const windows = ["1m", "5m", "15m", "1h"];

/** best picks the winning test per metric, honouring the metric's direction. */
function best(response: CompareResponse, key: string, better: string): string | undefined {
  const scored = response.results
    .filter((result) => result.values[key] !== undefined)
    .map((result) => ({ testid: result.testid, value: result.values[key] as number }));
  if (scored.length < 2) {
    return undefined;
  }
  return scored.reduce((a, b) => ((better === "lower" ? b.value < a.value : b.value > a.value) ? b : a))
    .testid;
}

function Compare() {
  const search = useSearchParams();
  const testCases = useResource(() => api.testCases(), [], 0);
  const [selected, setSelected] = useState<string[]>(
    search.get("testids")?.split(",").filter(Boolean) ?? [],
  );
  const [window, setWindow] = useState("5m");
  const [range, setRange] = useState(DEFAULT_RANGE.seconds);

  const comparison = useResource(
    () =>
      selected.length
        ? api.compare(selected, window)
        : Promise.resolve(undefined as unknown as CompareResponse),
    [selected.join(","), window],
    10000,
  );

  const rows = useMemo(() => comparison.data?.metrics ?? [], [comparison.data]);

  function toggle(name: string) {
    setSelected((current) =>
      current.includes(name) ? current.filter((id) => id !== name) : [...current, name],
    );
  }

  return (
    <>
      <Title headingLevel="h1">Compare</Title>
      <Content component="p">
        Pick the tests to put side by side. The table summarises the last {window} of each;
        the charts below plot them over time.
      </Content>

      <ErrorBanner error={comparison.error ?? testCases.error} />

      <Form>
        <Title headingLevel="h2">Tests</Title>
        <Flex>
          {(testCases.data ?? []).map((testCase) => (
            <FlexItem key={testCase.metadata.name}>
              <Checkbox
                id={`testid-${testCase.metadata.name}`}
                name={`testid-${testCase.metadata.name}`}
                label={testCase.metadata.name}
                isChecked={selected.includes(testCase.metadata.name)}
                onChange={() => toggle(testCase.metadata.name)}
              />
            </FlexItem>
          ))}
          {testCases.data?.length === 0 && <Content component="small">No test cases yet.</Content>}
        </Flex>
        <Grid hasGutter md={4}>
          <GridItem span={12} md={4}>
            <FormGroup label="Summary window" fieldId="window">
              <FormSelect id="window" value={window} onChange={(_e, value) => setWindow(value)}>
                {windows.map((option) => (
                  <FormSelectOption key={option} value={option} label={option} />
                ))}
              </FormSelect>
            </FormGroup>
          </GridItem>
          <GridItem span={12} md={4}>
            <FormGroup label="Chart range" fieldId="range">
              <TimeRangePicker seconds={range} onChange={setRange} />
            </FormGroup>
          </GridItem>
        </Grid>
      </Form>

      {selected.length === 0 ? (
        <Empty>Select at least one test.</Empty>
      ) : (
        <>
          <Title headingLevel="h2">Summary</Title>
          <Table role="table">
            <Thead>
              <Tr>
                <Th>Metric</Th>
                {(comparison.data?.results ?? []).map((result) => (
                  <Th key={result.testid}>{result.testid}</Th>
                ))}
              </Tr>
            </Thead>
            <Tbody>
              {rows.map((metric) => {
                const winner = comparison.data ? best(comparison.data, metric.key, metric.better) : undefined;
                return (
                  <Tr key={metric.key}>
                    <Td>
                      {metric.label} <Content component="small">({metric.better} is better)</Content>
                    </Td>
                    {(comparison.data?.results ?? []).map((result) => (
                      <Td key={result.testid}>
                        <code>
                          {formatMetricValue(result.values[metric.key], metric.unit)}
                          {winner === result.testid ? " ✓" : ""}
                        </code>
                      </Td>
                    ))}
                  </Tr>
                );
              })}
            </Tbody>
          </Table>

          <Title headingLevel="h2">Over time</Title>
          <Grid hasGutter>
            {rows.map((metric) => (
              <GridItem key={metric.key} span={6}>
                <MetricsChart metric={metric.key} testids={selected} rangeSeconds={range} />
              </GridItem>
            ))}
          </Grid>
        </>
      )}
    </>
  );
}

export default function ComparePage() {
  return (
    <Suspense fallback={<p>Loading...</p>}>
      <Compare />
    </Suspense>
  );
}
