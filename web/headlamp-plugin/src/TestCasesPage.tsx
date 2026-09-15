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
import { EmptyContent, Link, SectionBox, SimpleTable } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import { Alert, Button, Chip, Stack } from '@mui/material';
import { useParams } from 'react-router-dom';
import { api, ApiError, MANAGEMENT_CLUSTER } from './api';
import { TestCaseSection } from './DetailSections';
import { PrimaryLinkButton } from './PrimaryLinkButton';
import { StatusChip } from './StatusPopover';
import { TestCaseForm } from './TestCaseForm';
import { useResource } from './useResource';

export function TestCasesPage() {
  const testCases = useResource(() => api.testCases());

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
        actions: [<PrimaryLinkButton key="new" routeName="authentik-bench-testcases-new" label="New test case" />],
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
              <Link routeName="authentik-bench-testcases-view" params={{ name: testCase.metadata.name }}>
                {testCase.metadata.name}
              </Link>
            ),
          },
          { label: 'Deployment', getter: testCase => testCase.spec.deploymentRef.name },
          {
            label: 'Profile',
            getter: testCase => (
              <Chip size="small" label={testCase.spec.profile} color={testCase.spec.profile === 'Burn' ? 'warning' : 'default'} />
            ),
          },
          { label: 'VUs', getter: testCase => testCase.spec.vus },
          {
            label: 'Status',
            getter: testCase => (
              <StatusChip
                kind="TestCase"
                name={testCase.metadata.name}
                namespace={testCase.metadata.namespace ?? 'default'}
                cluster={MANAGEMENT_CLUSTER}
                phase={testCase.status?.phase}
                conditions={testCase.status?.conditions}
                links={[{ label: 'Deployment', routeName: 'authentik-bench-deployments-edit', params: { name: testCase.spec.deploymentRef.name } }]}
              />
            ),
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
                <Link routeName="authentik-bench-testcases-edit" params={{ name: testCase.metadata.name }}>
                  Edit
                </Link>
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
        <EmptyContent>Create a test case to drive load against one of your deployments.</EmptyContent>
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

export function TestCaseViewPage() {
  const { name } = useParams<{ name: string }>();
  const existing = useResource(() => api.testCase(name), [name], 0);
  return (
    <SectionBox title={name}>
      <TestCaseSection name={name} profile={existing.data?.spec.profile} />
    </SectionBox>
  );
}
