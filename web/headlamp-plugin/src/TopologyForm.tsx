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

// Ported from the deleted web/frontend/components/topology-form.tsx, on MUI +
// Headlamp's own SectionBox instead of PatternFly/Next.js's router.
import { SectionBox } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import { Alert, Box, Button, Grid, MenuItem, TextField, Typography } from '@mui/material';
import { useEffect, useState } from 'react';
import { useHistory } from 'react-router-dom';
import { api, ApiError } from './api';
import { PlacementMode, PostgresMode, Topology, TopologySpec } from './types';
import { useResource } from './useResource';

/**
 * emptySpec mirrors the CRD's own defaults. Restating them here is what makes
 * the form show what will actually be created rather than a set of blanks the
 * API server fills in invisibly.
 */
function emptySpec(): TopologySpec {
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

/** slugify turns a topology name into a Kubernetes-namespace-safe default:
 * lowercase, non-alphanumeric runs collapsed to a single dash, no leading/
 * trailing dash. */
function slugify(value: string): string {
  return value
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
}

export function TopologyForm({ existing }: { existing?: Topology }) {
  const history = useHistory();
  const clusters = useResource(() => api.clusters(), [], 0);
  const [name, setName] = useState(existing?.metadata.name ?? '');
  const [spec, setSpec] = useState<TopologySpec>(existing?.spec ?? emptySpec());
  const [extraValuesYAML, setExtraValuesYAML] = useState('');
  const [error, setError] = useState<string>();
  const [saving, setSaving] = useState(false);
  // Once the user has touched the namespace field directly, typing in the
  // name field should stop clobbering their choice.
  const [namespaceEdited, setNamespaceEdited] = useState(Boolean(existing));

  const patch = (changes: Partial<TopologySpec>) => setSpec(current => ({ ...current, ...changes }));

  // Extra values live in a separate ConfigMap, so a new topology has nothing
  // to load and an existing one is fetched once on mount.
  useEffect(() => {
    if (!existing) {
      return;
    }
    let cancelled = false;
    api
      .getExtraValues(existing.metadata.name)
      .then(res => {
        if (!cancelled) {
          setExtraValuesYAML(res.valuesYAML);
        }
      })
      .catch(() => {
        // Not fatal to the form - the field just starts blank, same as a new
        // topology, and the error surfaces properly if the user tries to save.
      });
    return () => {
      cancelled = true;
    };
  }, [existing]);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setSaving(true);
    setError(undefined);
    try {
      const savedName = existing?.metadata.name ?? name;
      if (existing) {
        await api.updateTopology(existing.metadata.name, { spec });
      } else {
        await api.createTopology({ metadata: { name }, spec });
      }
      await api.setExtraValues(savedName, extraValuesYAML);
      history.push('/authentik-bench/topologies');
    } catch (err) {
      setError(err instanceof ApiError ? err.message : String(err));
      setSaving(false);
    }
  }

  return (
    <SectionBox title={existing ? `Edit ${existing.metadata.name}` : 'New topology'}>
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
          <Grid item xs={12} md={6}>
            <TextField
              fullWidth
              required
              label="Name"
              value={name}
              disabled={Boolean(existing)}
              helperText="A Kubernetes object name: lowercase letters, digits and dashes"
              onChange={e => {
                setName(e.target.value);
                if (!namespaceEdited) {
                  patch({ namespace: slugify(e.target.value) });
                }
              }}
            />
          </Grid>
          <Grid item xs={12} md={6}>
            <TextField
              fullWidth
              label="Target name"
              value={spec.targetName ?? ''}
              placeholder={name}
              helperText="Defaults to the name"
              onChange={e => patch({ targetName: e.target.value || undefined })}
            />
          </Grid>
          <Grid item xs={12} md={6}>
            <TextField
              fullWidth
              required
              label="Namespace on the target cluster"
              value={spec.namespace}
              onChange={e => {
                setNamespaceEdited(true);
                patch({ namespace: e.target.value });
              }}
            />
          </Grid>
          <Grid item xs={12} md={6}>
            <TextField
              select
              fullWidth
              label="Cluster"
              value={spec.clusterRef?.name ?? ''}
              onChange={e => patch({ clusterRef: e.target.value ? { name: e.target.value } : undefined })}
            >
              <MenuItem value="">Management cluster</MenuItem>
              {(clusters.data ?? [])
                .filter(cluster => !cluster.management)
                .map(cluster => (
                  <MenuItem key={cluster.name} value={cluster.name}>
                    {cluster.name}
                  </MenuItem>
                ))}
            </TextField>
          </Grid>
        </Grid>

        <Typography variant="h6" gutterBottom>
          Workloads
        </Typography>
        <Grid container spacing={2} sx={{ mb: 3 }}>
          <Grid item xs={12} md={6}>
            <TextField
              fullWidth
              type="number"
              label="Server replicas"
              inputProps={{ min: 1 }}
              value={spec.server.replicas}
              onChange={e => patch({ server: { ...spec.server, replicas: Number(e.target.value) } })}
            />
          </Grid>
          <Grid item xs={12} md={6}>
            <TextField
              fullWidth
              type="number"
              label="Worker replicas"
              inputProps={{ min: 1 }}
              value={spec.worker.replicas}
              onChange={e => patch({ worker: { ...spec.worker, replicas: Number(e.target.value) } })}
            />
          </Grid>
          <Grid item xs={12} md={6}>
            <TextField
              fullWidth
              label="authentik image"
              value={spec.authentik?.image ?? ''}
              placeholder="ghcr.io/goauthentik/server"
              onChange={e => patch({ authentik: { ...spec.authentik, image: e.target.value || undefined } })}
            />
          </Grid>
          <Grid item xs={12} md={6}>
            <TextField
              fullWidth
              label="authentik image tag"
              value={spec.authentik?.tag ?? ''}
              placeholder="2026.6.0"
              helperText="Defaults to the chart's own"
              onChange={e => patch({ authentik: { ...spec.authentik, tag: e.target.value || undefined } })}
            />
          </Grid>
          <Grid item xs={12} md={6}>
            <TextField
              fullWidth
              label="Chart version"
              value={spec.chart?.version ?? ''}
              helperText="Defaults to latest"
              onChange={e => patch({ chart: { ...spec.chart, version: e.target.value || undefined } })}
            />
          </Grid>
        </Grid>

        <Typography variant="h6" gutterBottom>
          Placement
        </Typography>
        <Grid container spacing={2} sx={{ mb: 3 }}>
          <Grid item xs={12} md={6}>
            <TextField
              select
              fullWidth
              label="Mode"
              value={spec.placement?.mode ?? 'Default'}
              onChange={e => patch({ placement: { ...spec.placement, mode: e.target.value as PlacementMode } })}
            >
              <MenuItem value="Default">Default - pods may share nodes</MenuItem>
              <MenuItem value="Dedicated">Dedicated - one pod per node</MenuItem>
              <MenuItem value="Packed">Packed - all pods on the same node</MenuItem>
            </TextField>
          </Grid>
          {spec.placement?.mode === 'Dedicated' && (
            <Grid item xs={12} md={6}>
              <TextField
                fullWidth
                label="Anti-affinity topology key"
                value={spec.placement?.topologyKey ?? ''}
                placeholder="kubernetes.io/hostname"
                onChange={e => patch({ placement: { ...spec.placement, topologyKey: e.target.value } })}
              />
            </Grid>
          )}
        </Grid>

        <Typography variant="h6" gutterBottom>
          Postgres
        </Typography>
        <TextField
          select
          fullWidth
          label="Mode"
          sx={{ mb: 2 }}
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
          <MenuItem value="Embedded">Embedded - the chart's bundled subchart</MenuItem>
          <MenuItem value="CNPG">CloudNativePG - HA cluster</MenuItem>
          <MenuItem value="External">External - an instance you manage</MenuItem>
        </TextField>

        {spec.postgres.mode === 'Embedded' && (
          <Grid container spacing={2} sx={{ mb: 3 }}>
            <Grid item xs={12} md={6}>
              <TextField
                fullWidth
                label="Storage size"
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
            </Grid>
          </Grid>
        )}

        {spec.postgres.mode === 'CNPG' && (
          <Grid container spacing={2} sx={{ mb: 3 }}>
            <Grid item xs={12} md={6}>
              <TextField
                fullWidth
                type="number"
                label="Instances"
                inputProps={{ min: 1 }}
                value={spec.postgres.cnpg?.instances ?? 3}
                onChange={e =>
                  patch({ postgres: { ...spec.postgres, cnpg: { ...spec.postgres.cnpg, instances: Number(e.target.value) } } })
                }
              />
            </Grid>
            <Grid item xs={12} md={6}>
              <TextField
                fullWidth
                label="Storage size"
                value={spec.postgres.cnpg?.storageSize ?? ''}
                placeholder="10Gi"
                onChange={e =>
                  patch({
                    postgres: { ...spec.postgres, cnpg: { ...spec.postgres.cnpg, storageSize: e.target.value || undefined } },
                  })
                }
              />
            </Grid>
          </Grid>
        )}

        {spec.postgres.mode === 'External' && (
          <Grid container spacing={2} sx={{ mb: 3 }}>
            <Grid item xs={12} md={6}>
              <TextField
                fullWidth
                required
                label="Connection secret name"
                value={spec.postgres.external?.secretRef.name ?? ''}
                onChange={e =>
                  patch({
                    postgres: { ...spec.postgres, external: { ...spec.postgres.external, secretRef: { name: e.target.value } } },
                  })
                }
              />
            </Grid>
            <Grid item xs={12} md={6}>
              <TextField
                select
                fullWidth
                label="SSL mode"
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
                {['disable', 'require', 'verify-ca', 'verify-full'].map(mode => (
                  <MenuItem key={mode} value={mode}>
                    {mode}
                  </MenuItem>
                ))}
              </TextField>
            </Grid>
          </Grid>
        )}

        <Typography variant="h6" gutterBottom>
          Advanced: extra Helm values
        </Typography>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
          Raw values.yaml for the authentik chart, merged over everything above last - the escape
          hatch for settings this form doesn&apos;t expose a field for. Deep-merged: a key you
          don&apos;t set here is left alone.
        </Typography>
        <TextField
          fullWidth
          multiline
          minRows={10}
          spellCheck={false}
          placeholder={'authentik:\n  geoip:\n    enabled: true\n'}
          value={extraValuesYAML}
          onChange={e => setExtraValuesYAML(e.target.value)}
          sx={{ mb: 3, '& textarea': { fontFamily: 'monospace' } }}
        />

        <Box sx={{ display: 'flex', gap: 2 }}>
          <Button type="submit" variant="contained" disabled={saving}>
            {saving ? 'Saving…' : existing ? 'Save changes' : 'Create topology'}
          </Button>
          <Button onClick={() => history.push('/authentik-bench/topologies')}>Cancel</Button>
        </Box>
      </Box>
    </SectionBox>
  );
}
