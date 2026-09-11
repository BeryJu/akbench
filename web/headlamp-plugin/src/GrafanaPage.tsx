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
// an iframe, proxied through cmd/apiserver's own reverse proxy
// (internal/api/grafana_proxy.go) - NOT through the k8s API server's own
// service-proxy subresource (Headlamp's usual /clusters/<name>/... path).
// That was the original design, and it's confirmed broken: the k8s
// apiserver's proxy subresource content-rewrites absolute HTML paths in a
// way that has zero awareness of Headlamp's own outer /clusters/<name>/
// routing layer sitting in front of it, so a page as richly-linked as
// Grafana comes out with a doubled, broken <base href> no matter how
// Grafana's own root_url/serve_from_sub_path is configured.
//
// Instead, the iframe src points straight at cmd/apiserver's own NodePort
// (bypassing the k8s apiserver's proxy subresource entirely) at
// /grafana-proxy/<workloadClusterName>/d/<uid>/-?orgId=1&kiosk - a single,
// fully project-controlled reverse-proxy hop that preserves the request
// path, matching Grafana's own GF_SERVER_SERVE_FROM_SUB_PATH=true. See
// grafana_proxy.go's doc comment for the full story.
//
// The node IP + apiserver NodePort come from api.clusters()'s management
// cluster entry (Cluster.nodeIP, only ever populated there) - the browser
// needs a real routable address, not a cluster-relative DNS name.
import { SectionBox } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import { Alert, MenuItem, TextField } from '@mui/material';
import { useState } from 'react';
import { api, MANAGEMENT_CLUSTER } from './api';
import { useResource } from './useResource';

// Matches hack/dev-apiserver.yaml's apiserver-nodeport Service.
const APISERVER_NODEPORT = 30911;

const DASHBOARDS = [
  { uid: 'akbench-compare', label: 'Compare tests' },
  { uid: 'akbench-k6', label: 'k6 load generator' },
  { uid: 'akbench-authentik', label: 'authentik under load' },
  { uid: 'akbench-postgres', label: 'Postgres' },
  { uid: 'akbench-nodes', label: 'Nodes' },
];

function grafanaProxyURL(nodeIP: string, workloadClusterName: string, dashboardUID: string): string {
  return `http://${nodeIP}:${APISERVER_NODEPORT}/grafana-proxy/${workloadClusterName}/d/${dashboardUID}/-?orgId=1&kiosk`;
}

export function GrafanaPage() {
  const clusters = useResource(() => api.clusters(), [], 0);
  const workloadClusters = (clusters.data ?? []).filter(c => !c.management);
  const nodeIP = (clusters.data ?? []).find(c => c.name === MANAGEMENT_CLUSTER)?.nodeIP;

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
      {!clusters.loading && workloadClusters.length > 0 && !nodeIP && (
        <Alert severity="warning" sx={{ mb: 2 }}>
          Could not resolve a reachable IP for the management cluster - cmd/apiserver's
          Grafana proxy needs this to build the iframe's address. Check that its nodes
          report an InternalIP.
        </Alert>
      )}
      {workloadClusters.length > 0 && nodeIP && (
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
            src={grafanaProxyURL(nodeIP, cluster, selectedDashboard)}
            style={{ width: '100%', height: '80vh', border: 'none' }}
          />
        </>
      )}
    </SectionBox>
  );
}
