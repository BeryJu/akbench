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

// These mirror bench.goauthentik.io/v1alpha1 (api/v1alpha1/*_types.go)
// deliberately partially: only the fields this UI renders or offers a guided
// form for. cmd/apiserver passes CRs through untouched and the CRD schema
// stays the one authority on what's valid - every form edits a copy of the
// object it fetched, so a field missing here is preserved on save rather
// than dropped, and every view page also renders the full object as YAML
// (components/ObjectYaml.tsx) so nothing the CRD has is ever hidden.

export interface Condition {
  type: string;
  status: 'True' | 'False' | 'Unknown';
  reason?: string;
  message?: string;
  lastTransitionTime?: string;
}

// Alert is one firing/pending/silenced alert from a workload cluster's own
// Alertmanager (see internal/api/alerts.go - there is no central instance).
export interface Alert {
  cluster: string;
  labels: Record<string, string>;
  annotations: Record<string, string>;
  state: string;
  startsAt: string;
  endsAt?: string;
  generatorURL?: string;
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

export interface AuthentikDeploymentSpec {
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

export interface AuthentikDeploymentStatus {
  conditions?: Condition[];
  phase?: string;
  observedGeneration?: number;
  serviceEndpoint?: string;
  helmReleaseRef?: { name: string };
  cnpgClusterRef?: { name: string };
}

export interface AuthentikDeployment {
  metadata: ObjectMeta;
  spec: AuthentikDeploymentSpec;
  status?: AuthentikDeploymentStatus;
}

export interface TestCaseSpec {
  deploymentRef: { name: string };
  runnerClusterRef?: { name: string };
  template?: string;
  inline?: string;
  // scriptConfigMapRef is a power-user escape hatch (bring-your-own
  // ConfigMap, overrides inline/template) - deliberately not exposed as a
  // TestCaseForm input, but still a real spec field a TestCase can carry.
  scriptConfigMapRef?: { name: string };
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
    observedGeneration?: number;
    burnWorkloadRef?: { name: string };
  };
}

export interface TestCaseTemplateSpec {
  description?: string;
  script: string;
  requiresOAuthClient?: boolean;
}

export interface TestCaseTemplate {
  metadata: ObjectMeta;
  spec: TestCaseTemplateSpec;
  status?: {
    conditions?: Condition[];
    phase?: string;
    observedGeneration?: number;
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
    // resolvedSpec is an audit snapshot of the exact TestCaseSpec +
    // overrides this run actually used (internal/controller/testrun_controller.go
    // sets it) - what a historical run's own "what ran" record is.
    resolvedSpec?: TestCaseSpec;
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

export interface AutoscalingSpec {
  minReplicas?: number;
  maxReplicas: number;
}

export interface WorkerPoolSpec {
  name: string;
  replicas: number;
  incus?: IncusMachineSpec;
  // autoscaling hands this pool's replica count over to cluster-autoscaler
  // (WorkloadClusterReconciler's ensureClusterAutoscaler) - once set,
  // replicas above only sizes the pool at creation time.
  autoscaling?: AutoscalingSpec;
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
  observedGeneration?: number;
  controlPlaneReady?: boolean;
  infrastructureReady?: boolean;
  kubeconfigSecretRef?: { name: string };
  // prometheusURL/grafanaURL are reachable *from the management cluster*
  // (a node's InternalIP + a fixed NodePort - see
  // render.PrometheusNodePort/GrafanaNodePort), populated once
  // ensurePrometheus/ensureGrafana succeed.
  prometheusURL?: string;
  grafanaURL?: string;
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

// PodRangeSeries/PodMetricsResponse mirror internal/api/podmetrics.go -
// per-pod resource usage for a testid's own deployment, not per-testid like
// RangeSeries above (cAdvisor tags by namespace/pod, never by k6 testid).
export interface PodRangeSeries {
  testid: string;
  pod: string;
  points: RangePoint[];
}

export interface PodMetricsResponse {
  resource: string;
  label: string;
  unit: string;
  series: PodRangeSeries[];
}

/** readyCondition is the one condition every list view renders. */
export function readyCondition(conditions?: Condition[]): Condition | undefined {
  return conditions?.find(c => c.type === 'Ready');
}
