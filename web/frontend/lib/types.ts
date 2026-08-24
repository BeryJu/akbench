// Shapes mirroring bench.goauthentik.io/v1alpha1, kept deliberately partial:
// the backend passes CRs through untouched, so anything not typed here still
// round-trips, and the CRD schema on the API server stays the one authority on
// what is valid.

export interface Condition {
  type: string;
  status: "True" | "False" | "Unknown";
  reason?: string;
  message?: string;
  lastTransitionTime?: string;
}

export interface ObjectMeta {
  name: string;
  namespace?: string;
  generateName?: string;
  creationTimestamp?: string;
  labels?: Record<string, string>;
}

export type PostgresMode = "Embedded" | "CNPG" | "External";
export type PlacementMode = "Default" | "Dedicated";
export type TestProfile = "Quick" | "Burn";

export interface ResourceRequirements {
  requests?: Record<string, string>;
  limits?: Record<string, string>;
}

export interface WorkloadSpec {
  replicas: number;
  resources?: ResourceRequirements;
}

export interface TopologySpec {
  clusterRef?: { name: string };
  targetName?: string;
  namespace: string;
  chart?: { repo?: string; version?: string };
  server: WorkloadSpec;
  worker: WorkloadSpec;
  placement?: {
    mode?: PlacementMode;
    nodeSelector?: Record<string, string>;
    topologyKey?: string;
  };
  postgres: {
    mode: PostgresMode;
    embedded?: { storageSize?: string; storageClassName?: string };
    cnpg?: { instances?: number; storageSize?: string; storageClassName?: string };
    external?: { secretRef: { name: string }; sslMode?: string };
  };
  authentik?: {
    image?: string;
    tag?: string;
    logLevel?: string;
  };
}

export interface TopologyStatus {
  conditions?: Condition[];
  serviceEndpoint?: string;
  helmReleaseRef?: { name: string };
  cnpgClusterRef?: { name: string };
}

export interface Topology {
  metadata: ObjectMeta;
  spec: TopologySpec;
  status?: TopologyStatus;
}

export interface TestCaseSpec {
  topologyRef: { name: string };
  runnerClusterRef?: { name: string };
  script?: string;
  profile: TestProfile;
  vus: number;
  iterations?: number;
  flow?: string;
  quickDuration?: string;
  burnDuration?: string;
  traceSampleRate?: string;
  extraTags?: Record<string, string>;
}

export interface TestCase {
  metadata: ObjectMeta;
  spec: TestCaseSpec;
  status?: {
    conditions?: Condition[];
    burnWorkloadRef?: { name: string };
  };
}

export type TestRunPhase = "Pending" | "Running" | "Succeeded" | "Failed";

export interface TestRun {
  metadata: ObjectMeta;
  spec: {
    testCaseRef: { name: string };
    overrides?: { vus?: number; iterations?: number; flow?: string };
  };
  status?: {
    phase?: TestRunPhase;
    startTime?: string;
    completionTime?: string;
    jobRef?: { name: string };
    resolvedSpec?: TestCaseSpec;
    conditions?: Condition[];
  };
}

export interface Cluster {
  name: string;
  management: boolean;
  phase?: string;
  controlPlaneReady: boolean;
  infrastructureReady: boolean;
}

export interface CompareMetric {
  key: string;
  label: string;
  unit: string;
  better: "higher" | "lower";
}

export interface CompareResponse {
  window: string;
  metrics: CompareMetric[];
  results: { testid: string; values: Record<string, number> }[];
}

/** readyCondition is the one condition every list view renders. */
export function readyCondition(conditions?: Condition[]): Condition | undefined {
  return conditions?.find((c) => c.type === "Ready");
}
