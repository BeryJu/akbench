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

// Jumps to Headlamp's own generic detail view for a workload this plugin's
// controllers create (a topology's server/worker Deployment, its embedded
// postgres StatefulSet, a Burn test case's k6 Deployment) - on whichever
// cluster it actually runs on, which is frequently not the one currently
// selected in Headlamp (bench CRs always live on the management cluster;
// the workloads they render usually don't).
//
// Deliberately built on Headlamp's own CommonComponents Link (routeName +
// params) rather than Router.createRouteURL + history.push: Link's own
// implementation (node_modules/@kinvolk/headlamp-plugin's
// components/common/Link.js) already opens the resource in Headlamp's
// side-panel/Activity view instead of navigating away, whenever the user
// has "Show resource details in a side panel" enabled (Settings > General -
// backed by redux state.drawerMode.isDetailDrawerEnabled). That machinery
// (Activity.launch, the internal KubeObjectDetails component) isn't part of
// this SDK's public/externalized surface, so reaching for it directly would
// bundle those internals into our own plugin bundle instead of reusing the
// already-loaded copy in the host app - the same class of mistake that broke
// the plugin entirely once already this session (@mui/icons-material
// calling into an externalized @mui/material's stripped-down runtime
// object). Routing through Link gets the exact same side-panel behavior for
// free, using only the public API.
import { Link } from '@kinvolk/headlamp-plugin/lib/CommonComponents';

export function ClusterResourceButton({
  kind,
  cluster,
  namespace,
  name,
  label,
}: {
  kind: 'Deployment' | 'StatefulSet' | 'Job';
  cluster: string;
  namespace: string;
  name: string;
  label: string;
}) {
  return (
    <Link
      routeName={kind}
      params={{ namespace, name, cluster }}
      sx={{
        display: 'inline-block',
        px: 1.5,
        py: 0.5,
        border: '1px solid',
        borderColor: 'divider',
        borderRadius: 1,
        fontSize: '0.8125rem',
        textDecoration: 'none',
        '&:hover': { backgroundColor: 'action.hover', textDecoration: 'none' },
      }}
    >
      {label}
    </Link>
  );
}
