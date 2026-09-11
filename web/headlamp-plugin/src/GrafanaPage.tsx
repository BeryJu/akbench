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

// Embeds a workload cluster's own Grafana (internal/render/grafana.go) in
// an iframe, proxied - not a direct browser->Grafana connection. The iframe
// src points straight at the k8s API server's own service-proxy
// subresource for the selected cluster, reusing Headlamp's own
// already-authenticated session:
//   /clusters/<context>/api/v1/namespaces/<ns>/services/<svc>:<port>/proxy/...
// This is the same CLUSTERS_PREFIX convention the installed Headlamp SDK's
// own k8s/api/v1/constants.js uses for every other cluster-scoped request -
// confirmed directly against that file rather than assumed. It's built as a
// plain relative URL (not through ApiProxy.request(), which returns parsed
// JSON and can't back an <iframe src=): the plugin is served same-origin by
// Headlamp itself, so a relative path resolves correctly without needing
// Headlamp's own getAppUrl() helper.
//
// Grafana's own GF_SERVER_ROOT_URL is set (at render time, per cluster) to
// exactly this same path prefix, and anonymous Admin auth is enabled - both
// required for the embed to actually render a dashboard instead of a broken
// asset tree or a login page. See grafana.go's own doc comment for why.
import { SectionBox } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import { Alert, MenuItem, TextField } from '@mui/material';
import { useState } from 'react';
import { api, headlampClusterName } from './api';
import { useResource } from './useResource';

// Matches internal/render/grafana.go's GrafanaObjects/GrafanaServiceName/
// GrafanaPort and gitops/dashboards/*.json's own uid fields exactly.
const GRAFANA_NAMESPACE = 'bench-system';
const GRAFANA_SERVICE = 'grafana';
const GRAFANA_PORT = 3000;

const DASHBOARDS = [
  { uid: 'akbench-compare', label: 'Compare tests' },
  { uid: 'akbench-k6', label: 'k6 load generator' },
  { uid: 'akbench-authentik', label: 'authentik under load' },
  { uid: 'akbench-postgres', label: 'Postgres' },
  { uid: 'akbench-nodes', label: 'Nodes' },
];

function grafanaProxyURL(clusterRefName: string, dashboardUID: string): string {
  const cluster = headlampClusterName(clusterRefName);
  return (
    `/clusters/${cluster}/api/v1/namespaces/${GRAFANA_NAMESPACE}/services/${GRAFANA_SERVICE}:${GRAFANA_PORT}/proxy` +
    `/d/${dashboardUID}/-?orgId=1&kiosk`
  );
}

export function GrafanaPage() {
  const clusters = useResource(() => api.clusters(), [], 0);
  const workloadClusters = (clusters.data ?? []).filter(c => !c.management);

  const [selectedCluster, setSelectedCluster] = useState('');
  const [selectedDashboard, setSelectedDashboard] = useState(DASHBOARDS[0].uid);

  const cluster = selectedCluster || workloadClusters[0]?.name || '';

  return (
    <SectionBox title="Grafana">
      {clusters.error && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {clusters.error}
        </Alert>
      )}
      {!clusters.loading && workloadClusters.length === 0 && (
        <Alert severity="info" sx={{ mb: 2 }}>
          No workload clusters yet - Grafana is deployed per workload cluster (see
          WorkloadClusterReconciler's ensureGrafana), not centrally. Register one first.
        </Alert>
      )}
      {workloadClusters.length > 0 && (
        <>
          <TextField
            select
            label="Cluster"
            value={cluster}
            onChange={e => setSelectedCluster(e.target.value)}
            size="small"
            sx={{ mr: 2, mb: 2, minWidth: 240 }}
          >
            {workloadClusters.map(c => (
              <MenuItem key={c.name} value={c.name}>
                {c.name}
              </MenuItem>
            ))}
          </TextField>
          <TextField
            select
            label="Dashboard"
            value={selectedDashboard}
            onChange={e => setSelectedDashboard(e.target.value)}
            size="small"
            sx={{ mb: 2, minWidth: 240 }}
          >
            {DASHBOARDS.map(d => (
              <MenuItem key={d.uid} value={d.uid}>
                {d.label}
              </MenuItem>
            ))}
          </TextField>
          <iframe
            key={`${cluster}/${selectedDashboard}`}
            title="Grafana"
            src={grafanaProxyURL(cluster, selectedDashboard)}
            style={{ width: '100%', height: '80vh', border: 'none' }}
          />
        </>
      )}
    </SectionBox>
  );
}
