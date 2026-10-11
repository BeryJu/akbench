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

// Thin fetch wrapper over cmd/apiserver's REST API (internal/api/server.go's
// route table). The app is served by cmd/apiserver itself, so every path is
// same-origin and the credential is a plain Authorization: Bearer header -
// either the static API token or an OIDC access token, see auth.tsx.
import {
  Alert,
  AuthentikDeployment,
  AuthentikDeploymentSpec,
  Cluster,
  CompareResponse,
  RangeResponse,
  TestCase,
  TestCaseSpec,
  TestCaseTemplate,
  TestCaseTemplateSpec,
  TestRun,
  WorkloadCluster,
  WorkloadClusterSpec,
} from './types';

// Must match internal/api/clusters.go's ManagementClusterName - also the
// merged kubeconfig's context name for the management cluster.
export const MANAGEMENT_CLUSTER = 'management';

export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.status = status;
    this.name = 'ApiError';
  }
}

type TokenSource = () => Promise<string | undefined>;

let tokenSource: TokenSource = async () => undefined;
let onUnauthorized: () => void = () => {};

/** configureAuth is called once by AuthProvider. */
export function configureAuth(source: TokenSource, unauthorized: () => void) {
  tokenSource = source;
  onUnauthorized = unauthorized;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const token = await tokenSource();
  const res = await fetch(path, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(init?.headers as Record<string, string> | undefined),
    },
  });
  if (res.status === 401) {
    onUnauthorized();
  }
  if (!res.ok) {
    let message = `${res.status} ${res.statusText}`;
    try {
      const body = (await res.json()) as { error?: string };
      if (body.error) {
        message = body.error;
      }
    } catch {
      // Not every failure has the API's own JSON error body (a proxy in
      // between, say) - the status line is the best there is then.
    }
    throw new ApiError(res.status, message);
  }
  if (res.status === 204) {
    return undefined as T;
  }
  return (await res.json()) as T;
}

interface List<T> {
  items: T[] | null;
}

interface ObjectInput<Spec> {
  metadata: { name?: string; generateName?: string; labels?: Record<string, string> };
  spec: Spec;
}

const json = (method: string, body: unknown): RequestInit => ({ method, body: JSON.stringify(body) });
const seg = encodeURIComponent;

export interface AuthConfig {
  oidc?: { issuer: string; clientId: string; scopes: string[] };
}

export interface ShellContext {
  name: string;
  cluster: string;
}

export const api = {
  authConfig: () => request<AuthConfig>('/api/v1/auth/config'),

  // -- Clusters: internal/api/clusters.go's convenience endpoints - the
  // management cluster plus every CAPI Cluster (for pickers);
  // register/deprovision create/delete the WorkloadCluster CRD.
  clusters: () => request<List<Cluster>>('/api/v1/clusters').then(r => r.items ?? []),
  registerCluster: (obj: ObjectInput<WorkloadClusterSpec>) =>
    request<WorkloadCluster>('/api/v1/clusters', json('POST', obj)),
  deprovisionCluster: (name: string) => request<void>(`/api/v1/clusters/${seg(name)}`, { method: 'DELETE' }),
  workloadClusters: () =>
    request<List<WorkloadCluster>>('/api/v1/workloadclusters').then(r => r.items ?? []),
  workloadCluster: (name: string) => request<WorkloadCluster>(`/api/v1/workloadclusters/${seg(name)}`),
  updateWorkloadCluster: (name: string, obj: { spec: WorkloadClusterSpec }) =>
    request<WorkloadCluster>(`/api/v1/workloadclusters/${seg(name)}`, json('PUT', obj)),
  kickWorkloadCluster: (name: string) =>
    request<WorkloadCluster>(`/api/v1/workloadclusters/${seg(name)}/reconcile`, json('POST', {})),

  // -- Deployments
  deployments: () => request<List<AuthentikDeployment>>('/api/v1/deployments').then(r => r.items ?? []),
  deployment: (name: string) => request<AuthentikDeployment>(`/api/v1/deployments/${seg(name)}`),
  createDeployment: (obj: ObjectInput<AuthentikDeploymentSpec>) =>
    request<AuthentikDeployment>('/api/v1/deployments', json('POST', obj)),
  updateDeployment: (name: string, obj: { spec: AuthentikDeploymentSpec }) =>
    request<AuthentikDeployment>(`/api/v1/deployments/${seg(name)}`, json('PUT', obj)),
  deleteDeployment: (name: string) => request<void>(`/api/v1/deployments/${seg(name)}`, { method: 'DELETE' }),
  kickDeployment: (name: string) =>
    request<AuthentikDeployment>(`/api/v1/deployments/${seg(name)}/reconcile`, json('POST', {})),
  getExtraValues: (name: string) =>
    request<{ valuesYAML: string }>(`/api/v1/deployments/${seg(name)}/extra-values`),
  setExtraValues: (name: string, valuesYAML: string) =>
    request<{ valuesYAML: string }>(`/api/v1/deployments/${seg(name)}/extra-values`, json('PUT', { valuesYAML })),

  // -- Test cases
  testCases: () => request<List<TestCase>>('/api/v1/testcases').then(r => r.items ?? []),
  testCase: (name: string) => request<TestCase>(`/api/v1/testcases/${seg(name)}`),
  createTestCase: (obj: ObjectInput<TestCaseSpec>) => request<TestCase>('/api/v1/testcases', json('POST', obj)),
  updateTestCase: (name: string, obj: { spec: TestCaseSpec }) =>
    request<TestCase>(`/api/v1/testcases/${seg(name)}`, json('PUT', obj)),
  deleteTestCase: (name: string) => request<void>(`/api/v1/testcases/${seg(name)}`, { method: 'DELETE' }),
  // One TestRun per deployment the TestCase drives (see testCaseDeployments) -
  // always an array, even for today's common single-deployment case.
  runTestCase: (name: string) => request<TestRun[]>(`/api/v1/testcases/${seg(name)}/runs`, json('POST', {})),

  // -- Test case templates
  testCaseTemplates: () =>
    request<List<TestCaseTemplate>>('/api/v1/testcasetemplates').then(r => r.items ?? []),
  testCaseTemplate: (name: string) => request<TestCaseTemplate>(`/api/v1/testcasetemplates/${seg(name)}`),
  createTestCaseTemplate: (obj: ObjectInput<TestCaseTemplateSpec>) =>
    request<TestCaseTemplate>('/api/v1/testcasetemplates', json('POST', obj)),
  updateTestCaseTemplate: (name: string, obj: { spec: TestCaseTemplateSpec }) =>
    request<TestCaseTemplate>(`/api/v1/testcasetemplates/${seg(name)}`, json('PUT', obj)),
  deleteTestCaseTemplate: (name: string) =>
    request<void>(`/api/v1/testcasetemplates/${seg(name)}`, { method: 'DELETE' }),

  // -- Test runs (read-only: a TestRun is only ever created via runTestCase)
  testRuns: (testCase?: string) =>
    request<List<TestRun>>(
      testCase
        ? `/api/v1/testruns?labelSelector=${seg(`bench.goauthentik.io/testcase=${testCase}`)}`
        : '/api/v1/testruns'
    ).then(r => r.items ?? []),
  testRun: (name: string) => request<TestRun>(`/api/v1/testruns/${seg(name)}`),
  deleteTestRun: (name: string) => request<void>(`/api/v1/testruns/${seg(name)}`, { method: 'DELETE' }),

  // -- Metrics. atSeconds anchors the instant query to a unix time instead
  // of "now" - see format.ts's metricsAnchorFromRuns for why a Quick
  // TestCase needs that.
  compare: (testids: string[], window: string, atSeconds?: number) =>
    request<CompareResponse>(
      `/api/v1/metrics/compare?testids=${seg(testids.join(','))}&window=${seg(window)}` +
        (atSeconds !== undefined ? `&time=${atSeconds}` : '')
    ),
  metricsRange: (testids: string[], metric: string, startSeconds: number, endSeconds: number, step: string) =>
    request<RangeResponse>(
      `/api/v1/metrics/range?testids=${seg(testids.join(','))}` +
        `&metric=${seg(metric)}&start=${startSeconds}&end=${endSeconds}&step=${seg(step)}`
    ),
  // CPU/memory for each testid's own deployment, summed across its pods -
  // scoped by namespace, not by a testid label the way metricsRange's
  // queries are (cAdvisor never tags container metrics with a k6 testid).
  // Same RangeResponse shape as metricsRange (see internal/api/podmetrics.go).
  resourceRange: (testids: string[], resource: 'cpu' | 'memory', startSeconds: number, endSeconds: number, step: string) =>
    request<RangeResponse>(
      `/api/v1/metrics/resources?testids=${seg(testids.join(','))}` +
        `&resource=${seg(resource)}&start=${startSeconds}&end=${endSeconds}&step=${seg(step)}`
    ),

  // -- Alerts (internal/api/alerts.go) - aggregated across every workload
  // cluster's own Alertmanager, there is no central instance.
  alerts: () => request<List<Alert>>('/api/v1/alerts').then(r => r.items ?? []),

  // -- Shell (internal/api/shell.go)
  shellContexts: () => request<List<ShellContext>>('/api/v1/shell/contexts').then(r => r.items ?? []),
  createShellSession: (context: string) =>
    request<{ ticket: string; expiresAt: string }>('/api/v1/shell/sessions', json('POST', { context })),
};
