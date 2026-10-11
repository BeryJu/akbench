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
import {
  colorFor,
  formatDuration,
  formatMetricValue,
  metricsAnchorFromRuns,
  relativeDelta,
  shortTag,
  stepFor,
  targetNameOf,
} from './format';

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

describe('formatDuration', () => {
  it('renders seconds and minutes as a single unit', () => {
    expect(formatDuration(42)).toBe('42s');
    expect(formatDuration(7 * 60 + 5)).toBe('7m');
  });

  it('renders hours and days as their two largest units', () => {
    expect(formatDuration(19 * 3600 + 12 * 60)).toBe('19h 12m');
    expect(formatDuration(3 * 86400 + 4 * 3600 + 59)).toBe('3d 4h');
  });

  it('clamps a negative duration (clock skew) to zero', () => {
    expect(formatDuration(-5)).toBe('0s');
  });
});

describe('relativeDelta', () => {
  it('is the change relative to the baseline', () => {
    expect(relativeDelta(0.362, 0.421)).toBeCloseTo(-0.140, 3);
    expect(relativeDelta(194, 181)).toBeCloseTo(0.0718, 3);
  });

  it('is undefined without both values or with a zero baseline', () => {
    expect(relativeDelta(undefined, 1)).toBeUndefined();
    expect(relativeDelta(1, undefined)).toBeUndefined();
    expect(relativeDelta(1, 0)).toBeUndefined();
  });
});

describe('stepFor', () => {
  it('never goes finer than 15s', () => {
    expect(stepFor(900)).toBe('15s');
  });

  it('keeps long ranges to about 240 points', () => {
    expect(stepFor(7 * 86400)).toBe('2520s');
  });
});

describe('targetNameOf', () => {
  it('prefers spec.targetName and falls back to the object name', () => {
    expect(targetNameOf({ metadata: { name: 'ak-a' }, spec: { targetName: 'a' } })).toBe('a');
    expect(targetNameOf({ metadata: { name: 'ak-a' }, spec: {} })).toBe('ak-a');
  });
});

describe('shortTag', () => {
  it('abbreviates a trailing full commit hash', () => {
    expect(shortTag('gh-ccd5f45863edfb003c4dd876fabfcb0123456789')).toBe('gh-ccd5f45');
  });

  it('leaves other tags alone', () => {
    expect(shortTag('2026.8.2')).toBe('2026.8.2');
    expect(shortTag('gh-main')).toBe('gh-main');
  });
});
