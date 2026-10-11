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

import { describe, expect, it } from 'vitest';
import { colorFor, formatMetricValue, metricsAnchorFromRuns } from './format';

describe('formatMetricValue', () => {
  it('renders undefined as a dash', () => {
    expect(formatMetricValue(undefined, 's')).toBe('-');
  });

  it('renders a ratio as a percentage with one decimal', () => {
    expect(formatMetricValue(0.4567, 'ratio')).toBe('45.7%');
  });

  it('renders sub-second durations in milliseconds', () => {
    expect(formatMetricValue(0.234, 's')).toBe('234 ms');
  });

  it('renders one-second-or-longer durations in seconds', () => {
    expect(formatMetricValue(1.5, 's')).toBe('1.50 s');
  });

  it('renders large plain numbers with no decimals', () => {
    expect(formatMetricValue(1234, 'req/s')).toBe('1234');
  });

  it('renders small plain numbers with two decimals', () => {
    expect(formatMetricValue(4.2, 'req/s')).toBe('4.20');
  });
});

describe('colorFor', () => {
  it('returns a stable color for the same index', () => {
    expect(colorFor(0)).toBe(colorFor(0));
  });

  it('cycles once the palette is exhausted', () => {
    // The palette has 8 entries - index 8 must wrap back to index 0's color,
    // so two different tests eventually reuse a color rather than the
    // function throwing/returning undefined past the palette's length.
    expect(colorFor(8)).toBe(colorFor(0));
  });
});

describe('metricsAnchorFromRuns', () => {
  it('returns undefined with no run history', () => {
    expect(metricsAnchorFromRuns([])).toBeUndefined();
  });

  it('returns undefined when the latest run is still in flight', () => {
    const runs = [{ status: { startTime: '2026-09-15T12:00:00Z' } }];
    expect(metricsAnchorFromRuns(runs)).toBeUndefined();
  });

  it('anchors to the latest completed run, not just the last in the list', () => {
    const older = {
      status: { startTime: '2026-09-15T10:00:00Z', completionTime: '2026-09-15T10:05:00Z' },
    };
    const newer = {
      status: { startTime: '2026-09-15T12:00:00Z', completionTime: '2026-09-15T12:05:00Z' },
    };
    // Deliberately listed out of chronological order - anchoring must sort
    // by completionTime itself, not assume array order.
    const anchor = metricsAnchorFromRuns([older, newer]);
    const wantEnd = Math.floor(Date.parse(newer.status.completionTime) / 1000) + 60;
    expect(anchor?.endSeconds).toBe(wantEnd);
  });

  it('pads the range so the run\'s own start/end both fall inside the window', () => {
    const run = {
      status: { startTime: '2026-09-15T12:00:00Z', completionTime: '2026-09-15T12:05:00Z' },
    };
    const anchor = metricsAnchorFromRuns([run]);
    expect(anchor).toBeDefined();
    const startSeconds = anchor!.endSeconds - anchor!.rangeSeconds;
    expect(startSeconds).toBeLessThanOrEqual(Math.floor(Date.parse(run.status.startTime) / 1000));
    // A floor: even a very short run gets at least a 900s window, matching
    // the function's own documented minimum.
    expect(anchor!.rangeSeconds).toBeGreaterThanOrEqual(900);
  });

  it('falls back to a default lookback when startTime is missing', () => {
    const run = { status: { completionTime: '2026-09-15T12:05:00Z' } };
    const anchor = metricsAnchorFromRuns([run]);
    // No startTime -> the function's own 900s default lookback, plus its
    // fixed 120s padding: 900 + 120 = 1020, not the 900s floor itself.
    expect(anchor!.rangeSeconds).toBe(1020);
  });
});
