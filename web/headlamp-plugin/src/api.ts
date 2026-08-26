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
  Cluster,
  CompareResponse,
  RangeResponse,
  TestCase,
  TestCaseSpec,
  TestRun,
  Topology,
  TopologySpec,
  WorkloadCluster,
  WorkloadClusterSpec,
} from './types';

// Must match HeadlampKubeconfigReconciler's managementContextName
// (internal/render/headlampkubeconfig.go).
const MANAGEMENT_CLUSTER = 'management';

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

  // -- Topologies
  topologies: () => request<List<Topology>>('/api/v1/topologies').then(r => r.items ?? []),
  topology: (name: string) => request<Topology>(`/api/v1/topologies/${name}`),
  createTopology: (obj: ObjectInput<TopologySpec>) =>
    request<Topology>('/api/v1/topologies', { method: 'POST', body: JSON.stringify(obj) }),
  updateTopology: (name: string, obj: { spec: TopologySpec }) =>
    request<Topology>(`/api/v1/topologies/${name}`, { method: 'PUT', body: JSON.stringify(obj) }),
  deleteTopology: (name: string) => request<void>(`/api/v1/topologies/${name}`, { method: 'DELETE' }),
  getExtraValues: (name: string) => request<{ valuesYAML: string }>(`/api/v1/topologies/${name}/extra-values`),
  setExtraValues: (name: string, valuesYAML: string) =>
    request<{ valuesYAML: string }>(`/api/v1/topologies/${name}/extra-values`, {
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

  // -- Test runs
  testRuns: (testCase?: string) =>
    request<List<TestRun>>(
      testCase
        ? `/api/v1/testruns?labelSelector=${encodeURIComponent(`bench.goauthentik.io/testcase=${testCase}`)}`
        : '/api/v1/testruns'
    ).then(r => r.items ?? []),
  deleteTestRun: (name: string) => request<void>(`/api/v1/testruns/${name}`, { method: 'DELETE' }),

  // -- Metrics
  compare: (testids: string[], window: string) =>
    request<CompareResponse>(
      `/api/v1/metrics/compare?testids=${encodeURIComponent(testids.join(','))}&window=${encodeURIComponent(window)}`
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
