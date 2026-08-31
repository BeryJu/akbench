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

// List + create/edit views for TestCase, mirroring the deleted
// web/frontend's /testcases pages.
import { Router } from '@kinvolk/headlamp-plugin/lib';
import { EmptyContent, Link, SectionBox, SimpleTable } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import { Alert, Button, Chip, Stack } from '@mui/material';
import { useHistory, useParams } from 'react-router-dom';
import { api, ApiError, headlampClusterName } from './api';
import { ClusterResourceButton } from './ClusterResourceButton';
import { TestCaseForm } from './TestCaseForm';
import { useResource } from './useResource';

export function TestCasesPage() {
  const history = useHistory();
  const testCases = useResource(() => api.testCases());
  // Only needed to resolve which cluster a Burn test case's k6 Deployment
  // runs on - TestCaseSpec has no clusterRef of its own, it inherits the
  // topology's (or an explicit runnerClusterRef override).
  const topologies = useResource(() => api.topologies(), [], 0);
  const clusterByTopology = new Map(
    (topologies.data ?? []).map(topology => [topology.metadata.name, topology.spec.clusterRef?.name])
  );

  async function remove(name: string) {
    if (!window.confirm(`Delete test case "${name}"?`)) {
      return;
    }
    try {
      await api.deleteTestCase(name);
      testCases.reload();
    } catch (err) {
      window.alert(err instanceof ApiError ? err.message : String(err));
    }
  }

  async function runNow(name: string) {
    try {
      const run = await api.runTestCase(name);
      window.alert(`Started TestRun ${run.metadata.name}`);
    } catch (err) {
      window.alert(err instanceof ApiError ? err.message : String(err));
    }
  }

  return (
    <SectionBox
      title="Test cases"
      headerProps={{
        actions: [
          <Button
            key="new"
            variant="contained"
            onClick={() => history.push(Router.createRouteURL('authentik-bench-testcases-new'))}
          >
            New test case
          </Button>,
        ],
      }}
    >
      {testCases.error && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {testCases.error}
        </Alert>
      )}
      <SimpleTable
        columns={[
          {
            label: 'Name',
            getter: testCase => (
              <Link routeName="authentik-bench-testcases-edit" params={{ name: testCase.metadata.name }}>
                {testCase.metadata.name}
              </Link>
            ),
          },
          { label: 'Topology', getter: testCase => testCase.spec.topologyRef.name },
          {
            label: 'Profile',
            getter: testCase => (
              <Chip size="small" label={testCase.spec.profile} color={testCase.spec.profile === 'Burn' ? 'warning' : 'default'} />
            ),
          },
          { label: 'VUs', getter: testCase => testCase.spec.vus },
          {
            label: 'Resources',
            getter: testCase => {
              const burnDeployment = testCase.status?.burnWorkloadRef?.name;
              if (!burnDeployment || !testCase.metadata.namespace) {
                return null;
              }
              const cluster = headlampClusterName(
                testCase.spec.runnerClusterRef?.name ?? clusterByTopology.get(testCase.spec.topologyRef.name)
              );
              return (
                <ClusterResourceButton
                  kind="Deployment"
                  cluster={cluster}
                  namespace={testCase.metadata.namespace}
                  name={burnDeployment}
                  label="k6 Deployment"
                />
              );
            },
          },
          {
            label: '',
            getter: testCase => (
              <Stack direction="row" spacing={1}>
                {testCase.spec.profile === 'Quick' && (
                  <Button size="small" onClick={() => void runNow(testCase.metadata.name)}>
                    Run now
                  </Button>
                )}
                <Button size="small" color="error" onClick={() => void remove(testCase.metadata.name)}>
                  Delete
                </Button>
              </Stack>
            ),
          },
        ]}
        data={testCases.data ?? []}
        emptyMessage="No test cases yet."
      />
      {testCases.data?.length === 0 && (
        <EmptyContent>Create a test case to drive load against one of your topologies.</EmptyContent>
      )}
    </SectionBox>
  );
}

export function TestCaseCreatePage() {
  return <TestCaseForm />;
}

export function TestCaseEditPage() {
  const { name } = useParams<{ name: string }>();
  const existing = useResource(() => api.testCase(name), [name]);
  if (existing.loading) {
    return null;
  }
  if (existing.error) {
    return (
      <SectionBox title="Edit test case">
        <Alert severity="error">{existing.error}</Alert>
      </SectionBox>
    );
  }
  return <TestCaseForm existing={existing.data} />;
}
