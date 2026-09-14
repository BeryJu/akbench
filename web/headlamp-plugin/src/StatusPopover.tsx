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

// A single reusable "status at a glance" popover for any bench.goauthentik.io
// CRD instance (AuthentikDeployment/TestCase/TestRun/WorkloadCluster): a
// clickable phase/ready chip that opens a popover with the object's own
// conditions, its real Kubernetes Events (fetched live - the same Events
// internal/controller/conditions.go's setCondition now emits via
// r.Recorder.Event), and a set of caller-supplied "relevant links" (e.g. the
// namespace it deployed into, the cluster it targets, the HelmRelease it
// owns). Headlamp's own generic CRD detail page already renders conditions
// and events for free (see CustomResourceDetails.js) - this exists for the
// *list* pages, which don't get that generic detail chrome, and to bundle
// the CRD-specific "relevant links" alongside them in one place.
import { K8s } from '@kinvolk/headlamp-plugin/lib';
import { Link } from '@kinvolk/headlamp-plugin/lib/CommonComponents';
import {
  Box,
  Chip,
  CircularProgress,
  Divider,
  Popover,
  Stack,
  Typography,
} from '@mui/material';
import { MouseEvent, useState } from 'react';
import { Condition, readyCondition } from './types';

export interface StatusLink {
  label: string;
  routeName: string;
  params?: Record<string, string>;
  activeCluster?: string;
}

interface StatusPopoverProps {
  /** The CRD's own Kind, e.g. "AuthentikDeployment" - must match involvedObject.kind
   * on the Events this object's controller emits. */
  kind: string;
  name: string;
  /** Namespace the object itself lives in (not the namespace it *deploys to* -
   * bench CRs all live in one namespace on the management cluster). */
  namespace: string;
  /** Cluster to fetch Events from - almost always the management cluster,
   * since that's where every bench CRD instance and its Events live. */
  cluster: string;
  phase?: string;
  conditions?: Condition[];
  links?: StatusLink[];
}

function phaseColor(phase?: string): 'success' | 'error' | 'warning' | 'info' | 'default' {
  switch (phase) {
    case 'Ready':
    case 'Succeeded':
      return 'success';
    case 'Failed':
      return 'error';
    case 'Reconciling':
    case 'Running':
      return 'info';
    default:
      return 'default';
  }
}

function conditionColor(status: string): 'success' | 'error' | 'default' {
  return status === 'True' ? 'success' : status === 'False' ? 'error' : 'default';
}

/** StatusChip is the clickable trigger - a small phase/ready badge, matching
 * this plugin's existing ReadyChip/PhaseChip look (DeploymentsPage.tsx,
 * TestRunsPage.tsx) so swapping it in doesn't change a table's visual
 * density. */
export function StatusChip(props: StatusPopoverProps) {
  const [anchorEl, setAnchorEl] = useState<HTMLElement | null>(null);
  const label = props.phase ?? readyCondition(props.conditions)?.status ?? 'Unknown';

  return (
    <>
      <Chip
        size="small"
        label={label}
        color={phaseColor(props.phase ?? readyCondition(props.conditions)?.status)}
        onClick={(e: MouseEvent<HTMLElement>) => setAnchorEl(e.currentTarget)}
        sx={{ cursor: 'pointer' }}
      />
      <Popover
        open={Boolean(anchorEl)}
        anchorEl={anchorEl}
        onClose={() => setAnchorEl(null)}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'left' }}
      >
        <StatusPopoverContent {...props} />
      </Popover>
    </>
  );
}

function StatusPopoverContent({ kind, name, namespace, cluster, conditions, links }: StatusPopoverProps) {
  // Event isn't part of K8s.ResourceClasses (unlike Pod/Deployment/...) -
  // it's reached via the same public k8s module barrel's own star-export
  // instead (K8s.event, from k8s/index.d.ts's `export * as event from
  // './event'`), whose default export is the Event KubeObject class.
  const [events, eventsError] = K8s.event.default.useList({
    cluster,
    namespace,
    fieldSelector: `involvedObject.name=${name},involvedObject.kind=${kind}`,
  });

  return (
    <Box sx={{ p: 2, minWidth: 360, maxWidth: 480 }}>
      <Typography variant="subtitle2">
        {kind} / {name}
      </Typography>

      {conditions && conditions.length > 0 && (
        <>
          <Divider sx={{ my: 1 }} />
          <Typography variant="caption" color="text.secondary">
            Conditions
          </Typography>
          <Stack spacing={0.5} sx={{ mt: 0.5 }}>
            {conditions.map(c => (
              <Stack key={c.type} direction="row" spacing={1} alignItems="center">
                <Chip size="small" label={c.type} color={conditionColor(c.status)} sx={{ minWidth: 96 }} />
                <Typography variant="body2" sx={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>
                  {c.reason}
                  {c.message ? `: ${c.message}` : ''}
                </Typography>
              </Stack>
            ))}
          </Stack>
        </>
      )}

      <Divider sx={{ my: 1 }} />
      <Typography variant="caption" color="text.secondary">
        Recent events
      </Typography>
      {eventsError ? (
        <Typography variant="body2" color="error">
          {eventsError.message}
        </Typography>
      ) : events === null ? (
        <CircularProgress size={16} sx={{ display: 'block', mt: 1 }} />
      ) : events.length === 0 ? (
        <Typography variant="body2" color="text.secondary">
          No events yet.
        </Typography>
      ) : (
        <Stack spacing={0.5} sx={{ mt: 0.5, maxHeight: 200, overflowY: 'auto' }}>
          {[...events]
            .sort((a, b) => (b.lastOccurrence ?? '').localeCompare(a.lastOccurrence ?? ''))
            .slice(0, 10)
            .map(e => (
              <Stack key={e.metadata.uid} direction="row" spacing={1} alignItems="flex-start">
                <Chip
                  size="small"
                  label={e.type === 'Warning' ? 'Warning' : 'Normal'}
                  color={e.type === 'Warning' ? 'warning' : 'default'}
                  sx={{ minWidth: 72 }}
                />
                <Box>
                  <Typography variant="body2">
                    {e.reason}: {e.message}
                    {e.count > 1 ? ` (x${e.count})` : ''}
                  </Typography>
                  <Typography variant="caption" color="text.secondary">
                    {e.lastOccurrence}
                  </Typography>
                </Box>
              </Stack>
            ))}
        </Stack>
      )}

      {links && links.length > 0 && (
        <>
          <Divider sx={{ my: 1 }} />
          <Typography variant="caption" color="text.secondary">
            Links
          </Typography>
          <Stack spacing={0.5} sx={{ mt: 0.5 }}>
            {links.map(link => (
              <Link key={link.label} routeName={link.routeName} params={link.params} activeCluster={link.activeCluster}>
                {link.label}
              </Link>
            ))}
          </Stack>
        </>
      )}
    </Box>
  );
}
