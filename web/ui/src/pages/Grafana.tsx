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

// Embeds a workload cluster's own Grafana (internal/render/grafana.go) in an
// iframe, through cmd/apiserver's /grafana-proxy/<cluster>/ reverse proxy
// (internal/api/grafana_proxy.go). This app is served by cmd/apiserver
// itself, so the proxy is same-origin: no NodePort and no extra port-forward.
//
// The whole Grafana UI is embedded as-is - no dashboard picker or kiosk mode.
import { Link, useNavigate, useParams } from 'react-router';
import { api } from '../api';
import { Alert, Empty, Page } from '../components/ui';
import { useResource } from '../useResource';

// Must match render.grafanaProxyPathPrefix, which Grafana's own
// GF_SERVER_ROOT_URL is set to.
function grafanaProxyURL(cluster: string): string {
  return `/grafana-proxy/${encodeURIComponent(cluster)}/`;
}

export function GrafanaPage() {
  const { cluster: clusterParam } = useParams();
  const navigate = useNavigate();
  const clusters = useResource(() => api.workloadClusters(), [], 0);

  const withGrafana = (clusters.data ?? []).filter(c => c.status?.grafanaURL);
  const cluster = clusterParam ?? withGrafana[0]?.metadata.name;
  const known = clusters.data?.find(c => c.metadata.name === cluster);
  const ready = !!known?.status?.grafanaURL;

  const selectCluster = (name: string) => navigate(`/grafana/${encodeURIComponent(name)}`);

  return (
    <Page
      title="Grafana"
      subtitle="A workload cluster's own Grafana, proxied through the API server."
      actions={
        clusters.data &&
        withGrafana.length > 0 && (
          <select value={cluster ?? ''} onChange={e => selectCluster(e.target.value)} aria-label="Cluster">
            {clusterParam && !withGrafana.some(c => c.metadata.name === clusterParam) && (
              <option value={clusterParam}>{clusterParam}</option>
            )}
            {withGrafana.map(c => (
              <option key={c.metadata.name} value={c.metadata.name}>
                {c.metadata.name}
              </option>
            ))}
          </select>
        )
      }
    >
      <Alert>{clusters.error}</Alert>
      {clusters.data && withGrafana.length === 0 && (
        <Empty>
          No workload cluster has Grafana yet - it's installed per workload cluster once it's provisioned.{' '}
          <Link to="/clusters">Go to clusters</Link>
        </Empty>
      )}
      {clusters.data && cluster && !known && <Alert>Workload cluster {cluster} not found.</Alert>}
      {known && !ready && <Alert kind="info">Grafana on {cluster} isn't ready yet.</Alert>}
      {cluster && ready && (
        <iframe
          key={cluster}
          className="grafana-frame"
          title={`Grafana on ${cluster}`}
          src={grafanaProxyURL(cluster)}
        />
      )}
    </Page>
  );
}
