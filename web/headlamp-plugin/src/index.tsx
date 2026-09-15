/*
 * Copyright 2025 The Kubernetes Authors
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

// Replaces web/frontend (deleted): benchmark-specific pages/actions on top
// of Headlamp's own generic Kubernetes UI (resource browsing, logs, events,
// multi-cluster switching - the actual reason for this pivot) and its
// official Prometheus plugin (ships bundled, no setup here). See
// api.ts/DetailSections.tsx for how this reaches cmd/apiserver, deployed
// in-cluster (hack/dev-apiserver.yaml) rather than through a Next.js
// server-side proxy the old UI used.
//
// Guided create/edit forms for the three CRDs (Deployments/TestCases/
// Clusters) live alongside this - Headlamp's own generic CRD browser only
// offers raw-YAML editing, and the standalone UI this plugin replaces had
// proper forms for all three (DeploymentForm/TestCaseForm/ClusterForm).
//
// Top-level, cluster-independent by design: every route below sets
// useClusterURL: false. This plugin's content genuinely doesn't depend on
// which cluster is selected - api.ts pins { cluster: MANAGEMENT_CLUSTER } on
// every call regardless - so it's registered into the HOME sidebar
// container (alongside Notifications/Settings) rather than nested in the
// per-cluster IN-CLUSTER navigation that changes with the selected cluster.
// An earlier attempt at this (HOME container, but routes left at the
// useClusterURL default of true) broke every route with a 404, since a link
// reached from HOME carries no cluster segment. Read two more Headlamp
// internals to get this right this time:
// - lib/router/getRoutePath.js: a route's actual matched path only gets the
//   /c/:cluster/ prefix when getRouteUseClusterURL(route) is true (the
//   route's own useClusterURL, defaulting true) - set it false and the raw
//   path (e.g. /authentik-bench/deployments) is both registered and what
//   Router.createRouteURL returns, no cluster param involved anywhere.
// - components/Sidebar/SidebarItem.js: a *sidebar entry's* own useClusterURL
//   independently defaults to false (not true) - it already used our url
//   field as-is, unprefixed, before this change. The mismatch was on the
//   route side, not the sidebar side.
//
// Sidebar ordering note: Headlamp's sidebar (useSidebarItems.js in the
// Headlamp core) always renders a container's own built-in entries
// (Home/Notifications/Settings for HOME) before any plugin-registered
// top-level entry, and there is no `sidebarOrder`/priority field on
// registerSidebarEntry to override that in this Headlamp version (0.14) -
// so "authentik Bench" cannot be moved above them from a plugin. The one
// thing that is controllable is the icon.
//
// Route.sidebar gotcha (cost a real "entire sidebar vanishes" bug, kept
// here since it's easy to reintroduce): the object form is { item, sidebar }
// where `item` is the sidebar entry to highlight but `sidebar` is *which
// top-level sidebar container* to show - one of Headlamp's own
// DefaultSidebars values ('HOME' or 'IN-CLUSTER'), NOT the name of our own
// custom entry. DefaultSidebars is exported type-only from this SDK
// version, so the literal string (HOME_SIDEBAR below) is used directly
// instead of importing the enum.
import { registerDetailsViewSection, registerRoute, registerSidebarEntry } from '@kinvolk/headlamp-plugin/lib';
import { SectionBox } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import { ClusterCreatePage, ClusterEditPage, ClustersPage } from './ClustersPage';
import { ComparePage } from './ComparePage';
import { DashboardPage } from './DashboardPage';
import { DeploymentCreatePage, DeploymentEditPage,DeploymentsPage } from './DeploymentsPage';
import {
  DeploymentSection,
  TestCaseSection,
  TestCaseTemplateSection,
  TestRunSection,
  WorkloadClusterSection,
} from './DetailSections';
import { GrafanaPage } from './GrafanaPage';
import { registerAuthentikIcon } from "./icon";
import { TestCaseCreatePage, TestCaseEditPage, TestCasesPage } from './TestCasesPage';
import {
  TestCaseTemplateCreatePage,
  TestCaseTemplateEditPage,
  TestCaseTemplatesPage,
} from './TestCaseTemplatesPage';
import { TestRunsPage, TestRunViewPage } from './TestRunsPage';

const HOME_SIDEBAR = 'HOME';

registerAuthentikIcon();

registerSidebarEntry({
  parent: null,
  name: 'authentik-bench',
  label: 'authentik Bench',
  url: '/authentik-bench',
  icon: "authentik:logo",
  sidebar: HOME_SIDEBAR,
});
registerRoute({
  path: '/authentik-bench',
  sidebar: { item: 'authentik-bench', sidebar: HOME_SIDEBAR },
  name: 'authentik-bench',
  exact: true,
  useClusterURL: false,
  noAuthRequired: true,
  component: () => <DashboardPage />,
});

registerSidebarEntry({
  parent: 'authentik-bench',
  name: 'authentik-bench-deployments',
  label: 'Deployments',
  url: '/authentik-bench/deployments',
});
registerRoute({
  path: '/authentik-bench/deployments',
  sidebar: { item: 'authentik-bench-deployments', sidebar: HOME_SIDEBAR },
  name: 'authentik-bench-deployments',
  exact: true,
  useClusterURL: false,
  noAuthRequired: true,
  component: () => <DeploymentsPage />,
});
registerRoute({
  path: '/authentik-bench/deployments/new',
  sidebar: { item: 'authentik-bench-deployments', sidebar: HOME_SIDEBAR },
  name: 'authentik-bench-deployments-new',
  exact: true,
  useClusterURL: false,
  noAuthRequired: true,
  component: () => <DeploymentCreatePage />,
});
registerRoute({
  path: '/authentik-bench/deployments/:name/edit',
  sidebar: { item: 'authentik-bench-deployments', sidebar: HOME_SIDEBAR },
  name: 'authentik-bench-deployments-edit',
  exact: true,
  useClusterURL: false,
  noAuthRequired: true,
  component: () => <DeploymentEditPage />,
});

registerSidebarEntry({
  parent: 'authentik-bench',
  name: 'authentik-bench-testcases',
  label: 'Test cases',
  url: '/authentik-bench/testcases',
});
registerRoute({
  path: '/authentik-bench/testcases',
  sidebar: { item: 'authentik-bench-testcases', sidebar: HOME_SIDEBAR },
  name: 'authentik-bench-testcases',
  exact: true,
  useClusterURL: false,
  noAuthRequired: true,
  component: () => <TestCasesPage />,
});
registerRoute({
  path: '/authentik-bench/testcases/new',
  sidebar: { item: 'authentik-bench-testcases', sidebar: HOME_SIDEBAR },
  name: 'authentik-bench-testcases-new',
  exact: true,
  useClusterURL: false,
  noAuthRequired: true,
  component: () => <TestCaseCreatePage />,
});
registerRoute({
  path: '/authentik-bench/testcases/:name/edit',
  sidebar: { item: 'authentik-bench-testcases', sidebar: HOME_SIDEBAR },
  name: 'authentik-bench-testcases-edit',
  exact: true,
  useClusterURL: false,
  noAuthRequired: true,
  component: () => <TestCaseEditPage />,
});

registerSidebarEntry({
  parent: 'authentik-bench',
  name: 'authentik-bench-testcasetemplates',
  label: 'Test case templates',
  url: '/authentik-bench/testcasetemplates',
});
registerRoute({
  path: '/authentik-bench/testcasetemplates',
  sidebar: { item: 'authentik-bench-testcasetemplates', sidebar: HOME_SIDEBAR },
  name: 'authentik-bench-testcasetemplates',
  exact: true,
  useClusterURL: false,
  noAuthRequired: true,
  component: () => <TestCaseTemplatesPage />,
});
registerRoute({
  path: '/authentik-bench/testcasetemplates/new',
  sidebar: { item: 'authentik-bench-testcasetemplates', sidebar: HOME_SIDEBAR },
  name: 'authentik-bench-testcasetemplates-new',
  exact: true,
  useClusterURL: false,
  noAuthRequired: true,
  component: () => <TestCaseTemplateCreatePage />,
});
registerRoute({
  path: '/authentik-bench/testcasetemplates/:name/edit',
  sidebar: { item: 'authentik-bench-testcasetemplates', sidebar: HOME_SIDEBAR },
  name: 'authentik-bench-testcasetemplates-edit',
  exact: true,
  useClusterURL: false,
  noAuthRequired: true,
  component: () => <TestCaseTemplateEditPage />,
});

registerSidebarEntry({
  parent: 'authentik-bench',
  name: 'authentik-bench-clusters',
  label: 'Clusters',
  url: '/authentik-bench/clusters',
});
registerRoute({
  path: '/authentik-bench/clusters',
  sidebar: { item: 'authentik-bench-clusters', sidebar: HOME_SIDEBAR },
  name: 'authentik-bench-clusters',
  exact: true,
  useClusterURL: false,
  noAuthRequired: true,
  component: () => <ClustersPage />,
});
registerRoute({
  path: '/authentik-bench/clusters/new',
  sidebar: { item: 'authentik-bench-clusters', sidebar: HOME_SIDEBAR },
  name: 'authentik-bench-clusters-new',
  exact: true,
  useClusterURL: false,
  noAuthRequired: true,
  component: () => <ClusterCreatePage />,
});
registerRoute({
  path: '/authentik-bench/clusters/:name/edit',
  sidebar: { item: 'authentik-bench-clusters', sidebar: HOME_SIDEBAR },
  name: 'authentik-bench-clusters-edit',
  exact: true,
  useClusterURL: false,
  noAuthRequired: true,
  component: () => <ClusterEditPage />,
});

registerSidebarEntry({
  parent: 'authentik-bench',
  name: 'authentik-bench-testruns',
  label: 'Test runs',
  url: '/authentik-bench/testruns',
});
registerRoute({
  path: '/authentik-bench/testruns',
  sidebar: { item: 'authentik-bench-testruns', sidebar: HOME_SIDEBAR },
  name: 'authentik-bench-testruns',
  exact: true,
  useClusterURL: false,
  noAuthRequired: true,
  component: () => <TestRunsPage />,
});
registerRoute({
  path: '/authentik-bench/testruns/:name',
  sidebar: { item: 'authentik-bench-testruns', sidebar: HOME_SIDEBAR },
  name: 'authentik-bench-testruns-view',
  exact: true,
  useClusterURL: false,
  noAuthRequired: true,
  component: () => <TestRunViewPage />,
});

registerSidebarEntry({
  parent: 'authentik-bench',
  name: 'authentik-bench-compare',
  label: 'Compare',
  url: '/authentik-bench/compare',
});
registerRoute({
  path: '/authentik-bench/compare',
  sidebar: { item: 'authentik-bench-compare', sidebar: HOME_SIDEBAR },
  name: 'authentik-bench-compare',
  exact: true,
  useClusterURL: false,
  noAuthRequired: true,
  component: () => <ComparePage />,
});

registerSidebarEntry({
  parent: 'authentik-bench',
  name: 'authentik-bench-grafana',
  label: 'Grafana',
  url: '/authentik-bench/grafana',
});
registerRoute({
  path: '/authentik-bench/grafana',
  sidebar: { item: 'authentik-bench-grafana', sidebar: HOME_SIDEBAR },
  name: 'authentik-bench-grafana',
  exact: true,
  useClusterURL: false,
  noAuthRequired: true,
  component: () => <GrafanaPage />,
});

registerDetailsViewSection(resource => {
  if (resource?.kind !== 'TestCase') {
    return null;
  }
  return (
    <SectionBox title="Benchmark">
      <TestCaseSection name={resource.metadata.name} profile={resource.jsonData?.spec?.profile} />
    </SectionBox>
  );
});

registerDetailsViewSection(resource => {
  if (resource?.kind !== 'AuthentikDeployment') {
    return null;
  }
  return (
    <SectionBox title="Benchmark metrics">
      <DeploymentSection deploymentName={resource.metadata.name} />
    </SectionBox>
  );
});

registerDetailsViewSection(resource => {
  if (resource?.kind !== 'WorkloadCluster') {
    return null;
  }
  return (
    <SectionBox title="Cluster">
      <WorkloadClusterSection name={resource.metadata.name} />
    </SectionBox>
  );
});

registerDetailsViewSection(resource => {
  if (resource?.kind !== 'TestRun') {
    return null;
  }
  return (
    <SectionBox title="Run">
      <TestRunSection name={resource.metadata.name} />
    </SectionBox>
  );
});

registerDetailsViewSection(resource => {
  if (resource?.kind !== 'TestCaseTemplate') {
    return null;
  }
  return (
    <SectionBox title="Used by">
      <TestCaseTemplateSection name={resource.metadata.name} />
    </SectionBox>
  );
});
