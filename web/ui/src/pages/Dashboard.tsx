/*
 * Copyright 2026
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

import { Link } from 'react-router';
import { api } from '../api';
import { MetricsChart } from '../components/MetricsChart';
import { Badge } from '../components/Status';
import { Alert, Empty, formatTimestamp, Page, Stat, Table } from '../components/ui';
import { useResource } from '../useResource';

// An at-a-glance subset of what Compare and a test case's own page chart
// in full.
const DASHBOARD_METRICS = ['http_reqs', 'http_req_duration_p95'];

export function DashboardPage() {
  const deployments = useResource(() => api.deployments(), [], 15000);
  const testCases = useResource(() => api.testCases(), [], 15000);
  const runs = useResource(() => api.testRuns(), [], 10000);
  const clusters = useResource(() => api.clusters(), [], 30000);

  const burning = (testCases.data ?? []).filter(tc => tc.spec.profile === 'Burn').map(tc => tc.metadata.name);
  const recent = [...(runs.data ?? [])]
    .sort((a, b) => (b.metadata.creationTimestamp ?? '').localeCompare(a.metadata.creationTimestamp ?? ''))
    .slice(0, 8);
  const notReady = (deployments.data ?? []).filter(d => d.status?.phase && d.status.phase !== 'Ready');

  return (
    <Page title="Dashboard" subtitle="authentik load-test deployments, test cases and runs.">
      <Alert>{deployments.error ?? testCases.error ?? runs.error}</Alert>
      <div className="stats">
        <Stat value={deployments.data?.length ?? '-'} label="Deployments" />
        <Stat value={testCases.data?.length ?? '-'} label="Test cases" />
        <Stat value={burning.length} label="Burn tests running" />
        <Stat value={runs.data?.length ?? '-'} label="Test runs" />
        <Stat value={clusters.data?.length ?? '-'} label="Clusters" />
      </div>

      {notReady.length > 0 && (
        <>
          <h2>Needs attention</h2>
          <div className="row">
            {notReady.map(d => (
              <Link key={d.metadata.name} className="button small" to={`/deployments/${d.metadata.name}`}>
                {d.metadata.name} <Badge value={d.status?.phase} />
              </Link>
            ))}
          </div>
        </>
      )}

      <h2>Live metrics</h2>
      {burning.length ? (
        <div className="grid2">
          {DASHBOARD_METRICS.map(metric => (
            <MetricsChart key={metric} metric={metric} testids={burning} />
          ))}
        </div>
      ) : (
        <Empty>No Burn tests running - nothing to chart live.</Empty>
      )}

      <h2>Recent runs</h2>
      <Table
        rows={runs.data && recent}
        rowKey={r => r.metadata.name}
        empty="No runs yet."
        columns={[
          { label: 'Run', render: r => <Link to={`/testruns/${r.metadata.name}`}>{r.metadata.name}</Link> },
          {
            label: 'Test case',
            render: r => <Link to={`/testcases/${r.spec.testCaseRef.name}`}>{r.spec.testCaseRef.name}</Link>,
          },
          { label: 'Phase', render: r => <Badge value={r.status?.phase ?? 'Pending'} /> },
          { label: 'Started', render: r => formatTimestamp(r.status?.startTime ?? r.metadata.creationTimestamp) },
        ]}
      />
    </Page>
  );
}
