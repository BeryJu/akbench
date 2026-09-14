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

// Visualizes, across every cluster a deployment can run on, which node each
// deployment's pods actually landed on - the same bench.goauthentik.io/deployment
// label internal/render/isolation.go's cross-deployment pod anti-affinity
// matches on (every deployment's server/worker/postgres pods, and its k6
// runner, carry it) is what this reads, via Headlamp's own K8s.Pod.useList
// rather than cmd/apiserver: cmd/apiserver only ever talks to the
// management cluster's own API (api.ts pins { cluster: MANAGEMENT_CLUSTER }
// on every call), it has no cross-cluster pod access, whereas a Headlamp
// plugin already has one given the merged kubeconfig
// HeadlampKubeconfigReconciler maintains - K8s.Pod.useList's own
// `clusters: string[]` option queries several at once directly.
//
// Reuses this plugin's existing chart stack (recharts, the same
// SectionBox/EmptyContent/colorFor/useTheme pattern MetricsChart.tsx
// already uses) rather than a new charting approach - a stacked bar per
// node (one segment per deployment present on it) is the natural shape for
// "how many of this deployment's pods are on this node", and immediately
// shows a bar with more than one color as a same-node cross-deployment
// collision, which should never happen once the anti-affinity is in place.
import { K8s } from '@kinvolk/headlamp-plugin/lib';
import { EmptyContent, SectionBox } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import { Alert, useTheme } from '@mui/material';
import { useMemo } from 'react';
import { Bar, BarChart, CartesianGrid, Legend, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { api, headlampClusterName, MANAGEMENT_CLUSTER } from './api';
import { colorFor } from './format';
import { useResource } from './useResource';

const DEPLOYMENT_LABEL = 'bench.goauthentik.io/deployment';

/** displayCluster undoes headlampClusterName's "<name>-admin@<name>" form
 * for the chart's own labels - the raw context name is correct for Headlamp
 * routing but noisy to read in a legend/axis. */
function displayCluster(cluster: string): string {
  if (cluster === MANAGEMENT_CLUSTER) {
    return 'management';
  }
  const at = cluster.indexOf('-admin@');
  return at === -1 ? cluster : cluster.slice(0, at);
}

export function NodePlacementChart() {
  const theme = useTheme();
  const deployments = useResource(() => api.deployments(), [], 30000);

  const clusters = useMemo(() => {
    const names = new Set<string>();
    for (const t of deployments.data ?? []) {
      names.add(headlampClusterName(t.spec.clusterRef?.name));
    }
    return Array.from(names);
  }, [deployments.data]);

  const [pods, podsError] = K8s.ResourceClasses.Pod.useList({
    clusters,
    labelSelector: DEPLOYMENT_LABEL,
  });

  const { rows, deploymentNames } = useMemo(() => {
    const byNode = new Map<string, Record<string, number | string>>();
    const seenDeployments = new Set<string>();
    for (const pod of pods ?? []) {
      const node = pod.jsonData.spec?.nodeName;
      const deployment = pod.jsonData.metadata?.labels?.[DEPLOYMENT_LABEL];
      if (!node || !deployment) {
        continue;
      }
      seenDeployments.add(deployment);
      const key = `${pod.cluster}/${node}`;
      const row = byNode.get(key) ?? { key, cluster: displayCluster(pod.cluster ?? ''), node };
      row[deployment] = ((row[deployment] as number) ?? 0) + 1;
      byNode.set(key, row);
    }
    return {
      rows: Array.from(byNode.values()).sort((a, b) => (a.key as string).localeCompare(b.key as string)),
      deploymentNames: Array.from(seenDeployments).sort(),
    };
  }, [pods]);

  const error = deployments.error ?? podsError?.message;

  return (
    <SectionBox
      title="Node placement"
      subtitle="Which node each deployment's pods are running on, across every cluster - more than one color in a bar means two deployments are sharing a node."
    >
      {error && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {error}
        </Alert>
      )}
      {rows.length === 0 ? (
        <EmptyContent>No deployment pods found yet.</EmptyContent>
      ) : (
        <ResponsiveContainer width="100%" height={Math.max(220, rows.length * 36)}>
          <BarChart data={rows} layout="vertical" margin={{ top: 4, right: 24, left: 0, bottom: 0 }}>
            <CartesianGrid strokeDasharray="3 3" stroke={theme.palette.divider} />
            <XAxis type="number" allowDecimals={false} stroke={theme.palette.text.secondary} tick={{ fontSize: 11 }} />
            <YAxis
              type="category"
              dataKey="node"
              stroke={theme.palette.text.secondary}
              tick={{ fontSize: 11 }}
              width={160}
            />
            <Tooltip
              contentStyle={{
                background: theme.palette.background.paper,
                border: `1px solid ${theme.palette.divider}`,
                borderRadius: 6,
                fontSize: 13,
              }}
            />
            <Legend wrapperStyle={{ fontSize: 12 }} />
            {deploymentNames.map((name, i) => (
              <Bar key={name} dataKey={name} name={name} stackId="deployments" fill={colorFor(i)} />
            ))}
          </BarChart>
        </ResponsiveContainer>
      )}
    </SectionBox>
  );
}
