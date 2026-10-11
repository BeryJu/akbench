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

import { FormEvent, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router';
import { api, MANAGEMENT_CLUSTER } from '../api';
import { ObjectYaml } from '../components/ObjectYaml';
import { Badge, ConditionList, StatusBadge } from '../components/Status';
import { Alert, confirmAndRun, errorMessage, Field, Page, Table } from '../components/ui';
import { shellLinkForCluster } from '../shell/links';
import { WorkerPoolSpec, WorkloadCluster, WorkloadClusterSpec } from '../types';
import { useResource } from '../useResource';

export function ClustersPage() {
  const clusters = useResource(() => api.clusters());
  return (
    <Page
      title="Clusters"
      subtitle="Where deployments and test runners run: the management cluster plus every provisioned workload cluster."
      actions={
        <Link className="button primary" to="/clusters/new">
          Register cluster
        </Link>
      }
    >
      <Alert>{clusters.error}</Alert>
      <Table
        rows={clusters.data}
        rowKey={c => c.name}
        empty="No clusters."
        columns={[
          { label: 'Name', render: c => <Link to={`/clusters/${c.name}`}>{c.name}</Link> },
          { label: 'Role', render: c => (c.management ? 'Management' : 'Workload') },
          { label: 'Phase', render: c => <Badge value={c.phase} /> },
          { label: 'Control plane', render: c => <Badge value={c.controlPlaneReady ? 'Ready' : 'Not ready'} /> },
          { label: 'Infrastructure', render: c => <Badge value={c.infrastructureReady ? 'Ready' : 'Not ready'} /> },
          {
            label: '',
            className: 'actions',
            render: c => (
              <span className="row" style={{ justifyContent: 'flex-end' }}>
                <Link className="button small" to={shellLinkForCluster(c.management ? undefined : c.name)}>
                  Shell
                </Link>
                {!c.management && (
                  <>
                    <button
                      className="small"
                      title="Nudge the operator into reconciling this cluster right now, instead of waiting for its next naturally-triggered update."
                      onClick={() => void api.kickWorkloadCluster(c.name).catch(err => window.alert(errorMessage(err)))}
                    >
                      Kick
                    </button>
                    <Link className="button small" to={`/clusters/${c.name}/edit`}>
                      Edit
                    </Link>
                    <button
                      className="small danger"
                      onClick={() =>
                        void confirmAndRun(
                          `Deprovision cluster "${c.name}"? This tears down every machine Cluster API created for it.`,
                          () => api.deprovisionCluster(c.name),
                          clusters.reload
                        )
                      }
                    >
                      Deprovision
                    </button>
                  </>
                )}
              </span>
            ),
          },
        ]}
      />
    </Page>
  );
}

// The latest patch of each of the last 5 minor releases, newest first.
// Static rather than fetched live: a creation form that depends on reaching
// an external version index is a worse failure mode than a stale list.
// Sourced from dl.k8s.io/release/stable-<minor>.txt.
const KUBERNETES_VERSIONS = ['v1.36.4', 'v1.35.8', 'v1.34.11', 'v1.33.13', 'v1.32.13'];

// cluster-api-provider-incus's own "cX-mY" flavors (X vCPUs, Y GiB RAM).
const INSTANCE_FLAVORS = ['c1-m2', 'c2-m4', 'c4-m8', 'c8-m16', 'c16-m32'];

function flavorLabel(flavor: string): string {
  const match = /^c(\d+)-m(\d+)$/.exec(flavor);
  return match ? `${flavor} - ${match[1]} vCPU, ${match[2]} GiB RAM` : flavor;
}

function emptyPool(name: string): WorkerPoolSpec {
  return { name, replicas: 2, incus: { flavor: 'c2-m4' } };
}

function emptySpec(): WorkloadClusterSpec {
  return {
    kubernetesVersion: KUBERNETES_VERSIONS[0],
    provider: 'Incus',
    controlPlane: { replicas: 1 },
    workerPools: [emptyPool('workers')],
    incus: {
      lxcSecretRef: { name: 'lxc-secret' },
      image: 'ubuntu:24.04',
      profiles: ['default'],
      controlPlane: { flavor: 'c2-m4' },
    },
  };
}

/**
 * Registers a WorkloadCluster, or edits one. Editing is deliberately
 * narrow: WorkloadClusterReconciler only supports day-2 changes to worker
 * pools, so the control plane and the Incus-wide settings are read-only
 * once a cluster exists rather than accepting an edit that would never
 * take effect.
 */
function ClusterForm({ existing }: { existing?: WorkloadCluster }) {
  const navigate = useNavigate();
  const [name, setName] = useState(existing?.metadata.name ?? '');
  const [spec, setSpec] = useState<WorkloadClusterSpec>(existing?.spec ?? emptySpec());
  const [error, setError] = useState<string>();
  const [saving, setSaving] = useState(false);
  const locked = Boolean(existing);

  const patch = (changes: Partial<WorkloadClusterSpec>) => setSpec(current => ({ ...current, ...changes }));
  const patchIncus = (changes: Partial<NonNullable<WorkloadClusterSpec['incus']>>) =>
    setSpec(current => ({ ...current, incus: { ...current.incus!, ...changes } }));
  const patchPool = (index: number, changes: Partial<WorkerPoolSpec>) =>
    patch({ workerPools: spec.workerPools.map((p, i) => (i === index ? { ...p, ...changes } : p)) });

  async function submit(event: FormEvent) {
    event.preventDefault();
    setSaving(true);
    setError(undefined);
    try {
      const savedName = existing?.metadata.name ?? name;
      if (existing) {
        await api.updateWorkloadCluster(savedName, { spec });
      } else {
        await api.registerCluster({ metadata: { name }, spec });
      }
      navigate(`/clusters/${savedName}`);
    } catch (err) {
      setError(errorMessage(err));
      setSaving(false);
    }
  }

  const versions =
    existing && !KUBERNETES_VERSIONS.includes(spec.kubernetesVersion)
      ? [spec.kubernetesVersion, ...KUBERNETES_VERSIONS]
      : KUBERNETES_VERSIONS;

  return (
    <Page title={existing ? `Edit ${existing.metadata.name}` : 'Register cluster'}>
      <form onSubmit={submit}>
        <Alert>{error}</Alert>
        <fieldset>
          <legend>Identity</legend>
          <div className="fields">
            <Field label="Name" help="A Kubernetes object name: lowercase letters, digits and dashes">
              <input required value={name} disabled={locked} onChange={e => setName(e.target.value)} />
            </Field>
            <Field label="Kubernetes version">
              <select
                value={spec.kubernetesVersion}
                disabled={locked}
                onChange={e => patch({ kubernetesVersion: e.target.value })}
              >
                {versions.map((v, i) => (
                  <option key={v} value={v}>
                    {v}
                    {i === 0 && !existing ? ' (latest)' : ''}
                  </option>
                ))}
              </select>
            </Field>
            <Field label="Infrastructure provider">
              <select disabled value={spec.provider}>
                <option value="Incus">Incus / LXC</option>
              </select>
            </Field>
          </div>
        </fieldset>

        <fieldset>
          <legend>Control plane</legend>
          {locked && <p className="muted">Not reconfigurable after creation - register a replacement to change it.</p>}
          <div className="fields">
            <Field label="Replicas">
              <input
                type="number"
                min={1}
                disabled={locked}
                value={spec.controlPlane.replicas}
                onChange={e => patch({ controlPlane: { replicas: Number(e.target.value) } })}
              />
            </Field>
            <Field label="Flavor">
              <select
                disabled={locked}
                value={spec.incus?.controlPlane?.flavor ?? INSTANCE_FLAVORS[1]}
                onChange={e => patchIncus({ controlPlane: { flavor: e.target.value } })}
              >
                {INSTANCE_FLAVORS.map(f => (
                  <option key={f} value={f}>
                    {flavorLabel(f)}
                  </option>
                ))}
              </select>
            </Field>
          </div>
        </fieldset>

        <fieldset>
          <legend>Worker pools</legend>
          {spec.workerPools.map((pool, i) => (
            <div className="fields" key={i} style={{ alignItems: 'end' }}>
              <Field label="Pool name">
                <input required value={pool.name} onChange={e => patchPool(i, { name: e.target.value })} />
              </Field>
              <Field
                label="Replicas"
                help={pool.autoscaling && `Autoscaled ${pool.autoscaling.minReplicas ?? 0}-${pool.autoscaling.maxReplicas}; this only sizes the pool at creation`}
              >
                <input
                  type="number"
                  min={1}
                  value={pool.replicas}
                  onChange={e => patchPool(i, { replicas: Number(e.target.value) })}
                />
              </Field>
              <Field label="Flavor">
                <select
                  value={pool.incus?.flavor ?? INSTANCE_FLAVORS[1]}
                  onChange={e => patchPool(i, { incus: { ...pool.incus, flavor: e.target.value } })}
                >
                  {INSTANCE_FLAVORS.map(f => (
                    <option key={f} value={f}>
                      {flavorLabel(f)}
                    </option>
                  ))}
                </select>
              </Field>
              <div>
                <button
                  type="button"
                  className="small danger"
                  disabled={spec.workerPools.length <= 1}
                  onClick={() => patch({ workerPools: spec.workerPools.filter((_, j) => j !== i) })}
                >
                  Remove pool
                </button>
              </div>
            </div>
          ))}
          <button
            type="button"
            onClick={() => patch({ workerPools: [...spec.workerPools, emptyPool(`pool-${spec.workerPools.length + 1}`)] })}
          >
            Add worker pool
          </button>
        </fieldset>

        {spec.provider === 'Incus' && (
          <fieldset>
            <legend>Incus / LXC</legend>
            {locked && <p className="muted">Image and profiles apply to new or edited worker pools only.</p>}
            <div className="fields">
              <Field label="LXC daemon connection secret">
                <input
                  required
                  disabled={locked}
                  value={spec.incus?.lxcSecretRef.name ?? ''}
                  placeholder="lxc-secret"
                  onChange={e => patchIncus({ lxcSecretRef: { name: e.target.value } })}
                />
              </Field>
              <Field label="Image">
                <input
                  value={spec.incus?.image ?? ''}
                  placeholder="ubuntu:24.04"
                  onChange={e => patchIncus({ image: e.target.value })}
                />
              </Field>
              <Field label="Profiles" help="Comma-separated">
                <input
                  defaultValue={(spec.incus?.profiles ?? []).join(',')}
                  placeholder="default"
                  onChange={e =>
                    patchIncus({
                      profiles: e.target.value
                        .split(',')
                        .map(p => p.trim())
                        .filter(Boolean),
                    })
                  }
                />
              </Field>
            </div>
          </fieldset>
        )}

        <div className="form-actions">
          <button type="submit" className="primary" disabled={saving}>
            {saving ? 'Saving…' : existing ? 'Save changes' : 'Register cluster'}
          </button>
          <button type="button" onClick={() => navigate(-1)}>
            Cancel
          </button>
        </div>
      </form>
    </Page>
  );
}

export function ClusterCreatePage() {
  return <ClusterForm />;
}

export function ClusterEditPage() {
  const { name = '' } = useParams();
  const existing = useResource(() => api.workloadCluster(name), [name], 0);
  if (existing.error) {
    return (
      <Page title="Edit cluster">
        <Alert>{existing.error}</Alert>
      </Page>
    );
  }
  return existing.data ? <ClusterForm key={name} existing={existing.data} /> : null;
}

/**
 * A cluster's view. "Is this cluster healthy" is a kubectl question, so the
 * main action is a shell on it; this page adds what the shell can't show at
 * a glance - the WorkloadCluster's own status and sizing.
 */
export function ClusterViewPage() {
  const { name = '' } = useParams();
  const isManagement = name === MANAGEMENT_CLUSTER;
  const wc = useResource(
    () => (isManagement ? Promise.resolve(undefined) : api.workloadCluster(name)),
    [name, isManagement],
    10000
  );
  const shellTarget = isManagement ? undefined : name;
  const status = wc.data?.status;
  const pools = wc.data?.spec.workerPools ?? [];
  const [kicking, setKicking] = useState(false);

  const kick = async () => {
    setKicking(true);
    try {
      await api.kickWorkloadCluster(name);
      wc.reload();
    } catch (err) {
      window.alert(errorMessage(err));
    } finally {
      setKicking(false);
    }
  };

  return (
    <Page
      title={name}
      subtitle={isManagement ? 'The management cluster - where the operator and this UI run.' : 'WorkloadCluster'}
      actions={
        <>
          <Link className="button primary" to={shellLinkForCluster(shellTarget)}>
            Open shell
          </Link>
          {!isManagement && (
            <>
              <button
                className="button"
                disabled={kicking}
                title="Nudge the operator into reconciling this cluster right now, instead of waiting for its next naturally-triggered update."
                onClick={() => void kick()}
              >
                {kicking ? 'Kicking…' : 'Kick'}
              </button>
              <Link className="button" to={`/clusters/${name}/edit`}>
                Edit
              </Link>
            </>
          )}
        </>
      }
    >
      <Alert>{wc.error}</Alert>
      <h2>Quick checks</h2>
      <div className="row">
        <Link className="button" to={shellLinkForCluster(shellTarget, 'kubectl get nodes -o wide')}>
          Nodes
        </Link>
        <Link
          className="button"
          to={shellLinkForCluster(shellTarget, 'kubectl get pods -A --field-selector=status.phase!=Running,status.phase!=Succeeded')}
        >
          Unhealthy pods
        </Link>
        <Link className="button" to={shellLinkForCluster(shellTarget, 'kubectl top nodes')}>
          Node usage
        </Link>
        <Link className="button" to={shellLinkForCluster(shellTarget, 'kubectl get events -A --sort-by=.lastTimestamp | tail -50')}>
          Recent events
        </Link>
      </div>

      {wc.data && (
        <>
          <h2>Status</h2>
          <div className="panel">
            <div className="row" style={{ marginBottom: 8 }}>
              <StatusBadge kind="workloadcluster" name={name} namespace={wc.data.metadata.namespace} phase={status?.phase} conditions={status?.conditions} />
              <span>
                Control plane <Badge value={status?.controlPlaneReady ? 'Ready' : 'Not ready'} />
              </span>
              <span>
                Infrastructure <Badge value={status?.infrastructureReady ? 'Ready' : 'Not ready'} />
              </span>
              <span className="muted">Kubernetes {wc.data.spec.kubernetesVersion}</span>
            </div>
            <ConditionList conditions={status?.conditions} />
          </div>

          <h2>Worker pools</h2>
          <Table
            rows={pools}
            rowKey={p => p.name}
            empty="No worker pools."
            columns={[
              { label: 'Pool', render: p => p.name },
              {
                label: 'Size',
                render: p =>
                  p.autoscaling
                    ? `autoscaling ${p.autoscaling.minReplicas ?? 0}-${p.autoscaling.maxReplicas}`
                    : `${p.replicas} (fixed)`,
              },
              { label: 'Flavor', render: p => (p.incus?.flavor ? flavorLabel(p.incus.flavor) : '-') },
            ]}
          />

          {(status?.prometheusURL || status?.grafanaURL) && (
            <>
              <h2>Observability</h2>
              <div className="row">
                {status?.prometheusURL && (
                  <span>
                    Prometheus: <code>{status.prometheusURL}</code>
                  </span>
                )}
                {status?.grafanaURL && (
                  <a className="button" href={`/grafana-proxy/${encodeURIComponent(name)}/`} target="_blank" rel="noreferrer">
                    Open Grafana ↗
                  </a>
                )}
              </div>
            </>
          )}
        </>
      )}
      <ObjectYaml object={wc.data} />
    </Page>
  );
}
