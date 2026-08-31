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

// History view for TestRun (Quick test case executions), mirroring the
// deleted web/frontend's /testruns page. There's no create/edit form here -
// a TestRun is only ever created via "Run now" (TestCaseForm/
// DetailSections' TestCaseSection), never hand-authored - so this page is
// list + delete only, matching api.testRuns()/api.deleteTestRun() which
// were otherwise unused since the Headlamp pivot.
import { EmptyContent, SectionBox, SimpleTable } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import { Alert, Button, Chip } from '@mui/material';
import { api, ApiError } from './api';
import { useResource } from './useResource';

function PhaseChip({ phase }: { phase?: string }) {
  if (!phase) {
    return <Chip size="small" label="Unknown" />;
  }
  const color =
    phase === 'Succeeded' ? 'success' : phase === 'Failed' ? 'error' : phase === 'Running' ? 'info' : 'default';
  return <Chip size="small" label={phase} color={color} />;
}

export function TestRunsPage() {
  const testRuns = useResource(() => api.testRuns());

  async function remove(name: string) {
    if (!window.confirm(`Delete test run "${name}"?`)) {
      return;
    }
    try {
      await api.deleteTestRun(name);
      testRuns.reload();
    } catch (err) {
      window.alert(err instanceof ApiError ? err.message : String(err));
    }
  }

  const runs = [...(testRuns.data ?? [])].sort((a, b) =>
    (b.metadata.creationTimestamp ?? '').localeCompare(a.metadata.creationTimestamp ?? '')
  );

  return (
    <SectionBox title="Test runs">
      {testRuns.error && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {testRuns.error}
        </Alert>
      )}
      <SimpleTable
        columns={[
          { label: 'Run', getter: run => run.metadata.name },
          { label: 'Test case', getter: run => run.spec.testCaseRef.name },
          { label: 'Phase', getter: run => <PhaseChip phase={run.status?.phase} /> },
          { label: 'Started', getter: run => run.status?.startTime ?? run.metadata.creationTimestamp ?? '-' },
          { label: 'Completed', getter: run => run.status?.completionTime ?? '-' },
          {
            label: '',
            getter: run => (
              <Button size="small" color="error" onClick={() => void remove(run.metadata.name)}>
                Delete
              </Button>
            ),
          },
        ]}
        data={runs}
        emptyMessage="No test runs yet."
      />
      {runs.length === 0 && <EmptyContent>Run a Quick test case to see its history here.</EmptyContent>}
    </SectionBox>
  );
}
