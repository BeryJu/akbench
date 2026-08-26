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

// List + create/edit views for AuthentikTopology, mirroring the deleted
// web/frontend's /topologies pages. Headlamp's own generic CRD browser can
// list/edit these too (as raw YAML), but this page is what actually exposes
// the guided form (TopologyForm) the standalone UI had.
import { Router } from '@kinvolk/headlamp-plugin/lib';
import { EmptyContent, Link, SectionBox, SimpleTable } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import { Alert, Button, Chip, Stack } from '@mui/material';
import { useHistory, useParams } from 'react-router-dom';
import { api, ApiError, MANAGEMENT_CLUSTER } from './api';
import { ClusterResourceButton } from './ClusterResourceButton';
import { TopologyForm } from './TopologyForm';
import { readyCondition } from './types';
import { useResource } from './useResource';

function ReadyChip({ status }: { status?: string }) {
  if (!status) {
    return <Chip size="small" label="Unknown" />;
  }
  return (
    <Chip
      size="small"
      label={status}
      color={status === 'True' ? 'success' : status === 'False' ? 'error' : 'default'}
    />
  );
}

export function TopologiesPage() {
  const history = useHistory();
  const topologies = useResource(() => api.topologies());

  async function remove(name: string) {
    if (!window.confirm(`Delete topology "${name}"? This tears down its Helm release.`)) {
      return;
    }
    try {
      await api.deleteTopology(name);
      topologies.reload();
    } catch (err) {
      window.alert(err instanceof ApiError ? err.message : String(err));
    }
  }

  return (
    <SectionBox
      title="Topologies"
      headerProps={{
        actions: [
          <Button
            key="new"
            variant="contained"
            onClick={() => history.push(Router.createRouteURL('authentik-bench-topologies-new'))}
          >
            New topology
          </Button>,
        ],
      }}
    >
      {topologies.error && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {topologies.error}
        </Alert>
      )}
      <SimpleTable
        columns={[
          {
            label: 'Name',
            getter: topology => (
              <Link routeName="authentik-bench-topologies-edit" params={{ name: topology.metadata.name }}>
                {topology.metadata.name}
              </Link>
            ),
          },
          { label: 'Target', getter: topology => topology.spec.targetName || topology.metadata.name },
          { label: 'Cluster', getter: topology => topology.spec.clusterRef?.name ?? 'management' },
          { label: 'Postgres', getter: topology => topology.spec.postgres.mode },
          { label: 'Ready', getter: topology => <ReadyChip status={readyCondition(topology.status?.conditions)?.status} /> },
          {
            label: 'Resources',
            getter: topology => {
              const cluster = topology.spec.clusterRef?.name ?? MANAGEMENT_CLUSTER;
              return (
                <Stack direction="row" spacing={1} flexWrap="wrap">
                  <ClusterResourceButton
                    kind="Deployment"
                    cluster={cluster}
                    namespace={topology.spec.namespace}
                    name={`${topology.metadata.name}-authentik-server`}
                    label="Server"
                  />
                  <ClusterResourceButton
                    kind="Deployment"
                    cluster={cluster}
                    namespace={topology.spec.namespace}
                    name={`${topology.metadata.name}-authentik-worker`}
                    label="Worker"
                  />
                  {topology.spec.postgres.mode === 'Embedded' && (
                    <ClusterResourceButton
                      kind="StatefulSet"
                      cluster={cluster}
                      namespace={topology.spec.namespace}
                      name={`${topology.metadata.name}-postgresql`}
                      label="Postgres"
                    />
                  )}
                </Stack>
              );
            },
          },
          {
            label: '',
            getter: topology => (
              <Button size="small" color="error" onClick={() => void remove(topology.metadata.name)}>
                Delete
              </Button>
            ),
          },
        ]}
        data={topologies.data ?? []}
        emptyMessage="No topologies yet."
      />
      {topologies.data?.length === 0 && (
        <EmptyContent>Create a topology to deploy an authentik install to benchmark.</EmptyContent>
      )}
    </SectionBox>
  );
}

export function TopologyCreatePage() {
  return <TopologyForm />;
}

export function TopologyEditPage() {
  const { name } = useParams<{ name: string }>();
  const existing = useResource(() => api.topology(name), [name]);
  if (existing.loading) {
    return null;
  }
  if (existing.error) {
    return (
      <SectionBox title="Edit topology">
        <Alert severity="error">{existing.error}</Alert>
      </SectionBox>
    );
  }
  return <TopologyForm existing={existing.data} />;
}
