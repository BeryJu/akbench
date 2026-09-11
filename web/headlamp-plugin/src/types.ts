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

// Ported from the deleted web/frontend/lib/types.ts, unchanged - these
// mirror bench.goauthentik.io/v1alpha1 deliberately partially, since
// cmd/apiserver passes CRs through untouched and the CRD schema stays the
// one authority on what's valid.

export interface Condition {
  type: string;
  status: 'True' | 'False' | 'Unknown';
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

export type PostgresMode = 'Embedded' | 'CNPG' | 'External';
export type PlacementMode = 'Default' | 'Dedicated' | 'Packed';
export type TestProfile = 'Quick' | 'Burn';

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
    errorReporting?: { enabled?: boolean; tracingEndpoint?: string; sampleRate?: string };
    pyroscopeHost?: string;
    extraValuesRef?: { name: string };
  };
}

export interface TopologyStatus {
  conditions?: Condition[];
  phase?: string;
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
    phase?: string;
    burnWorkloadRef?: { name: string };
  };
}

export type TestRunPhase = 'Pending' | 'Running' | 'Succeeded' | 'Failed';

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

export type InfrastructureProvider = 'Incus';

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
}

export interface WorkerPoolSpec {
  name: string;
  replicas: number;
  incus?: IncusMachineSpec;
}

export interface WorkloadClusterSpec {
  kubernetesVersion: string;
  provider: InfrastructureProvider;
  controlPlane: MachineSpec;
  workerPools: WorkerPoolSpec[];
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
  better: 'higher' | 'lower';
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
  return conditions?.find(c => c.type === 'Ready');
}
