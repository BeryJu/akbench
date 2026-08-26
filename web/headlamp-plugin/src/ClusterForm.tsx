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

// Ported from the deleted web/frontend/components/cluster-form.tsx, on MUI +
// Headlamp's own SectionBox instead of PatternFly/Next.js's router.
import { SectionBox } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import DeleteIcon from '@mui/icons-material/Delete';
import { Alert, Box, Button, Grid, IconButton, MenuItem, TextField, Typography } from '@mui/material';
import { useState } from 'react';
import { useHistory } from 'react-router-dom';
import { api, ApiError } from './api';
import { WorkerPoolSpec, WorkloadCluster, WorkloadClusterSpec } from './types';

// KUBERNETES_VERSIONS is the latest patch of each of the last 5 minor
// releases, newest first. Static rather than fetched live: a cluster-
// creation form that depends on reaching an external version index at
// request time is a worse failure mode than an occasionally-stale list.
// Sourced from dl.k8s.io/release/stable[-<minor>].txt - refresh these when
// they drift too far behind.
const KUBERNETES_VERSIONS = ['v1.36.4', 'v1.35.8', 'v1.34.11', 'v1.33.13', 'v1.32.13'];

// INSTANCE_FLAVORS are cluster-api-provider-incus's own "cX-mY" flavor
// format (X vCPUs, Y GiB RAM), not a made-up scheme of this project's - so
// the dropdown's labels can be computed directly from the value instead of
// hand-maintained alongside it.
const INSTANCE_FLAVORS = ['c1-m2', 'c2-m4', 'c4-m8', 'c8-m16', 'c16-m32'];

function flavorLabel(flavor: string): string {
  const match = /^c(\d+)-m(\d+)$/.exec(flavor);
  if (!match) {
    return flavor;
  }
  const [, cpu, memGiB] = match;
  return `${flavor} - ${cpu} vCPU, ${memGiB} GiB RAM`;
}

function emptyPool(name: string): WorkerPoolSpec {
  return { name, replicas: 2, incus: { flavor: 'c2-m4' } };
}

/**
 * emptySpec mirrors WorkloadClusterSpec's own CRD defaults - what you see is
 * what gets created.
 */
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
 * ClusterForm registers a new WorkloadCluster, or - given `existing` - edits
 * one already provisioned. Editing is deliberately narrow: only worker pool
 * replicas/flavor (and adding/removing whole pools) actually do anything on
 * an existing cluster, since WorkloadClusterReconciler only supports day-2
 * changes to worker pools - the control plane and every Incus/LXC-wide
 * setting (image, profiles, secret) is provisioned once and left alone after
 * that, so those fields are read-only once a cluster exists rather than
 * silently accepting an edit that would never take effect.
 */
export function ClusterForm({ existing }: { existing?: WorkloadCluster }) {
  const history = useHistory();
  const [name, setName] = useState(existing?.metadata.name ?? '');
  const [spec, setSpec] = useState<WorkloadClusterSpec>(existing?.spec ?? emptySpec());
  const [error, setError] = useState<string>();
  const [saving, setSaving] = useState(false);

  const patch = (changes: Partial<WorkloadClusterSpec>) => setSpec(current => ({ ...current, ...changes }));
  const patchIncus = (changes: Partial<NonNullable<WorkloadClusterSpec['incus']>>) =>
    setSpec(current => ({ ...current, incus: { ...current.incus!, ...changes } }));

  function patchPool(index: number, changes: Partial<WorkerPoolSpec>) {
    const pools = spec.workerPools.slice();
    pools[index] = { ...pools[index], ...changes };
    patch({ workerPools: pools });
  }
  function addPool() {
    const poolName = `pool-${spec.workerPools.length + 1}`;
    patch({ workerPools: [...spec.workerPools, emptyPool(poolName)] });
  }
  function removePool(index: number) {
    patch({ workerPools: spec.workerPools.filter((_, i) => i !== index) });
  }

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setSaving(true);
    setError(undefined);
    try {
      if (existing) {
        await api.updateWorkloadCluster(existing.metadata.name, { spec });
      } else {
        await api.registerCluster({ metadata: { name }, spec });
      }
      history.push('/authentik-bench/clusters');
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err));
      setSaving(false);
    }
  }

  return (
    <SectionBox title={existing ? `Edit ${existing.metadata.name}` : 'Register cluster'}>
      <Box component="form" onSubmit={submit}>
        {error && (
          <Alert severity="error" sx={{ mb: 2 }}>
            {error}
          </Alert>
        )}

        <Typography variant="h6" gutterBottom>
          Identity
        </Typography>
        <Grid container spacing={2} sx={{ mb: 3 }}>
          <Grid item xs={12} md={4}>
            <TextField
              fullWidth
              required
              label="Name"
              helperText="A Kubernetes object name: lowercase letters, digits and dashes"
              value={name}
              disabled={Boolean(existing)}
              onChange={e => setName(e.target.value)}
            />
          </Grid>
          <Grid item xs={12} md={4}>
            <TextField
              select
              fullWidth
              label="Kubernetes version"
              disabled={Boolean(existing)}
              value={spec.kubernetesVersion}
              onChange={e => patch({ kubernetesVersion: e.target.value })}
            >
              {(existing && !KUBERNETES_VERSIONS.includes(spec.kubernetesVersion)
                ? [spec.kubernetesVersion, ...KUBERNETES_VERSIONS]
                : KUBERNETES_VERSIONS
              ).map((version, i) => (
                <MenuItem key={version} value={version}>
                  {version}
                  {i === 0 && !existing ? ' (latest)' : ''}
                </MenuItem>
              ))}
            </TextField>
          </Grid>
          <Grid item xs={12} md={4}>
            <TextField select fullWidth label="Infrastructure provider" disabled value={spec.provider}>
              <MenuItem value="Incus">Incus / LXC</MenuItem>
            </TextField>
          </Grid>
        </Grid>

        <Typography variant="h6" gutterBottom>
          Control plane
        </Typography>
        {existing && (
          <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
            Not resizable/reconfigurable after creation - deprovision and register a replacement
            to change it.
          </Typography>
        )}
        <TextField
          type="number"
          label="Control plane replicas"
          inputProps={{ min: 1 }}
          disabled={Boolean(existing)}
          value={spec.controlPlane.replicas}
          onChange={e => patch({ controlPlane: { replicas: Number(e.target.value) } })}
          sx={{ mb: 3 }}
        />

        <Typography variant="h6" gutterBottom>
          Worker pools
        </Typography>
        {spec.workerPools.map((pool, i) => (
          <Grid container spacing={2} key={i} alignItems="center" sx={{ mb: 2 }}>
            <Grid item xs={12} md={4}>
              <TextField
                fullWidth
                required
                label="Pool name"
                helperText="Lowercase letters, digits and dashes"
                value={pool.name}
                onChange={e => patchPool(i, { name: e.target.value })}
              />
            </Grid>
            <Grid item xs={12} md={3}>
              <TextField
                fullWidth
                type="number"
                label="Replicas"
                inputProps={{ min: 1 }}
                value={pool.replicas}
                onChange={e => patchPool(i, { replicas: Number(e.target.value) })}
              />
            </Grid>
            <Grid item xs={12} md={4}>
              <TextField
                select
                fullWidth
                label="Flavor"
                value={pool.incus?.flavor ?? INSTANCE_FLAVORS[1]}
                onChange={e => patchPool(i, { incus: { ...pool.incus, flavor: e.target.value } })}
              >
                {INSTANCE_FLAVORS.map(flavor => (
                  <MenuItem key={flavor} value={flavor}>
                    {flavorLabel(flavor)}
                  </MenuItem>
                ))}
              </TextField>
            </Grid>
            <Grid item xs={12} md={1}>
              <IconButton
                aria-label={`Remove pool ${pool.name}`}
                disabled={spec.workerPools.length <= 1}
                onClick={() => removePool(i)}
              >
                <DeleteIcon />
              </IconButton>
            </Grid>
          </Grid>
        ))}
        <Button variant="outlined" onClick={addPool} sx={{ mb: 3 }}>
          Add worker pool
        </Button>

        {spec.provider === 'Incus' && (
          <>
            <Typography variant="h6" gutterBottom>
              Incus / LXC
            </Typography>
            {existing && (
              <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
                Applies to new/edited worker pools only - the control plane and already-provisioned
                pools keep what they were created with.
              </Typography>
            )}
            <Grid container spacing={2} sx={{ mb: 2 }}>
              <Grid item xs={12} md={4}>
                <TextField
                  fullWidth
                  required
                  label="LXC daemon connection secret"
                  disabled={Boolean(existing)}
                  value={spec.incus?.lxcSecretRef.name ?? ''}
                  placeholder="lxc-secret"
                  onChange={e => patchIncus({ lxcSecretRef: { name: e.target.value } })}
                />
              </Grid>
              <Grid item xs={12} md={4}>
                <TextField
                  fullWidth
                  label="Image"
                  value={spec.incus?.image ?? ''}
                  placeholder="ubuntu:24.04"
                  onChange={e => patchIncus({ image: e.target.value })}
                />
              </Grid>
              <Grid item xs={12} md={4}>
                <TextField
                  fullWidth
                  label="Profiles (comma-separated)"
                  value={(spec.incus?.profiles ?? []).join(',')}
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
              </Grid>
            </Grid>
            <TextField
              select
              label="Control plane flavor"
              disabled={Boolean(existing)}
              value={spec.incus?.controlPlane?.flavor ?? INSTANCE_FLAVORS[1]}
              onChange={e => patchIncus({ controlPlane: { flavor: e.target.value } })}
              sx={{ mb: 3, minWidth: 280 }}
            >
              {INSTANCE_FLAVORS.map(flavor => (
                <MenuItem key={flavor} value={flavor}>
                  {flavorLabel(flavor)}
                </MenuItem>
              ))}
            </TextField>
          </>
        )}

        <Box sx={{ display: 'flex', gap: 2 }}>
          <Button type="submit" variant="contained" disabled={saving}>
            {saving ? 'Saving…' : existing ? 'Save changes' : 'Register cluster'}
          </Button>
          <Button onClick={() => history.push('/authentik-bench/clusters')}>Cancel</Button>
        </Box>
      </Box>
    </SectionBox>
  );
}
