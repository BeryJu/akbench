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

// Test runs are created by "Run now" on a Quick test case, never by hand -
// so this is history plus a read-only view.
import { Link, useParams } from 'react-router';
import { api } from '../api';
import { MetricsGrid } from '../components/MetricsChart';
import { ObjectYaml } from '../components/ObjectYaml';
import { Badge, ConditionList } from '../components/Status';
import { Alert, confirmAndRun, Empty, formatTimestamp, Page, Table } from '../components/ui';
import { metricsAnchorFromRuns } from '../format';
import { shellLinkForCluster } from '../shell/links';
import { testCaseDeployments } from '../types';
import { useResource } from '../useResource';

export function TestRunsPage() {
  const runs = useResource(() => api.testRuns());
  const sorted = runs.data && [...runs.data].sort((a, b) =>
    (b.metadata.creationTimestamp ?? '').localeCompare(a.metadata.creationTimestamp ?? '')
  );
  return (
    <Page title="Test runs" subtitle="One-shot runs of Quick test cases.">
      <Alert>{runs.error}</Alert>
      <Table
        rows={sorted}
        rowKey={r => r.metadata.name}
        empty="No runs yet - start one with Run now on a Quick test case."
        columns={[
          { label: 'Run', render: r => <Link to={`/testruns/${r.metadata.name}`}>{r.metadata.name}</Link> },
          {
            label: 'Test case',
            render: r => <Link to={`/testcases/${r.spec.testCaseRef.name}`}>{r.spec.testCaseRef.name}</Link>,
          },
          {
            label: 'Deployment',
            // Unset on a run created before multi-deployment TestCases
            // existed, in which case it ran against that TestCase's own
            // (only) deployment - not worth an extra fetch per row just to
            // resolve that here, so it's shown blank instead.
            render: r => r.spec.deploymentRef?.name ?? <span className="muted">-</span>,
          },
          { label: 'Phase', render: r => <Badge value={r.status?.phase ?? 'Pending'} /> },
          { label: 'Started', render: r => formatTimestamp(r.status?.startTime ?? r.metadata.creationTimestamp) },
          { label: 'Completed', render: r => formatTimestamp(r.status?.completionTime) },
          {
            label: '',
            className: 'actions',
            render: r => (
              <button
                className="small danger"
                onClick={() =>
                  void confirmAndRun(
                    `Delete test run "${r.metadata.name}"?`,
                    () => api.deleteTestRun(r.metadata.name),
                    runs.reload
                  )
                }
              >
                Delete
              </button>
            ),
          },
        ]}
      />
    </Page>
  );
}

export function TestRunViewPage() {
  const { name = '' } = useParams();
  const run = useResource(() => api.testRun(name), [name], 5000);
  const testCaseName = run.data?.spec.testCaseRef.name;
  const testCase = useResource(
    () => (testCaseName ? api.testCase(testCaseName) : Promise.resolve(undefined)),
    [testCaseName],
    0
  );
  // run.spec.deploymentRef pins which of the TestCase's own deployments this
  // run targeted; unset on a run created before multi-deployment TestCases
  // existed, in which case it's that TestCase's own first/only deployment -
  // same fallback TestRunReconciler itself uses.
  const deploymentName =
    run.data?.spec.deploymentRef?.name ?? (testCase.data ? testCaseDeployments(testCase.data.spec)[0]?.name : undefined);
  const deployment = useResource(
    () => (deploymentName ? api.deployment(deploymentName) : Promise.resolve(undefined)),
    [deploymentName],
    0
  );
  const cluster = testCase.data?.spec.runnerClusterRef?.name ?? deployment.data?.spec.clusterRef?.name;
  const namespace = testCase.data?.metadata.namespace;
  const jobName = run.data?.status?.jobRef?.name;
  // Metrics scoped to exactly this run's own window.
  const anchor = run.data ? metricsAnchorFromRuns([run.data]) : undefined;
  const status = run.data?.status;

  return (
    <Page title={name} subtitle={testCaseName && <>Run of <Link to={`/testcases/${testCaseName}`}>{testCaseName}</Link></>}>
      <Alert>{run.error}</Alert>
      {run.data && (
        <div className="panel">
          <div className="row" style={{ marginBottom: 8 }}>
            <Badge value={status?.phase ?? 'Pending'} />
            <span className="muted">
              {formatTimestamp(status?.startTime)} → {formatTimestamp(status?.completionTime)}
            </span>
            {jobName && namespace && (
              <Link className="button small" to={shellLinkForCluster(cluster, `kubectl logs -n ${namespace} job/${jobName} --tail=500`)}>
                k6 logs
              </Link>
            )}
          </div>
          <ConditionList conditions={status?.conditions} />
        </div>
      )}
      <h2>Metrics</h2>
      {!anchor || !testCaseName ? (
        <Empty>Run still in progress - metrics will show once it completes.</Empty>
      ) : (
        <MetricsGrid testids={[testCaseName]} endSeconds={anchor.endSeconds} rangeSeconds={anchor.rangeSeconds} />
      )}
      <ObjectYaml object={run.data} />
    </Page>
  );
}
