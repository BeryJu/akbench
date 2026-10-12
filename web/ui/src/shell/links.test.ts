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
