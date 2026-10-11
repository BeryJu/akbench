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
import { sanitizePrefill, shellLink, shellLinkForCluster } from './links';

describe('sanitizePrefill', () => {
  it('keeps an ordinary command intact', () => {
    expect(sanitizePrefill('kubectl get pods -n ak-1 | grep -v Running')).toBe(
      'kubectl get pods -n ak-1 | grep -v Running'
    );
  });

  it('never lets a link press Enter or send escape sequences', () => {
    const out = sanitizePrefill('true\rrm -rf /\nx\u001b[2J\u0003\u009b');
    expect(out).not.toMatch(/[\r\n\u001b\u0003\u009b]/);
  });

  it('handles a missing parameter', () => {
    expect(sanitizePrefill(null)).toBe('');
  });
});

describe('shell links', () => {
  it('encodes context and prefill', () => {
    expect(shellLink('wc1-admin@wc1', 'kubectl get nodes')).toBe(
      '/shell?context=wc1-admin%40wc1&prefill=kubectl+get+nodes'
    );
  });

  it('falls back to the management context without a cluster', () => {
    expect(shellLinkForCluster(undefined)).toBe('/shell?context=management');
    expect(shellLinkForCluster('wc1')).toBe('/shell?cluster=wc1');
  });
});
