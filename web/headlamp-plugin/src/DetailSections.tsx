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

// registerDetailsViewSection sections attached to TestCase/AuthentikDeployment
// detail pages (Headlamp's own generic CRD detail view already renders
// everything else - spec, status, events, YAML - these sections add exactly
// the parts that are benchmark-specific).
import { EmptyContent } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import { Alert, Box, Button, Link as MuiLink, Stack, Typography } from '@mui/material';
import { useState } from 'react';
import { api, headlampClusterName } from './api';
import { ClusterResourceButton } from './ClusterResourceButton';
import { metricsAnchorFromRuns } from './format';
import { DASHBOARDS, grafanaProxyURL } from './GrafanaPage';
import { MetricsChart } from './MetricsChart';
import { CompareMetric } from './types';
import { useResource } from './useResource';

/**
 * GrafanaDashboardLinks: one link per dashboard this project's Grafana ships
 * (see GrafanaPage.tsx) for a specific workload cluster - reused by
 * DeploymentSection and WorkloadClusterSection so both link straight to a
 * real dashboard instead of only sending someone to the plugin's own
 * Grafana tab to pick the cluster over again by hand.
 */
function GrafanaDashboardLinks({ workloadClusterName }: { workloadClusterName: string }) {
  return (
    <Stack direction="row" spacing={2} sx={{ mb: 2 }} flexWrap="wrap">
      {DASHBOARDS.map(d => (
        <MuiLink
          key={d.uid}
          href={grafanaProxyURL(workloadClusterName, d.uid)}
          target="_blank"
          rel="noreferrer"
          sx={{ fontSize: '0.8125rem' }}
        >
          {d.label} ↗
        </MuiLink>
      ))}
    </Stack>
  );
}

/**
 * useAvailableMetrics discovers every metric key/label cmd/apiserver
 * currently knows about (internal/api/metrics.go's own compareMetrics list)
 * from a live /metrics/compare call, the same way ComparePage.tsx already
 * does - rather than hardcoding a duplicate list of metric keys here, which
 * would silently go stale the next time a metric is added or renamed on the
 * backend. Only the *definitions* (.metrics) are used; the one testid this
 * calls with is just to get a real response back, not to read its values.
 * Fetched once (metric definitions are static for the lifetime of a running
 * apiserver) rather than polled.
 */
function useAvailableMetrics(testids: string[]): CompareMetric[] {
  const primary = testids[0];
  const result = useResource(
    () => (primary ? api.compare([primary], '5m') : Promise.resolve(undefined)),
    [primary],
    0
  );
  return result.data?.metrics ?? [];
}

/**
 * TestCaseSection: a "Run now" button (only meaningful for Quick profile -
 * Burn already runs continuously as a self-healing Deployment), a link to
 * the actual k6 Deployment for a Burn test case (Quick has no persistent
 * workload to link to - it's a one-shot Job per TestRun), plus the same
 * metrics chart DeploymentSection renders, scoped to this one test.
 */
export function TestCaseSection({ name, profile }: { name: string; profile?: string }) {
  const [running, setRunning] = useState(false);
  const [error, setError] = useState<string>();
  const [ranAt, setRanAt] = useState<string>();

  const testCase = useResource(() => api.testCase(name), [name], 0);
  const deploymentName = testCase.data?.spec.deploymentRef.name;
  const deployment = useResource(
    () => (deploymentName ? api.deployment(deploymentName) : Promise.resolve(undefined)),
    [deploymentName],
    0
  );
  const cluster = headlampClusterName(
    testCase.data?.spec.runnerClusterRef?.name ?? deployment.data?.spec.clusterRef?.name
  );
  const namespace = testCase.data?.metadata.namespace;
  const burnDeployment = testCase.data?.status?.burnWorkloadRef?.name;

  // Only Quick has a run history to anchor against - Burn runs continuously,
  // so "now" (MetricsChart's own default) is already correct for it.
  const runs = useResource(
    () => (profile === 'Quick' ? api.testRuns(name) : Promise.resolve([])),
    [name, profile],
    15000
  );
  const anchor = profile === 'Quick' ? metricsAnchorFromRuns(runs.data ?? []) : undefined;
  const metrics = useAvailableMetrics([name]);

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
      {burnDeployment && namespace && (
        <Stack direction="row" spacing={1} sx={{ mb: 2 }}>
          <ClusterResourceButton
            kind="Deployment"
            cluster={cluster}
            namespace={namespace}
            name={burnDeployment}
            label={`View k6 Deployment on ${cluster}`}
          />
        </Stack>
      )}
      {profile === 'Quick' && !anchor && (runs.data?.length ?? 0) > 0 && (
        <Alert severity="info" sx={{ mb: 2 }}>
          Latest run is still in progress - metrics will show once it completes.
        </Alert>
      )}
      {metrics.map(metric => (
        <MetricsChart
          key={metric.key}
          metric={metric.key}
          testids={[name]}
          endSeconds={anchor?.endSeconds}
          rangeSeconds={anchor?.rangeSeconds}
        />
      ))}
    </Box>
  );
}

/**
 * DeploymentSection: links to the authentik chart's own server/worker
 * Deployments and (Embedded postgres only) its StatefulSet on whichever
 * cluster the deployment actually runs on, plus metrics for every TestCase
 * currently pointed at this deployment - the only way to see a Burn TestCase's
 * metrics scoped to "the deployment it's hammering" rather than picking it
 * manually on Compare, and the exact gap /metrics/range
 * (internal/api/metrics.go) was originally built to close for deployments
 * with no TestRun history at all.
 */
export function DeploymentSection({ deploymentName }: { deploymentName: string }) {
  const deployment = useResource(() => api.deployment(deploymentName), [deploymentName], 0);
  const testCases = useResource(() => api.testCases(), [], 10000);
  const testids = (testCases.data ?? [])
    .filter(tc => tc.spec.deploymentRef.name === deploymentName)
    .map(tc => tc.metadata.name);
  const metrics = useAvailableMetrics(testids);

  const clusterRefName = deployment.data?.spec.clusterRef?.name;
  const cluster = headlampClusterName(clusterRefName);
  const namespace = deployment.data?.spec.namespace;

  return (
    <Box>
      {clusterRefName && <GrafanaDashboardLinks workloadClusterName={clusterRefName} />}
      {namespace && (
        <Stack direction="row" spacing={1} sx={{ mb: 2 }} flexWrap="wrap">
          <ClusterResourceButton
            kind="Deployment"
            cluster={cluster}
            namespace={namespace}
            name={`${deploymentName}-authentik-server`}
            label={`View server Deployment on ${cluster}`}
          />
          <ClusterResourceButton
            kind="Deployment"
            cluster={cluster}
            namespace={namespace}
            name={`${deploymentName}-authentik-worker`}
            label={`View worker Deployment on ${cluster}`}
          />
          {deployment.data?.spec.postgres.mode === 'Embedded' && (
            <ClusterResourceButton
              kind="StatefulSet"
              cluster={cluster}
              namespace={namespace}
              name={`${deploymentName}-postgresql`}
              label={`View postgres StatefulSet on ${cluster}`}
            />
          )}
        </Stack>
      )}
      {testids.length === 0 ? (
        <EmptyContent>No test cases reference this deployment yet.</EmptyContent>
      ) : (
        metrics.map(metric => <MetricsChart key={metric.key} metric={metric.key} testids={testids} />)
      )}
    </Box>
  );
}

/**
 * WorkloadClusterSection: links to every Grafana dashboard for this
 * specific cluster (see GrafanaPage.tsx - each of the 5 built-in
 * dashboards, deployed per workload cluster, not centrally), the raw
 * Prometheus URL cmd/apiserver itself resolves per-testid queries against,
 * and its worker pools' own sizing (fixed replicas vs. cluster-autoscaler
 * min/max) - the one part of a WorkloadCluster's spec Headlamp's generic
 * YAML/table view doesn't summarize at a glance. An inline "Nodes"
 * dashboard embed is the actual "metrics specific to it": node-level
 * CPU/memory/disk is the one thing this whole cluster (not any single
 * benchmark) has of its own to show.
 */
export function WorkloadClusterSection({ name }: { name: string }) {
  const wc = useResource(() => api.workloadCluster(name), [name], 10000);
  const pools = wc.data?.spec.workerPools ?? [];
  const grafanaURL = wc.data?.status?.grafanaURL;
  const prometheusURL = wc.data?.status?.prometheusURL;

  return (
    <Box>
      {pools.length > 0 && (
        <Box sx={{ mb: 2 }}>
          {pools.map(p => (
            <Typography key={p.name} variant="body2">
              <strong>{p.name}</strong>:{' '}
              {p.autoscaling
                ? `autoscaling ${p.autoscaling.minReplicas ?? 0}-${p.autoscaling.maxReplicas} replicas`
                : `${p.replicas} replicas (fixed)`}
            </Typography>
          ))}
        </Box>
      )}
      {prometheusURL && (
        <Typography variant="body2" sx={{ mb: 2 }}>
          Prometheus:{' '}
          <MuiLink href={prometheusURL} target="_blank" rel="noreferrer">
            {prometheusURL}
          </MuiLink>
        </Typography>
      )}
      {!grafanaURL ? (
        <EmptyContent>Grafana not installed on this cluster yet.</EmptyContent>
      ) : (
        <>
          <GrafanaDashboardLinks workloadClusterName={name} />
          <iframe
            title="Grafana - Nodes"
            src={grafanaProxyURL(name, 'akbench-nodes')}
            style={{ width: '100%', height: '60vh', border: 'none' }}
          />
        </>
      )}
    </Box>
  );
}

/**
 * TestRunSection: which TestCase this one-shot run belongs to, a link to
 * its actual k6 Job (a TestRun only ever runs Quick - Burn has no
 * TestRun), and metrics scoped to this specific run's own start/completion
 * window (metricsAnchorFromRuns reused with a single-element array, the
 * same anchoring TestCaseSection uses across a Quick TestCase's whole
 * history, just narrowed to one run).
 */
export function TestRunSection({ name }: { name: string }) {
  const run = useResource(() => api.testRun(name), [name], 5000);
  const testCaseName = run.data?.spec.testCaseRef.name;
  const testCase = useResource(
    () => (testCaseName ? api.testCase(testCaseName) : Promise.resolve(undefined)),
    [testCaseName],
    0
  );
  const deploymentName = testCase.data?.spec.deploymentRef.name;
  const deployment = useResource(
    () => (deploymentName ? api.deployment(deploymentName) : Promise.resolve(undefined)),
    [deploymentName],
    0
  );
  const cluster = headlampClusterName(
    testCase.data?.spec.runnerClusterRef?.name ?? deployment.data?.spec.clusterRef?.name
  );
  const namespace = testCase.data?.metadata.namespace;
  const jobName = run.data?.status?.jobRef?.name;
  const anchor = run.data ? metricsAnchorFromRuns([run.data]) : undefined;
  const metrics = useAvailableMetrics(testCaseName ? [testCaseName] : []);

  return (
    <Box>
      <Stack direction="row" spacing={2} sx={{ mb: 2 }} flexWrap="wrap" alignItems="center">
        {testCaseName && (
          <Typography variant="body2">
            Test case: <strong>{testCaseName}</strong>
          </Typography>
        )}
        {jobName && namespace && (
          <ClusterResourceButton
            kind="Job"
            cluster={cluster}
            namespace={namespace}
            name={jobName}
            label={`View k6 Job on ${cluster}`}
          />
        )}
      </Stack>
      {!anchor ? (
        <EmptyContent>Run still in progress - metrics will show once it completes.</EmptyContent>
      ) : (
        metrics.map(metric => (
          <MetricsChart
            key={metric.key}
            metric={metric.key}
            testids={testCaseName ? [testCaseName] : []}
            endSeconds={anchor.endSeconds}
            rangeSeconds={anchor.rangeSeconds}
          />
        ))
      )}
    </Box>
  );
}

/**
 * TestCaseTemplateSection: which TestCases currently use this template -
 * the one cross-reference Headlamp's generic YAML/status view can't show,
 * since a TestCase names its template, not the other way around.
 */
export function TestCaseTemplateSection({ name }: { name: string }) {
  const testCases = useResource(() => api.testCases(), [], 10000);
  const users = (testCases.data ?? []).filter(tc => tc.spec.template === name);

  if (users.length === 0) {
    return <EmptyContent>No test cases use this template yet.</EmptyContent>;
  }
  return (
    <Stack spacing={0.5}>
      {users.map(tc => (
        <Typography key={tc.metadata.name} variant="body2">
          {tc.metadata.name}
        </Typography>
      ))}
    </Stack>
  );
}
