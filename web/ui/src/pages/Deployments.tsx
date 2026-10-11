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

import { FormEvent, useEffect, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router';
import { api } from '../api';
import { CodeEditor } from '../components/CodeEditor';
import { MetricsGrid } from '../components/MetricsChart';
import { ObjectYaml } from '../components/ObjectYaml';
import { ConditionList, StatusBadge } from '../components/Status';
import { Alert, confirmAndRun, Empty, errorMessage, Field, Page, Table } from '../components/ui';
import { toast } from '../components/Toast';
import { shellLinkForCluster } from '../shell/links';
import { AuthentikDeployment, AuthentikDeploymentSpec, PlacementMode, PostgresMode, testCaseDeployments } from '../types';
import { useResource } from '../useResource';

export function DeploymentsPage() {
  const deployments = useResource(() => api.deployments(), [], 5000, 'AuthentikDeployment');

  return (
    <Page
      title="Deployments"
      subtitle="authentik installs to benchmark."
      actions={
        <Link className="button primary" to="/deployments/new">
          New deployment
        </Link>
      }
    >
      <Alert>{deployments.error}</Alert>
      <Table
        rows={deployments.data}
        rowKey={d => d.metadata.name}
        empty="No deployments yet - create one to deploy an authentik install to benchmark."
        columns={[
          { label: 'Name', render: d => <Link to={`/deployments/${d.metadata.name}`}>{d.metadata.name}</Link> },
          { label: 'Target', render: d => d.spec.targetName || d.metadata.name },
          { label: 'Cluster', render: d => d.spec.clusterRef?.name ?? 'management' },
          { label: 'Namespace', render: d => <code>{d.spec.namespace}</code> },
          { label: 'Postgres', render: d => d.spec.postgres.mode },
          { label: 'Replicas', render: d => `${d.spec.server.replicas} server / ${d.spec.worker.replicas} worker` },
          {
            label: 'Status',
            render: d => (
              <StatusBadge
                kind="authentikdeployment"
                name={d.metadata.name}
                namespace={d.metadata.namespace}
                phase={d.status?.phase}
                conditions={d.status?.conditions}
              />
            ),
          },
          {
            label: '',
            className: 'actions',
            render: d => (
              <span className="row" style={{ justifyContent: 'flex-end' }}>
                <button
                  className="small"
                  title="Nudge the operator into reconciling this deployment right now, instead of waiting for its next naturally-triggered update."
                  onClick={() =>
                    void api
                      .kickDeployment(d.metadata.name)
                      .then(() => toast.success(`Kicked ${d.metadata.name}`))
                      .catch(err => toast.error(errorMessage(err)))
                  }
                >
                  Kick
                </button>
                <Link className="button small" to={`/deployments/${d.metadata.name}/edit`}>
                  Edit
                </Link>
                <button
                  className="small danger"
                  onClick={() =>
                    void confirmAndRun(
                      `Delete deployment "${d.metadata.name}"? This tears down its Helm release.`,
                      () => api.deleteDeployment(d.metadata.name),
                      deployments.reload,
                      `Deleted ${d.metadata.name}`
                    )
                  }
                >
                  Delete
                </button>
              </span>
            ),
          },
        ]}
      />
    </Page>
  );
}

/**
 * emptySpec mirrors the CRD's own defaults, so the form shows what will
 * actually be created rather than blanks the API server fills in invisibly.
 */
function emptySpec(): AuthentikDeploymentSpec {
  return {
    namespace: '',
    server: { replicas: 1 },
    worker: { replicas: 1 },
    placement: { mode: 'Default', topologyKey: 'kubernetes.io/hostname' },
    postgres: { mode: 'Embedded', embedded: {} },
    chart: { repo: 'https://charts.goauthentik.io' },
    authentik: { image: 'ghcr.io/goauthentik/server' },
  };
}

/** slugify turns a deployment name into a namespace-safe default. */
function slugify(value: string): string {
  return value
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
}

function DeploymentForm({ existing }: { existing?: AuthentikDeployment }) {
  const navigate = useNavigate();
  const clusters = useResource(() => api.clusters(), [], 0);
  const [name, setName] = useState(existing?.metadata.name ?? '');
  // Starts from the fetched spec, so fields this form has no input for are
  // carried through a save untouched.
  const [spec, setSpec] = useState<AuthentikDeploymentSpec>(existing?.spec ?? emptySpec());
  const [extraValuesYAML, setExtraValuesYAML] = useState('');
  const [error, setError] = useState<string>();
  const [saving, setSaving] = useState(false);
  // Once the user edits the namespace directly, the name stops driving it.
  const [namespaceEdited, setNamespaceEdited] = useState(Boolean(existing));

  const patch = (changes: Partial<AuthentikDeploymentSpec>) => setSpec(current => ({ ...current, ...changes }));

  // Extra values live in a separate ConfigMap: fetched once for an edit.
  useEffect(() => {
    if (!existing) {
      return;
    }
    let cancelled = false;
    api
      .getExtraValues(existing.metadata.name)
      .then(res => !cancelled && setExtraValuesYAML(res.valuesYAML))
      // Not fatal - the field starts blank and a real problem surfaces on save.
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, [existing]);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setSaving(true);
    setError(undefined);
    try {
      const savedName = existing?.metadata.name ?? name;
      if (existing) {
        await api.updateDeployment(savedName, { spec });
        toast.success(`Saved ${savedName}`);
      } else {
        await api.createDeployment({ metadata: { name }, spec });
        toast.success(`Created ${savedName}`);
      }
      await api.setExtraValues(savedName, extraValuesYAML);
      navigate(`/deployments/${savedName}`);
    } catch (err) {
      setError(errorMessage(err));
      setSaving(false);
    }
  }

  const authentik = spec.authentik ?? {};
  const errorReporting = authentik.errorReporting ?? {};

  return (
    <Page title={existing ? `Edit ${existing.metadata.name}` : 'New deployment'}>
      <form onSubmit={submit}>
        <Alert>{error}</Alert>

        <fieldset>
          <legend>Identity</legend>
          <div className="fields">
            <Field label="Name" help="A Kubernetes object name: lowercase letters, digits and dashes">
              <input
                required
                value={name}
                disabled={Boolean(existing)}
                onChange={e => {
                  setName(e.target.value);
                  if (!namespaceEdited) {
                    patch({ namespace: slugify(e.target.value) });
                  }
                }}
              />
            </Field>
            <Field label="Target name" help="Defaults to the name">
              <input
                value={spec.targetName ?? ''}
                placeholder={name}
                onChange={e => patch({ targetName: e.target.value || undefined })}
              />
            </Field>
            <Field label="Namespace on the target cluster">
              <input
                required
                value={spec.namespace}
                onChange={e => {
                  setNamespaceEdited(true);
                  patch({ namespace: e.target.value });
                }}
              />
            </Field>
            <Field label="Cluster">
              <select
                value={spec.clusterRef?.name ?? ''}
                onChange={e => patch({ clusterRef: e.target.value ? { name: e.target.value } : undefined })}
              >
                <option value="">Management cluster</option>
                {(clusters.data ?? [])
                  .filter(c => !c.management)
                  .map(c => (
                    <option key={c.name} value={c.name}>
                      {c.name}
                    </option>
                  ))}
              </select>
            </Field>
          </div>
        </fieldset>

        <fieldset>
          <legend>Workloads</legend>
          <div className="fields">
            <Field label="Server replicas">
              <input
                type="number"
                min={1}
                value={spec.server.replicas}
                onChange={e => patch({ server: { ...spec.server, replicas: Number(e.target.value) } })}
              />
            </Field>
            <Field label="Worker replicas">
              <input
                type="number"
                min={1}
                value={spec.worker.replicas}
                onChange={e => patch({ worker: { ...spec.worker, replicas: Number(e.target.value) } })}
              />
            </Field>
            <Field label="authentik image">
              <input
                value={authentik.image ?? ''}
                placeholder="ghcr.io/goauthentik/server"
                onChange={e => patch({ authentik: { ...authentik, image: e.target.value || undefined } })}
              />
            </Field>
            <Field label="authentik image tag" help="Defaults to the chart's own">
              <input
                value={authentik.tag ?? ''}
                placeholder="2026.6.0"
                onChange={e => patch({ authentik: { ...authentik, tag: e.target.value || undefined } })}
              />
            </Field>
            <Field label="Chart version" help="Defaults to latest">
              <input
                value={spec.chart?.version ?? ''}
                onChange={e => patch({ chart: { ...spec.chart, version: e.target.value || undefined } })}
              />
            </Field>
          </div>
        </fieldset>

        <fieldset>
          <legend>Observability</legend>
          <div className="fields">
            <Field label="Pyroscope host" help="Defaults to this cluster's own Pyroscope">
              <input
                value={authentik.pyroscopeHost ?? ''}
                placeholder="http://pyroscope.bench-system.svc.cluster.local:4040"
                onChange={e => patch({ authentik: { ...authentik, pyroscopeHost: e.target.value || undefined } })}
              />
            </Field>
            <label className="check">
              <input
                type="checkbox"
                checked={errorReporting.enabled ?? false}
                onChange={e =>
                  patch({ authentik: { ...authentik, errorReporting: { ...errorReporting, enabled: e.target.checked } } })
                }
              />
              Send traces (error reporting / OTLP)
            </label>
            {errorReporting.enabled && (
              <Field label="Tracing endpoint" help="Defaults to this cluster's own Tempo">
                <input
                  value={errorReporting.tracingEndpoint ?? ''}
                  placeholder="http://tempo.bench-system.svc.cluster.local:4318"
                  onChange={e =>
                    patch({
                      authentik: {
                        ...authentik,
                        errorReporting: { ...errorReporting, tracingEndpoint: e.target.value || undefined },
                      },
                    })
                  }
                />
              </Field>
            )}
          </div>
        </fieldset>

        <fieldset>
          <legend>Placement</legend>
          <div className="fields">
            <Field label="Mode">
              <select
                value={spec.placement?.mode ?? 'Default'}
                onChange={e => patch({ placement: { ...spec.placement, mode: e.target.value as PlacementMode } })}
              >
                <option value="Default">Default - pods may share nodes</option>
                <option value="Dedicated">Dedicated - one pod per node</option>
                <option value="Packed">Packed - all pods on the same node</option>
              </select>
            </Field>
            {spec.placement?.mode === 'Dedicated' && (
              <Field label="Anti-affinity topology key">
                <input
                  value={spec.placement?.topologyKey ?? ''}
                  placeholder="kubernetes.io/hostname"
                  onChange={e => patch({ placement: { ...spec.placement, topologyKey: e.target.value } })}
                />
              </Field>
            )}
            <Field label="Node selector" help="Comma-separated key=value">
              <input
                defaultValue={Object.entries(spec.placement?.nodeSelector ?? {})
                  .map(([k, v]) => `${k}=${v}`)
                  .join(',')}
                placeholder="disktype=ssd"
                onChange={e => {
                  const nodeSelector: Record<string, string> = {};
                  for (const pair of e.target.value.split(',')) {
                    const [k, v] = pair.split('=').map(s => s.trim());
                    if (k) {
                      nodeSelector[k] = v ?? '';
                    }
                  }
                  patch({ placement: { ...spec.placement, nodeSelector } });
                }}
              />
            </Field>
          </div>
          <div className="muted">
            Tolerations aren&apos;t a form field (a structured list) - set them with <code>kubectl edit</code> in the
            shell.
          </div>
        </fieldset>

        <fieldset>
          <legend>Postgres</legend>
          <div className="fields">
            <Field label="Mode">
              <select
                value={spec.postgres.mode}
                onChange={e => {
                  const mode = e.target.value as PostgresMode;
                  patch({
                    postgres: {
                      mode,
                      embedded: mode === 'Embedded' ? (spec.postgres.embedded ?? {}) : undefined,
                      cnpg: mode === 'CNPG' ? (spec.postgres.cnpg ?? { instances: 3 }) : undefined,
                      external:
                        mode === 'External'
                          ? (spec.postgres.external ?? { secretRef: { name: '' }, sslMode: 'require' })
                          : undefined,
                    },
                  });
                }}
              >
                <option value="Embedded">Embedded - the chart&apos;s bundled subchart</option>
                <option value="CNPG">CloudNativePG - HA cluster</option>
                <option value="External">External - an instance you manage</option>
              </select>
            </Field>
            {spec.postgres.mode === 'Embedded' && (
              <Field label="Storage size">
                <input
                  value={spec.postgres.embedded?.storageSize ?? ''}
                  placeholder="8Gi"
                  onChange={e =>
                    patch({
                      postgres: {
                        ...spec.postgres,
                        embedded: { ...spec.postgres.embedded, storageSize: e.target.value || undefined },
                      },
                    })
                  }
                />
              </Field>
            )}
            {spec.postgres.mode === 'CNPG' && (
              <>
                <Field label="Instances">
                  <input
                    type="number"
                    min={1}
                    value={spec.postgres.cnpg?.instances ?? 3}
                    onChange={e =>
                      patch({
                        postgres: { ...spec.postgres, cnpg: { ...spec.postgres.cnpg, instances: Number(e.target.value) } },
                      })
                    }
                  />
                </Field>
                <Field label="Storage size">
                  <input
                    value={spec.postgres.cnpg?.storageSize ?? ''}
                    placeholder="10Gi"
                    onChange={e =>
                      patch({
                        postgres: {
                          ...spec.postgres,
                          cnpg: { ...spec.postgres.cnpg, storageSize: e.target.value || undefined },
                        },
                      })
                    }
                  />
                </Field>
              </>
            )}
            {spec.postgres.mode === 'External' && (
              <>
                <Field label="Connection secret name">
                  <input
                    required
                    value={spec.postgres.external?.secretRef.name ?? ''}
                    onChange={e =>
                      patch({
                        postgres: {
                          ...spec.postgres,
                          external: { ...spec.postgres.external, secretRef: { name: e.target.value } },
                        },
                      })
                    }
                  />
                </Field>
                <Field label="SSL mode">
                  <select
                    value={spec.postgres.external?.sslMode ?? 'require'}
                    onChange={e =>
                      patch({
                        postgres: {
                          ...spec.postgres,
                          external: {
                            secretRef: spec.postgres.external?.secretRef ?? { name: '' },
                            sslMode: e.target.value,
                          },
                        },
                      })
                    }
                  >
                    {['disable', 'require', 'verify-ca', 'verify-full'].map(m => (
                      <option key={m} value={m}>
                        {m}
                      </option>
                    ))}
                  </select>
                </Field>
              </>
            )}
          </div>
        </fieldset>

        <fieldset>
          <legend>Advanced: extra Helm values</legend>
          <p className="muted">
            Raw values.yaml for the authentik chart, deep-merged over everything above last - the escape hatch for
            settings this form doesn&apos;t expose. For example: <code>authentik.geoip.enabled: true</code>
          </p>
          <CodeEditor language="yaml" value={extraValuesYAML} onChange={setExtraValuesYAML} />
        </fieldset>

        <div className="form-actions">
          <button type="submit" className="primary" disabled={saving}>
            {saving ? 'Saving…' : existing ? 'Save changes' : 'Create deployment'}
          </button>
          <button type="button" onClick={() => navigate(-1)}>
            Cancel
          </button>
        </div>
      </form>
    </Page>
  );
}

export function DeploymentCreatePage() {
  return <DeploymentForm />;
}

export function DeploymentEditPage() {
  const { name = '' } = useParams();
  const existing = useResource(() => api.deployment(name), [name], 0);
  if (existing.error) {
    return (
      <Page title="Edit deployment">
        <Alert>{existing.error}</Alert>
      </Page>
    );
  }
  return existing.data ? <DeploymentForm key={name} existing={existing.data} /> : null;
}

/**
 * The deployment's view: its status, shell hand-offs for its pods on
 * whichever cluster it runs on, and metrics for every TestCase pointed at it.
 */
export function DeploymentViewPage() {
  const { name = '' } = useParams();
  const deployment = useResource(() => api.deployment(name), [name], 10000, 'AuthentikDeployment');
  const testCases = useResource(() => api.testCases(), [], 15000, 'TestCase');
  const testids = (testCases.data ?? [])
    .filter(tc => testCaseDeployments(tc.spec).some(ref => ref.name === name))
    .map(tc => tc.metadata.name);
  const [kicking, setKicking] = useState(false);

  const d = deployment.data;
  const cluster = d?.spec.clusterRef?.name;
  const ns = d?.spec.namespace;

  const kick = async () => {
    setKicking(true);
    try {
      await api.kickDeployment(name);
      deployment.reload();
      toast.success(`Kicked ${name}`);
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setKicking(false);
    }
  };

  return (
    <Page
      title={name}
      subtitle={d && `AuthentikDeployment on ${cluster ?? 'the management cluster'}, namespace ${ns}`}
      actions={
        <>
          <button
            className="button"
            disabled={kicking}
            title="Nudge the operator into reconciling this deployment right now, instead of waiting for its next naturally-triggered update."
            onClick={() => void kick()}
          >
            {kicking ? 'Kicking…' : 'Kick'}
          </button>
          <Link className="button" to={`/deployments/${name}/edit`}>
            Edit
          </Link>
        </>
      }
    >
      <Alert>{deployment.error}</Alert>
      {d && (
        <>
          <div className="panel">
            <div className="row" style={{ marginBottom: 8 }}>
              <StatusBadge kind="authentikdeployment" name={name} namespace={d.metadata.namespace} phase={d.status?.phase} conditions={d.status?.conditions} />
              {d.status?.serviceEndpoint && (
                <span className="muted">
                  Service: <code>{d.status.serviceEndpoint}</code>
                </span>
              )}
            </div>
            <ConditionList conditions={d.status?.conditions} />
          </div>

          <h2>On the cluster</h2>
          <div className="row">
            <Link className="button" to={shellLinkForCluster(cluster, `kubectl get pods -n ${ns} -o wide`)}>
              Pods
            </Link>
            <Link
              className="button"
              to={shellLinkForCluster(cluster, `kubectl logs -n ${ns} deploy/${name}-authentik-server -f --tail=200`)}
            >
              Server logs
            </Link>
            <Link
              className="button"
              to={shellLinkForCluster(cluster, `kubectl logs -n ${ns} deploy/${name}-authentik-worker -f --tail=200`)}
            >
              Worker logs
            </Link>
            <Link className="button" to={shellLinkForCluster(cluster, `kubectl get events -n ${ns} --sort-by=.lastTimestamp`)}>
              Events
            </Link>
            {d.spec.postgres.mode === 'Embedded' && (
              <Link className="button" to={shellLinkForCluster(cluster, `kubectl describe statefulset -n ${ns} ${name}-postgresql`)}>
                Postgres
              </Link>
            )}
          </div>
        </>
      )}

      <h2>Metrics</h2>
      {testids.length === 0 ? (
        <Empty>No test cases reference this deployment yet.</Empty>
      ) : (
        <MetricsGrid testids={testids} />
      )}

      <ObjectYaml object={d} />
    </Page>
  );
}
