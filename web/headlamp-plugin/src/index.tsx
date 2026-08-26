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
// Guided create/edit forms for the three CRDs (Topologies/TestCases/
// Clusters) live alongside this - Headlamp's own generic CRD browser only
// offers raw-YAML editing, and the standalone UI this plugin replaces had
// proper forms for all three (TopologyForm/TestCaseForm/ClusterForm).
//
// Sidebar ordering note: Headlamp's sidebar (useSidebarItems.js in the
// Headlamp core) always renders the IN-CLUSTER container's own built-in
// categories (Cluster, Workloads, Storage, Network, Security, Config,
// Custom Resources, ...) before any plugin-registered top-level entry, and
// there is no `sidebarOrder`/priority field on registerSidebarEntry to
// override that in this Headlamp version (0.14) - so "authentik Bench"
// cannot be moved above them from a plugin. The one thing that is
// controllable is the icon.
//
// Registering into the HOME container instead (tried once) doesn't work
// here: every route below is cluster-prefixed (useClusterURL defaults to
// true), and routes reached from the HOME container's own navigation don't
// carry a cluster segment, so they 404 - broke every route in the plugin
// the moment it was tried. Staying on IN-CLUSTER despite this plugin's
// content being cluster-independent (api.ts pins
// { cluster: MANAGEMENT_CLUSTER } on every call, regardless of the
// currently selected cluster) is the tradeoff that actually works without
// also reworking every route to opt out of cluster-prefixing.
//
// Route.sidebar gotcha (cost a real "entire sidebar vanishes" bug): the
// object form is { item, sidebar } where `item` is the sidebar entry to
// highlight but `sidebar` is *which top-level sidebar container* to show -
// one of Headlamp's own DefaultSidebars values ('HOME' or 'IN-CLUSTER'),
// NOT the name of our own custom entry. Passing our entry's own name there
// (as this file originally did, copying what looked like a reasonable
// pattern) switches Headlamp's rendered sidebar to a container called
// "authentik-bench" that was never actually populated (registerSidebarEntry
// entries land in one of the two real containers, not a same-named one), so
// Sidebar.js's useSidebarItems() looks it up, finds nothing, and renders an
// empty sidebar the moment that route becomes active - confirmed by reading
// Headlamp's own Sidebar.js (useSidebarItem) and useSidebarItems.js source.
// DefaultSidebars is exported type-only from this SDK version, so the
// literal string (IN_CLUSTER_SIDEBAR below) is used directly instead of
// importing the enum.
import { registerDetailsViewSection, registerRoute, registerSidebarEntry } from '@kinvolk/headlamp-plugin/lib';
import { SectionBox } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import { ClusterCreatePage, ClusterEditPage, ClustersPage } from './ClustersPage';
import { ComparePage } from './ComparePage';
import { DashboardPage } from './DashboardPage';
import { TestCaseSection, TopologySection } from './DetailSections';
import { TestCaseCreatePage, TestCaseEditPage, TestCasesPage } from './TestCasesPage';
import { TopologiesPage,TopologyCreatePage, TopologyEditPage } from './TopologiesPage';

const IN_CLUSTER_SIDEBAR = 'IN-CLUSTER';

// simple-icons:authentik (the actual authentik brand mark) was tried first,
// but @iconify/react resolves icon names via a runtime fetch to
// api.iconify.design - when that lookup 404s or the request fails (no
// egress from this cluster, or the slug being wrong), the sidebar entry
// fails to render at all rather than falling back to a blank/default glyph.
// mdi: icons are the set every other built-in Headlamp sidebar entry uses
// and are already confirmed working here.
const ICON = 'mdi:speedometer';

registerSidebarEntry({
  parent: null,
  name: 'authentik-bench',
  label: 'authentik Bench',
  url: '/authentik-bench',
  icon: ICON,
  sidebar: IN_CLUSTER_SIDEBAR,
});
registerRoute({
  path: '/authentik-bench',
  sidebar: { item: 'authentik-bench', sidebar: IN_CLUSTER_SIDEBAR },
  name: 'authentik-bench',
  exact: true,
  component: () => <DashboardPage />,
});

registerSidebarEntry({
  parent: 'authentik-bench',
  name: 'authentik-bench-topologies',
  label: 'Topologies',
  url: '/authentik-bench/topologies',
});
registerRoute({
  path: '/authentik-bench/topologies',
  sidebar: { item: 'authentik-bench-topologies', sidebar: IN_CLUSTER_SIDEBAR },
  name: 'authentik-bench-topologies',
  exact: true,
  component: () => <TopologiesPage />,
});
registerRoute({
  path: '/authentik-bench/topologies/new',
  sidebar: { item: 'authentik-bench-topologies', sidebar: IN_CLUSTER_SIDEBAR },
  name: 'authentik-bench-topologies-new',
  exact: true,
  component: () => <TopologyCreatePage />,
});
registerRoute({
  path: '/authentik-bench/topologies/:name/edit',
  sidebar: { item: 'authentik-bench-topologies', sidebar: IN_CLUSTER_SIDEBAR },
  name: 'authentik-bench-topologies-edit',
  exact: true,
  component: () => <TopologyEditPage />,
});

registerSidebarEntry({
  parent: 'authentik-bench',
  name: 'authentik-bench-testcases',
  label: 'Test cases',
  url: '/authentik-bench/testcases',
});
registerRoute({
  path: '/authentik-bench/testcases',
  sidebar: { item: 'authentik-bench-testcases', sidebar: IN_CLUSTER_SIDEBAR },
  name: 'authentik-bench-testcases',
  exact: true,
  component: () => <TestCasesPage />,
});
registerRoute({
  path: '/authentik-bench/testcases/new',
  sidebar: { item: 'authentik-bench-testcases', sidebar: IN_CLUSTER_SIDEBAR },
  name: 'authentik-bench-testcases-new',
  exact: true,
  component: () => <TestCaseCreatePage />,
});
registerRoute({
  path: '/authentik-bench/testcases/:name/edit',
  sidebar: { item: 'authentik-bench-testcases', sidebar: IN_CLUSTER_SIDEBAR },
  name: 'authentik-bench-testcases-edit',
  exact: true,
  component: () => <TestCaseEditPage />,
});

registerSidebarEntry({
  parent: 'authentik-bench',
  name: 'authentik-bench-clusters',
  label: 'Clusters',
  url: '/authentik-bench/clusters',
});
registerRoute({
  path: '/authentik-bench/clusters',
  sidebar: { item: 'authentik-bench-clusters', sidebar: IN_CLUSTER_SIDEBAR },
  name: 'authentik-bench-clusters',
  exact: true,
  component: () => <ClustersPage />,
});
registerRoute({
  path: '/authentik-bench/clusters/new',
  sidebar: { item: 'authentik-bench-clusters', sidebar: IN_CLUSTER_SIDEBAR },
  name: 'authentik-bench-clusters-new',
  exact: true,
  component: () => <ClusterCreatePage />,
});
registerRoute({
  path: '/authentik-bench/clusters/:name/edit',
  sidebar: { item: 'authentik-bench-clusters', sidebar: IN_CLUSTER_SIDEBAR },
  name: 'authentik-bench-clusters-edit',
  exact: true,
  component: () => <ClusterEditPage />,
});

registerSidebarEntry({
  parent: 'authentik-bench',
  name: 'authentik-bench-compare',
  label: 'Compare',
  url: '/authentik-bench/compare',
});
registerRoute({
  path: '/authentik-bench/compare',
  sidebar: { item: 'authentik-bench-compare', sidebar: IN_CLUSTER_SIDEBAR },
  name: 'authentik-bench-compare',
  exact: true,
  component: () => <ComparePage />,
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
  if (resource?.kind !== 'AuthentikTopology') {
    return null;
  }
  return (
    <SectionBox title="Benchmark metrics">
      <TopologySection topologyName={resource.metadata.name} />
    </SectionBox>
  );
});
