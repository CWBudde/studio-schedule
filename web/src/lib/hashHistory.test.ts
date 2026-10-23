import { afterEach, describe, expect, it } from 'vitest';
import { createAppHashHistory } from './hashHistory';

afterEach(() => {
  window.history.replaceState(null, '', '/');
});

describe('createAppHashHistory', () => {
  it('reads the route search from the hash only', () => {
    // E.g. a shared link that got a tracking parameter appended in front of the hash.
    window.history.replaceState(null, '', '/demo/?fbclid=abc#/schedule?from=2026-10-26&view=week');

    const history = createAppHashHistory();

    expect(history.location.pathname).toBe('/schedule');
    expect(history.location.search).toBe('?from=2026-10-26&view=week');
    expect(window.location.href).toBe(
      'http://localhost:3000/demo/#/schedule?from=2026-10-26&view=week',
    );
    history.destroy();
  });

  it('leaves URLs without a query string alone', () => {
    window.history.replaceState(null, '', '/demo/#/schedule?view=week');

    const history = createAppHashHistory();

    expect(history.location.search).toBe('?view=week');
    expect(window.location.pathname + window.location.hash).toBe('/demo/#/schedule?view=week');
    history.destroy();
  });
});
