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

// Ported from the deleted web/frontend/lib/api.ts. That version's request()
// went through a Next.js server-side route handler that added the bearer
// token out of the browser's reach; this one instead goes through
// Headlamp's own ApiProxy.request(), which proxies through the
// already-authenticated Kubernetes API server to cmd/apiserver's own
// in-cluster Service via the standard
// /api/v1/namespaces/{ns}/services/{name}:{port}/proxy/ subresource -
// verified live end-to-end (see internal/api/auth.go and
// hack/dev-apiserver.yaml) before writing any of this.
//
// cmd/apiserver always lives on the management cluster, regardless of which
// cluster the user currently has selected in Headlamp - every call
// explicitly pins { cluster: MANAGEMENT_CLUSTER } rather than relying on
// ApiProxy's "current URL cluster" default, which would otherwise silently
// send requests to whichever workload cluster is currently being viewed.
//
// Every method/path here is checked directly against internal/api/server.go's
// route table (and clusters.go/resources.go/extravalues.go/runs.go's
// handlers) rather than assumed, since a wrong path fails silently behind
// ApiProxy's own error formatting (see the "Service Unavailable - undefined"
// bug this session already hit once for a related reason).
import { ApiProxy } from '@kinvolk/headlamp-plugin/lib';
import {
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

// Must match HeadlampKubeconfigReconciler's managementContextName
// (internal/render/headlampkubeconfig.go).
export const MANAGEMENT_CLUSTER = 'management';

// headlampClusterName maps a WorkloadCluster's own object name (what
// AuthentikDeployment.spec.clusterRef/TestCase.spec.runnerClusterRef actually
// store) to the cluster identifier Headlamp shows in its own switcher and
// expects in a route's `cluster` param. These differ: CAPI's generated
// kubeconfig (internal/render/headlampkubeconfig.go merges it in verbatim,
// no renaming) names the cluster entry after the WorkloadCluster
// (`<name>`), but its context is `<name>-admin@<name>` - and Headlamp
// derives the cluster name it shows from the context, not the raw cluster
// entry, so every workload cluster is reachable as `<name>-admin`, not
// `<name>`, confirmed against a live merged kubeconfig.
export function headlampClusterName(clusterRefName: string | undefined): string {
  return clusterRefName ? `${clusterRefName}-admin@${clusterRefName}` : MANAGEMENT_CLUSTER;
}

// Must match hack/dev-apiserver.yaml's namespace/Service name/port.
const APISERVER_PROXY_PATH =
  '/api/v1/namespaces/authentik-bench/services/apiserver:8080/proxy';

// Matches cmd/apiserver's own BENCH_API_TOKEN (hack/dev-apiserver.yaml's
// apiserver-token Secret) - dev-only fixed value, same caveat as that
// Secret's own comment.
const API_TOKEN = 'dev-token';

// Matches internal/api/auth.go's tokenHeader: Authorization is overwritten
// by Headlamp's own proxy layer with its own Kubernetes bearer token before
// the request reaches the proxied service, so the token has nowhere else to
// go but a header of our own.
const TOKEN_HEADER = 'X-Bench-Api-Token';

export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.status = status;
    this.name = 'ApiError';
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  try {
    return (await ApiProxy.request(
      `${APISERVER_PROXY_PATH}${path}`,
      {
        ...init,
        headers: {
          'Content-Type': 'application/json',
          [TOKEN_HEADER]: API_TOKEN,
          ...(init?.headers as Record<string, string> | undefined),
        },
        cluster: MANAGEMENT_CLUSTER,
      },
      false,
      true
    )) as T;
  } catch (err: any) {
    // ApiProxy.request throws ApiError (from lib/k8s/api/v2/ApiError) with
    // its own shape - normalize to this module's own ApiError so callers
    // (useResource) only need to handle one type.
    const status = typeof err?.status === 'number' ? err.status : 0;
    const message = err?.message ?? String(err);
    throw new ApiError(status, message);
  }
}

interface List<T> {
  items: T[];
}

interface ObjectInput<Spec> {
  metadata: { name?: string; generateName?: string; labels?: Record<string, string> };
  spec: Spec;
}

export const api = {
  // -- Clusters: internal/api/clusters.go's own /clusters convenience
  // endpoints - listClusters returns the management cluster plus every CAPI
  // Cluster (for pickers); registerCluster/deleteCluster create/delete the
  // underlying WorkloadCluster CRD (which WorkloadClusterReconciler then
  // provisions/tears down for real).
  clusters: () => request<List<Cluster>>('/api/v1/clusters').then(r => r.items ?? []),
  registerCluster: (obj: ObjectInput<WorkloadClusterSpec>) =>
    request<WorkloadCluster>('/api/v1/clusters', { method: 'POST', body: JSON.stringify(obj) }),
  deprovisionCluster: (name: string) =>
    request<void>(`/api/v1/clusters/${name}`, { method: 'DELETE' }),

  // -- WorkloadClusters: the CRD itself, at the generic resourceKinds route -
  // used to read/edit a cluster's full spec (registerCluster/deleteCluster
  // above only take a name + spec on the way in/out).
  workloadCluster: (name: string) => request<WorkloadCluster>(`/api/v1/workloadclusters/${name}`),
  updateWorkloadCluster: (name: string, obj: { spec: WorkloadClusterSpec }) =>
    request<WorkloadCluster>(`/api/v1/workloadclusters/${name}`, {
      method: 'PUT',
      body: JSON.stringify(obj),
    }),

  // -- Deployments
  deployments: () => request<List<AuthentikDeployment>>('/api/v1/deployments').then(r => r.items ?? []),
  deployment: (name: string) => request<AuthentikDeployment>(`/api/v1/deployments/${name}`),
  createDeployment: (obj: ObjectInput<AuthentikDeploymentSpec>) =>
    request<AuthentikDeployment>('/api/v1/deployments', { method: 'POST', body: JSON.stringify(obj) }),
  updateDeployment: (name: string, obj: { spec: AuthentikDeploymentSpec }) =>
    request<AuthentikDeployment>(`/api/v1/deployments/${name}`, { method: 'PUT', body: JSON.stringify(obj) }),
  deleteDeployment: (name: string) => request<void>(`/api/v1/deployments/${name}`, { method: 'DELETE' }),
  getExtraValues: (name: string) => request<{ valuesYAML: string }>(`/api/v1/deployments/${name}/extra-values`),
  setExtraValues: (name: string, valuesYAML: string) =>
    request<{ valuesYAML: string }>(`/api/v1/deployments/${name}/extra-values`, {
      method: 'PUT',
      body: JSON.stringify({ valuesYAML }),
    }),

  // -- Test cases
  testCases: () => request<List<TestCase>>('/api/v1/testcases').then(r => r.items ?? []),
  testCase: (name: string) => request<TestCase>(`/api/v1/testcases/${name}`),
  createTestCase: (obj: ObjectInput<TestCaseSpec>) =>
    request<TestCase>('/api/v1/testcases', { method: 'POST', body: JSON.stringify(obj) }),
  updateTestCase: (name: string, obj: { spec: TestCaseSpec }) =>
    request<TestCase>(`/api/v1/testcases/${name}`, { method: 'PUT', body: JSON.stringify(obj) }),
  deleteTestCase: (name: string) => request<void>(`/api/v1/testcases/${name}`, { method: 'DELETE' }),
  runTestCase: (name: string) =>
    request<TestRun>(`/api/v1/testcases/${name}/runs`, { method: 'POST', body: JSON.stringify({}) }),

  // -- Test case templates
  testCaseTemplates: () =>
    request<List<TestCaseTemplate>>('/api/v1/testcasetemplates').then(r => r.items ?? []),
  testCaseTemplate: (name: string) => request<TestCaseTemplate>(`/api/v1/testcasetemplates/${name}`),
  createTestCaseTemplate: (obj: ObjectInput<TestCaseTemplateSpec>) =>
    request<TestCaseTemplate>('/api/v1/testcasetemplates', { method: 'POST', body: JSON.stringify(obj) }),
  updateTestCaseTemplate: (name: string, obj: { spec: TestCaseTemplateSpec }) =>
    request<TestCaseTemplate>(`/api/v1/testcasetemplates/${name}`, {
      method: 'PUT',
      body: JSON.stringify(obj),
    }),
  deleteTestCaseTemplate: (name: string) =>
    request<void>(`/api/v1/testcasetemplates/${name}`, { method: 'DELETE' }),

  // -- Test runs
  testRuns: (testCase?: string) =>
    request<List<TestRun>>(
      testCase
        ? `/api/v1/testruns?labelSelector=${encodeURIComponent(`bench.goauthentik.io/testcase=${testCase}`)}`
        : '/api/v1/testruns'
    ).then(r => r.items ?? []),
  deleteTestRun: (name: string) => request<void>(`/api/v1/testruns/${name}`, { method: 'DELETE' }),

  // -- Metrics
  // atSeconds anchors the instant query to a specific unix time instead of
  // "now" (internal/api/metrics.go's own `time` param, forwarded straight to
  // Prometheus) - needed for a Quick TestCase, whose k6 Job has already
  // exited by the time anyone looks: an instant query always evaluated at
  // "now" finds nothing once more than the summary window has elapsed since
  // the run completed. See ComparePage.tsx/DetailSections.tsx for how the
  // anchor is resolved from a TestRun's own completionTime.
  compare: (testids: string[], window: string, atSeconds?: number) =>
    request<CompareResponse>(
      `/api/v1/metrics/compare?testids=${encodeURIComponent(testids.join(','))}&window=${encodeURIComponent(window)}` +
        (atSeconds !== undefined ? `&time=${atSeconds}` : '')
    ),
  metricsRange: (
    testids: string[],
    metric: string,
    startSeconds: number,
    endSeconds: number,
    step: string
  ) =>
    request<RangeResponse>(
      `/api/v1/metrics/range?testids=${encodeURIComponent(testids.join(','))}` +
        `&metric=${encodeURIComponent(metric)}&start=${startSeconds}&end=${endSeconds}` +
        `&step=${encodeURIComponent(step)}`
    ),
};
