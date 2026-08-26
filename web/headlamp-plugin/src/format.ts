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

/** formatMetricValue renders a CompareMetric value the same way in the
 * compare table and every chart tooltip/axis, so a number means the same
 * thing wherever it appears - ported unchanged from the deleted
 * web/frontend/lib/format.ts. */
export function formatMetricValue(value: number | undefined, unit: string): string {
  if (value === undefined) {
    return '-';
  }
  if (unit === 'ratio') {
    return `${(value * 100).toFixed(1)}%`;
  }
  if (unit === 's') {
    return value < 1 ? `${(value * 1000).toFixed(0)} ms` : `${value.toFixed(2)} s`;
  }
  return value >= 100 ? value.toFixed(0) : value.toFixed(2);
}

// A small qualitative palette, cycled by index so the same test keeps the
// same color across every chart on a page. The old web/frontend used
// PatternFly's own chart color tokens; this plugin isn't built on
// PatternFly (Headlamp's shared deps are Material-UI, not PatternFly), so
// this is a plain hand-picked equivalent instead of a design-system import.
const palette = [
  '#0066cc',
  '#4cb140',
  '#f0ab00',
  '#c9190b',
  '#8476d1',
  '#009596',
  '#ec7a08',
  '#6ca100',
];

export function colorFor(index: number): string {
  return palette[index % palette.length];
}
