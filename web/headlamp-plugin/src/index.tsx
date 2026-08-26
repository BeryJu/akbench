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
import { registerDetailsViewSection, registerRoute, registerSidebarEntry } from '@kinvolk/headlamp-plugin/lib';
import { SectionBox } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import { ComparePage } from './ComparePage';
import { DashboardPage } from './DashboardPage';
import { TestCaseSection, TopologySection } from './DetailSections';

registerSidebarEntry({
  parent: null,
  name: 'authentik-bench',
  label: 'authentik Bench',
  url: '/authentik-bench',
  icon: 'mdi:chart-line',
});
registerRoute({
  path: '/authentik-bench',
  sidebar: 'authentik-bench',
  name: 'authentik-bench',
  component: () => <DashboardPage />,
});

registerSidebarEntry({
  parent: 'authentik-bench',
  name: 'authentik-bench-compare',
  label: 'Compare',
  url: '/authentik-bench/compare',
});
registerRoute({
  path: '/authentik-bench/compare',
  sidebar: { item: 'authentik-bench-compare', sidebar: 'authentik-bench' },
  name: 'authentik-bench-compare',
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
