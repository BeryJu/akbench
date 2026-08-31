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

// List + register/edit views for WorkloadCluster, mirroring the deleted
// web/frontend's /clusters pages. The management cluster itself is always
// listed but can't be edited or deprovisioned (registerCluster/deleteCluster
// on internal/api/clusters.go both reject its reserved name the same way).
import { EmptyContent, Link, SectionBox, SimpleTable } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import { Alert, Button, Chip } from '@mui/material';
import { useParams } from 'react-router-dom';
import { api, ApiError, headlampClusterName } from './api';
import { ClusterForm } from './ClusterForm';
import { PrimaryLinkButton } from './PrimaryLinkButton';
import { useResource } from './useResource';

export function ClustersPage() {
  const clusters = useResource(() => api.clusters());

  async function remove(name: string) {
    if (!window.confirm(`Deprovision cluster "${name}"? This tears down every machine Cluster API created for it.`)) {
      return;
    }
    try {
      await api.deprovisionCluster(name);
      clusters.reload();
    } catch (err) {
      window.alert(err instanceof ApiError ? err.message : String(err));
    }
  }

  return (
    <SectionBox
      title="Clusters"
      headerProps={{
        actions: [<PrimaryLinkButton key="new" routeName="authentik-bench-clusters-new" label="Register cluster" />],
      }}
    >
      {clusters.error && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {clusters.error}
        </Alert>
      )}
      <SimpleTable
        columns={[
          {
            label: 'Name',
            getter: cluster =>
              cluster.management ? (
                cluster.name
              ) : (
                <Link to={`/c/${headlampClusterName(cluster.name)}`}>
                  {cluster.name}
                </Link>
              ),
          },
          {
            label: 'Role',
            getter: cluster => (cluster.management ? <Chip size="small" label="Management" /> : 'Workload'),
          },
          { label: 'Phase', getter: cluster => cluster.phase ?? '-' },
          {
            label: 'Control plane',
            getter: cluster => <Chip size="small" label={cluster.controlPlaneReady ? 'Ready' : 'Not ready'} color={cluster.controlPlaneReady ? 'success' : 'default'} />,
          },
          {
            label: 'Infrastructure',
            getter: cluster => <Chip size="small" label={cluster.infrastructureReady ? 'Ready' : 'Not ready'} color={cluster.infrastructureReady ? 'success' : 'default'} />,
          },
          {
            label: '',
            getter: cluster =>
              !cluster.management && (
                <>
                  <Link routeName="authentik-bench-clusters-edit" params={{ name: cluster.name }}>
                    Edit
                  </Link>
                  <Button color="error" onClick={() => void remove(cluster.name)}>
                    Deprovision
                  </Button>
                </>
              ),
          },
        ]}
        data={clusters.data ?? []}
        emptyMessage="No clusters yet."
      />
      {clusters.data?.length === 0 && <EmptyContent>Register a cluster to deploy topologies onto it.</EmptyContent>}
    </SectionBox>
  );
}

export function ClusterCreatePage() {
  return <ClusterForm />;
}

export function ClusterEditPage() {
  const { name } = useParams<{ name: string }>();
  const existing = useResource(() => api.workloadCluster(name), [name]);
  if (existing.loading) {
    return null;
  }
  if (existing.error) {
    return (
      <SectionBox title="Edit cluster">
        <Alert severity="error">{existing.error}</Alert>
      </SectionBox>
    );
  }
  return <ClusterForm existing={existing.data} />;
}
