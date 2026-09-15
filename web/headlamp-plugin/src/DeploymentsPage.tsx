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

// List + create/edit views for AuthentikDeployment, mirroring the deleted
// web/frontend's /deployments pages. Headlamp's own generic CRD browser can
// list/edit these too (as raw YAML), but this page is what actually exposes
// the guided form (DeploymentForm) the standalone UI had.
import { EmptyContent, Link, SectionBox, SimpleTable } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import { Alert, Button, Stack } from '@mui/material';
import { useParams } from 'react-router-dom';
import { api, ApiError, headlampClusterName, MANAGEMENT_CLUSTER } from './api';
import { ClusterResourceButton } from './ClusterResourceButton';
import { DeploymentForm } from './DeploymentForm';
import { DeploymentSection } from './DetailSections';
import { PrimaryLinkButton } from './PrimaryLinkButton';
import { StatusChip } from './StatusPopover';
import { useResource } from './useResource';

export function DeploymentsPage() {
  const deployments = useResource(() => api.deployments());

  async function remove(name: string) {
    if (!window.confirm(`Delete deployment "${name}"? This tears down its Helm release.`)) {
      return;
    }
    try {
      await api.deleteDeployment(name);
      deployments.reload();
    } catch (err) {
      window.alert(err instanceof ApiError ? err.message : String(err));
    }
  }

  return (
    <SectionBox
      title="Deployments"
      headerProps={{
        actions: [<PrimaryLinkButton key="new" routeName="authentik-bench-deployments-new" label="New deployment" />],
      }}
    >
      {deployments.error && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {deployments.error}
        </Alert>
      )}
      <SimpleTable
        columns={[
          {
            label: 'Name',
            getter: deployment => {
              const cluster = headlampClusterName(deployment.spec.clusterRef?.name);
              return <Link
                  routeName="Namespace"
                  params={{name: deployment.spec.namespace}}
                  activeCluster={cluster}>
                    {deployment.metadata.name}
                </Link>
            },
          },
          { label: 'Target', getter: deployment => deployment.spec.targetName || deployment.metadata.name },
          { label: 'Cluster', getter: deployment => deployment.spec.clusterRef?.name ?? 'management' },
          { label: 'Postgres', getter: deployment => deployment.spec.postgres.mode },
          {
            label: 'Status',
            getter: deployment => {
              const cluster = headlampClusterName(deployment.spec.clusterRef?.name);
              const links = [{ label: 'Namespace', routeName: 'Namespace', params: { name: deployment.spec.namespace }, activeCluster: cluster }];
              return (
                <StatusChip
                  kind="AuthentikDeployment"
                  name={deployment.metadata.name}
                  namespace={deployment.metadata.namespace ?? 'default'}
                  cluster={MANAGEMENT_CLUSTER}
                  phase={deployment.status?.phase}
                  conditions={deployment.status?.conditions}
                  links={links}
                />
              );
            },
          },
          {
            label: 'Resources',
            getter: deployment => {
              const cluster = headlampClusterName(deployment.spec.clusterRef?.name);
              return (
                <Stack direction="row" spacing={1} flexWrap="wrap">
                  <ClusterResourceButton
                    kind="Deployment"
                    cluster={cluster}
                    namespace={deployment.spec.namespace}
                    name={`${deployment.metadata.name}-authentik-server`}
                    label="Server"
                  />
                  <ClusterResourceButton
                    kind="Deployment"
                    cluster={cluster}
                    namespace={deployment.spec.namespace}
                    name={`${deployment.metadata.name}-authentik-worker`}
                    label="Worker"
                  />
                  {deployment.spec.postgres.mode === 'Embedded' && (
                    <ClusterResourceButton
                      kind="StatefulSet"
                      cluster={cluster}
                      namespace={deployment.spec.namespace}
                      name={`${deployment.metadata.name}-postgresql`}
                      label="Postgres"
                    />
                  )}
                </Stack>
              );
            },
          },
          {
            label: '',
            getter: deployment => (
              <>
                <Link routeName="authentik-bench-deployments-edit" params={{ name: deployment.metadata.name }}>
                  Edit
                </Link>
                <Button color="error" onClick={() => void remove(deployment.metadata.name)}>
                  Delete
                </Button>
              </>
            ),
          },
        ]}
        data={deployments.data ?? []}
        emptyMessage="No deployments yet."
      />
      {deployments.data?.length === 0 && (
        <EmptyContent>Create a deployment to deploy an authentik install to benchmark.</EmptyContent>
      )}
    </SectionBox>
  );
}

export function DeploymentCreatePage() {
  return <DeploymentForm />;
}

export function DeploymentEditPage() {
  const { name } = useParams<{ name: string }>();
  const existing = useResource(() => api.deployment(name), [name]);
  if (existing.loading) {
    return null;
  }
  if (existing.error) {
    return (
      <SectionBox title="Edit deployment">
        <Alert severity="error">{existing.error}</Alert>
      </SectionBox>
    );
  }
  return (
    <>
      <DeploymentForm existing={existing.data} />
      <SectionBox title="Benchmark metrics">
        <DeploymentSection deploymentName={name} />
      </SectionBox>
    </>
  );
}
