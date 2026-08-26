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

// registerDetailsViewSection sections attached to TestCase/AuthentikTopology
// detail pages (Headlamp's own generic CRD detail view already renders
// everything else - spec, status, events, YAML - these sections add exactly
// the parts that are benchmark-specific).
import { EmptyContent } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import { Alert, Box, Button } from '@mui/material';
import { useState } from 'react';
import { api } from './api';
import { MetricsChart } from './MetricsChart';
import { useResource } from './useResource';

/**
 * TestCaseSection: a "Run now" button (only meaningful for Quick profile -
 * Burn already runs continuously as a self-healing Deployment) plus the
 * same metrics chart TopologySection renders, scoped to this one test.
 */
export function TestCaseSection({ name, profile }: { name: string; profile?: string }) {
  const [running, setRunning] = useState(false);
  const [error, setError] = useState<string>();
  const [ranAt, setRanAt] = useState<string>();

  async function runNow() {
    setRunning(true);
    setError(undefined);
    try {
      const run = await api.runTestCase(name);
      setRanAt(run.metadata.name);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setRunning(false);
    }
  }

  return (
    <Box>
      {profile === 'Quick' && (
        <Box sx={{ mb: 2 }}>
          <Button variant="contained" disabled={running} onClick={() => void runNow()}>
            {running ? 'Starting…' : 'Run now'}
          </Button>
          {ranAt && (
            <Alert severity="success" sx={{ mt: 1 }}>
              Started TestRun {ranAt}
            </Alert>
          )}
          {error && (
            <Alert severity="error" sx={{ mt: 1 }}>
              {error}
            </Alert>
          )}
        </Box>
      )}
      <MetricsChart metric="http_reqs" testids={[name]} />
      <MetricsChart metric="http_req_duration_p95" testids={[name]} />
    </Box>
  );
}

/**
 * TopologySection: metrics for every TestCase currently pointed at this
 * topology - the only way to see a Burn TestCase's metrics scoped to "the
 * topology it's hammering" rather than picking it manually on Compare, and
 * the exact gap /metrics/range (internal/api/metrics.go) was originally
 * built to close for topologies with no TestRun history at all.
 */
export function TopologySection({ topologyName }: { topologyName: string }) {
  const testCases = useResource(() => api.testCases(), [], 10000);
  const testids = (testCases.data ?? [])
    .filter(tc => tc.spec.topologyRef.name === topologyName)
    .map(tc => tc.metadata.name);

  if (testids.length === 0) {
    return <EmptyContent>No test cases reference this topology yet.</EmptyContent>;
  }
  return (
    <Box>
      <MetricsChart metric="http_reqs" testids={testids} />
      <MetricsChart metric="http_req_duration_p95" testids={testids} />
    </Box>
  );
}
