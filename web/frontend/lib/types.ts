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
export type PlacementMode = "Default" | "Dedicated" | "Packed";
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
    // extraValuesRef names the ConfigMap the extra-values YAML editor
    // (topology-form's "Advanced" section) reads and writes via
    // /topologies/{name}/extra-values - set by that endpoint, not directly
    // by the form.
    extraValuesRef?: { name: string };
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

// InfrastructureProvider mirrors bench.goauthentik.io's own enum: Incus is
// the only one WorkloadClusterReconciler actually provisions today, but the
// field is a picker (not hardcoded) so the UI doesn't need to change shape
// when a second provider lands - it'll just get another option and another
// provider-specific sub-form, the same way postgres.mode already works.
export type InfrastructureProvider = "Incus";

export interface MachineSpec {
  replicas: number;
}

export interface IncusMachineSpec {
  flavor?: string;
}

export interface IncusProviderSpec {
  lxcSecretRef: { name: string };
  image?: string;
  profiles?: string[];
  controlPlane?: IncusMachineSpec;
  workers?: IncusMachineSpec;
}

export interface WorkloadClusterSpec {
  kubernetesVersion: string;
  provider: InfrastructureProvider;
  controlPlane: MachineSpec;
  workers: MachineSpec;
  incus?: IncusProviderSpec;
}

export interface WorkloadClusterStatus {
  conditions?: Condition[];
  phase?: string;
  controlPlaneReady?: boolean;
  infrastructureReady?: boolean;
  kubeconfigSecretRef?: { name: string };
}

export interface WorkloadCluster {
  metadata: ObjectMeta;
  spec: WorkloadClusterSpec;
  status?: WorkloadClusterStatus;
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

export interface RangePoint {
  time: number;
  value: number;
}

export interface RangeSeries {
  testid: string;
  points: RangePoint[];
}

export interface RangeResponse {
  metric: CompareMetric;
  series: RangeSeries[];
}

/** readyCondition is the one condition every list view renders. */
export function readyCondition(conditions?: Condition[]): Condition | undefined {
  return conditions?.find((c) => c.type === "Ready");
}
