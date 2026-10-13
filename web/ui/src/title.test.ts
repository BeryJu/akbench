import { describe, expect, it } from 'vitest';
import { APP_TITLE, pageTitle } from './title';

describe('pageTitle', () => {
  it('falls back to the app name', () => {
    expect(pageTitle([])).toBe(APP_TITLE);
    expect(pageTitle([{ params: {}, handle: undefined }])).toBe(APP_TITLE);
  });

  it('uses the deepest route with a title', () => {
    expect(
      pageTitle([
        { params: {}, handle: { title: 'Outer' } },
        {
          params: { name: 'foo' },
          handle: { title: (p: { name?: string }) => `Deployment ${p.name}` },
        },
      ])
    ).toBe(`Deployment foo · ${APP_TITLE}`);
  });

  it('skips routes without a title', () => {
    expect(
      pageTitle([
        { params: {}, handle: { title: 'Test runs' } },
        { params: {}, handle: undefined },
      ])
    ).toBe(`Test runs · ${APP_TITLE}`);
  });
});
