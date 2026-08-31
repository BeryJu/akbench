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

// A "New topology"/"Register cluster"-style primary action, styled like a
// contained MUI Button but rendered as Headlamp's own CommonComponents Link
// (routeName, no cluster/params needed - every route this plugin registers
// is useClusterURL: false) instead of a Button + history.push. A real link
// gets real link semantics for free (ctrl/cmd-click and middle-click open a
// new tab, right-click offers "Open in new tab/window", hovering shows the
// destination in the status bar) that an onClick handler never provides -
// see ClusterResourceButton.tsx for the same reasoning applied to jumping
// to a resource on another cluster.
import { Link } from '@kinvolk/headlamp-plugin/lib/CommonComponents';

export function PrimaryLinkButton({ routeName, label }: { routeName: string; label: string }) {
  return (
    <Link
      routeName={routeName}
      sx={{
        display: 'inline-flex',
        alignItems: 'center',
        px: 2,
        py: 0.75,
        bgcolor: 'primary.main',
        color: 'primary.contrastText',
        borderRadius: 1,
        textDecoration: 'none',
        fontSize: '0.875rem',
        fontWeight: 500,
        lineHeight: 1.75,
        '&:hover': { bgcolor: 'primary.dark', textDecoration: 'none' },
      }}
    >
      {label}
    </Link>
  );
}
