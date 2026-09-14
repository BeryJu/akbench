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

// Ported from the deleted web/frontend/app/page.tsx, on Headlamp's own
// SectionBox/SimpleTable/EmptyContent instead of PatternFly. The full
// Deployments table from that version is dropped here - Headlamp's own
// generic CRD browser (sidebar -> Custom Resources) already lists/links
// every AuthentikDeployment for free, so duplicating that table would just be
// a second, un-synced way to see the same thing. This page keeps only
// what's genuinely benchmark-specific: at-a-glance counts, live metrics for
// whatever's actively Burn-testing, and recent runs.
import { EmptyContent, SectionBox, SimpleTable } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import { Alert, Box, Grid, Paper, Typography } from '@mui/material';
import { api } from './api';
import { MetricsChart } from './MetricsChart';
import { NodePlacementChart } from './NodePlacementChart';
import { useResource } from './useResource';

// A small at-a-glance subset of what Compare and a TestCase's own detail
// section can chart in full.
const DASHBOARD_METRICS = ['http_reqs', 'http_req_duration_p95'];

function StatCard({ value, label }: { value: number | string; label: string }) {
  return (
    <Grid item xs={6} md={3}>
      <Paper variant="outlined" sx={{ p: 2, textAlign: 'center' }}>
        <Typography variant="h4">{value}</Typography>
        <Typography variant="body2" color="text.secondary">
          {label}
        </Typography>
      </Paper>
    </Grid>
  );
}

export function DashboardPage() {
  const deployments = useResource(() => api.deployments());
  const testCases = useResource(() => api.testCases());
  const runs = useResource(() => api.testRuns());

  const burning = (testCases.data ?? []).filter(tc => tc.spec.profile === 'Burn');
  const burningNames = burning.map(tc => tc.metadata.name);
  const recent = [...(runs.data ?? [])]
    .sort((a, b) => (b.metadata.creationTimestamp ?? '').localeCompare(a.metadata.creationTimestamp ?? ''))
    .slice(0, 8);

  return (
    <SectionBox title="authentik-bench" subtitle="authentik load-test deployments, test cases and runs.">
      {(deployments.error || testCases.error || runs.error) && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {deployments.error ?? testCases.error ?? runs.error}
        </Alert>
      )}

      <Grid container spacing={2} sx={{ mb: 3 }}>
        <StatCard value={deployments.data?.length ?? '-'} label="Deployments" />
        <StatCard value={testCases.data?.length ?? '-'} label="Test cases" />
        <StatCard value={burning.length} label="Burn tests running" />
        <StatCard value={runs.data?.length ?? '-'} label="Test runs" />
      </Grid>

      <Box sx={{ mb: 3 }}>
        <Typography variant="h6" gutterBottom>
          Live metrics
        </Typography>
        {burningNames.length ? (
          <Grid container spacing={2}>
            {DASHBOARD_METRICS.map(metric => (
              <Grid item xs={12} md={6} key={metric}>
                <MetricsChart metric={metric} testids={burningNames} />
              </Grid>
            ))}
          </Grid>
        ) : (
          <EmptyContent>No Burn tests running - nothing to chart live.</EmptyContent>
        )}
      </Box>

      <Box sx={{ mb: 3 }}>
        <NodePlacementChart />
      </Box>

      <Typography variant="h6" gutterBottom>
        Recent runs
      </Typography>
      <SimpleTable
        columns={[
          { label: 'Run', getter: run => run.metadata.name },
          { label: 'Test case', getter: run => run.spec.testCaseRef.name },
          { label: 'Phase', getter: run => run.status?.phase ?? 'Unknown' },
          { label: 'Started', getter: run => run.status?.startTime ?? run.metadata.creationTimestamp ?? '-' },
        ]}
        data={recent}
        emptyMessage="No runs yet."
      />
    </SectionBox>
  );
}
