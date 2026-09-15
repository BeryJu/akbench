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

import { beforeEach, describe, expect, it, vi } from 'vitest';

// api.ts talks to cmd/apiserver exclusively through Headlamp's own
// ApiProxy.request - mocked here rather than exercised for real, both
// because the real one needs a running Headlamp backend/browser session,
// and because a unit test for this file should verify the URL/method/body
// this module builds, not ApiProxy's own proxying behavior.
const requestMock = vi.fn();
vi.mock('@kinvolk/headlamp-plugin/lib', () => ({
  ApiProxy: { request: (...args: unknown[]) => requestMock(...args) },
}));

import { api, headlampClusterName, MANAGEMENT_CLUSTER } from './api';

describe('headlampClusterName', () => {
  it('maps the management cluster (undefined clusterRef) to MANAGEMENT_CLUSTER', () => {
    expect(headlampClusterName(undefined)).toBe(MANAGEMENT_CLUSTER);
  });

  it('maps a WorkloadCluster name to its "<name>-admin@<name>" kubeconfig context', () => {
    // Not just "<name>" - HeadlampKubeconfigReconciler's merged kubeconfig
    // names the context this way, and Headlamp shows the context name, not
    // the raw cluster entry name (confirmed live - see this function's own
    // doc comment in api.ts).
    expect(headlampClusterName('default-dev-vm-incus')).toBe(
      'default-dev-vm-incus-admin@default-dev-vm-incus'
    );
  });
});

describe('api request construction', () => {
  beforeEach(() => {
    requestMock.mockReset();
    requestMock.mockResolvedValue({ items: [] });
  });

  it('always pins requests to the management cluster, not whichever cluster is selected', () => {
    api.deployments();
    const [, opts] = requestMock.mock.calls[0];
    expect(opts.cluster).toBe(MANAGEMENT_CLUSTER);
  });

  it('proxies through cmd/apiserver Service via the k8s API service-proxy subresource', () => {
    api.deployments();
    const [path] = requestMock.mock.calls[0];
    expect(path).toBe(
      '/api/v1/namespaces/authentik-bench/services/apiserver:8080/proxy/api/v1/deployments'
    );
  });

  it('sends the dev bearer token in the header cmd/apiserver expects', () => {
    api.deployments();
    const [, opts] = requestMock.mock.calls[0];
    expect(opts.headers['X-Bench-Api-Token']).toBe('dev-token');
  });

  it('builds testRuns() with no filter when no test case is given', () => {
    api.testRuns();
    const [path] = requestMock.mock.calls[0];
    expect(path).toContain('/api/v1/testruns');
    expect(path).not.toContain('labelSelector');
  });

  it('builds testRuns(testCase) with an encoded labelSelector', () => {
    api.testRuns('login-main');
    const [path] = requestMock.mock.calls[0];
    expect(path).toContain(
      `labelSelector=${encodeURIComponent('bench.goauthentik.io/testcase=login-main')}`
    );
  });

  it('builds compare() without a time param when atSeconds is omitted', () => {
    api.compare(['a', 'b'], '5m');
    const [path] = requestMock.mock.calls[0];
    expect(path).toContain('/api/v1/metrics/compare?testids=a%2Cb&window=5m');
    expect(path).not.toContain('time=');
  });

  it('builds compare() with an anchored time param when atSeconds is given', () => {
    api.compare(['a', 'b'], '5m', 12345);
    const [path] = requestMock.mock.calls[0];
    expect(path).toContain('/api/v1/metrics/compare?testids=a%2Cb&window=5m&time=12345');
  });

  it('builds metricsRange() with all range params encoded', () => {
    api.metricsRange(['a'], 'iterations', 100, 200, '15s');
    const [path] = requestMock.mock.calls[0];
    expect(path).toContain(
      '/api/v1/metrics/range?testids=a&metric=iterations&start=100&end=200&step=15s'
    );
  });

  it('sends DELETE with the right method for deleteTestRun', () => {
    api.deleteTestRun('run-1');
    const [path, opts] = requestMock.mock.calls[0];
    expect(path).toContain('/api/v1/testruns/run-1');
    expect(opts.method).toBe('DELETE');
  });

  it('JSON-encodes the body for createTestCase', () => {
    const spec = { deploymentRef: { name: 'd1' } } as any;
    api.createTestCase({ metadata: { name: 'tc1' }, spec });
    const [, opts] = requestMock.mock.calls[0];
    expect(opts.method).toBe('POST');
    expect(JSON.parse(opts.body)).toEqual({ metadata: { name: 'tc1' }, spec });
  });
});
