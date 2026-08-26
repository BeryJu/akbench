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

// Jumps straight to Headlamp's own generic detail view for a workload this
// plugin's controllers create (a topology's server/worker Deployment, its
// embedded postgres StatefulSet, a Burn test case's k6 Deployment) - on
// whichever cluster it actually runs on, which is frequently not the one
// currently selected in Headlamp (bench CRs always live on the management
// cluster; the workloads they render usually don't).
//
// Headlamp's own built-in resource routes are registered by Kind
// (KubeObject.detailsRoute defaults to `this.kind`, e.g. 'Deployment',
// 'StatefulSet' - see node_modules/@kinvolk/headlamp-plugin's own
// lib/k8s/KubeObject.js), so Router.createRouteURL('Deployment', {...})
// resolves the same cluster-prefixed URL Headlamp's own list/detail pages
// use, without needing to fetch the object first. Same
// Router.createRouteURL + history.push pattern as every other in-plugin
// navigation, for the same reason (see TopologiesPage.tsx etc.) - a
// hardcoded path would miss the cluster prefix these routes require.
import { Router } from '@kinvolk/headlamp-plugin/lib';
import { Button } from '@mui/material';
import { useHistory } from 'react-router-dom';

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
  const history = useHistory();
  return (
    <Button
      size="small"
      variant="outlined"
      onClick={() => history.push(Router.createRouteURL(kind, { namespace, name, cluster }))}
    >
      {label}
    </Button>
  );
}
