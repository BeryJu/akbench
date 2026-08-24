// Every call goes through the Next.js route handler at /api/bench, which adds
// the bearer token server-side. The token therefore never reaches the browser,
// which matters more than usual here: it is a single shared secret with full
// write access to every benchmark CRD.

import type {
  Cluster,
  CompareResponse,
  TestCase,
  TestRun,
  Topology,
} from "./types";

export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.status = status;
    this.name = "ApiError";
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`/api/bench${path}`, {
    ...init,
    headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
    cache: "no-store",
  });
  if (!response.ok) {
    let message = response.statusText;
    try {
      const body = (await response.json()) as { error?: string };
      if (body.error) {
        message = body.error;
      }
    } catch {
      // A non-JSON body means the failure came from somewhere other than the
      // backend - the status line is the best description available.
    }
    throw new ApiError(response.status, message);
  }
  if (response.status === 204) {
    return undefined as T;
  }
  return (await response.json()) as T;
}

interface List<T> {
  items: T[];
}

export const api = {
  clusters: () => request<List<Cluster>>("/clusters").then((r) => r.items ?? []),

  topologies: () => request<List<Topology>>("/topologies").then((r) => r.items ?? []),
  topology: (name: string) => request<Topology>(`/topologies/${name}`),
  createTopology: (body: Partial<Topology>) =>
    request<Topology>("/topologies", { method: "POST", body: JSON.stringify(body) }),
  updateTopology: (name: string, body: Partial<Topology>) =>
    request<Topology>(`/topologies/${name}`, { method: "PUT", body: JSON.stringify(body) }),
  deleteTopology: (name: string) => request<void>(`/topologies/${name}`, { method: "DELETE" }),

  testCases: () => request<List<TestCase>>("/testcases").then((r) => r.items ?? []),
  testCase: (name: string) => request<TestCase>(`/testcases/${name}`),
  createTestCase: (body: Partial<TestCase>) =>
    request<TestCase>("/testcases", { method: "POST", body: JSON.stringify(body) }),
  updateTestCase: (name: string, body: Partial<TestCase>) =>
    request<TestCase>(`/testcases/${name}`, { method: "PUT", body: JSON.stringify(body) }),
  deleteTestCase: (name: string) => request<void>(`/testcases/${name}`, { method: "DELETE" }),
  runTestCase: (name: string, overrides?: Record<string, unknown>) =>
    request<TestRun>(`/testcases/${name}/runs`, {
      method: "POST",
      body: JSON.stringify(overrides ? { overrides } : {}),
    }),

  testRuns: (testCase?: string) =>
    request<List<TestRun>>(
      testCase
        ? `/testruns?labelSelector=${encodeURIComponent(`bench.goauthentik.io/testcase=${testCase}`)}`
        : "/testruns",
    ).then((r) => r.items ?? []),
  deleteTestRun: (name: string) => request<void>(`/testruns/${name}`, { method: "DELETE" }),

  compare: (testids: string[], window: string) =>
    request<CompareResponse>(
      `/metrics/compare?testids=${encodeURIComponent(testids.join(","))}&window=${encodeURIComponent(window)}`,
    ),
  embedURL: (dashboard: string, vars: string[], from: string) =>
    request<{ url: string }>(
      `/grafana/embed-url?dashboard=${encodeURIComponent(dashboard)}` +
        `&vars=${encodeURIComponent(vars.join(","))}&from=${encodeURIComponent(from)}`,
    ),
};
