import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

// A fresh module per test: the choice is kept in memory, too.
let school: typeof import('./school');
let entered: typeof import('./entered');

beforeEach(async () => {
  vi.resetModules();
  school = await import('./school');
  entered = await import('./entered');
});

afterEach(() => {
  vi.restoreAllMocks();
  localStorage.removeItem('studio-schedule-demo-school');
  localStorage.removeItem('studio-schedule-demo-entered');
  history.replaceState(null, '', '/');
});

describe('demoSchool', () => {
  it('is programme until the visitor chooses', () => {
    expect(school.demoSchool()).toBe('programme');
  });

  it('is the stored choice', () => {
    localStorage.setItem('studio-schedule-demo-school', 'werkstatt');
    expect(school.demoSchool()).toBe('werkstatt');
  });

  it('ignores a stored value that names no school', () => {
    localStorage.setItem('studio-schedule-demo-school', 'Werkstatt');
    expect(school.demoSchool()).toBe('programme');
  });

  it('stores the choice under its key', () => {
    school.chooseDemoSchool('werkstatt');
    expect(localStorage.getItem(school.DEMO_SCHOOL_KEY)).toBe('werkstatt');
    expect(school.demoSchool()).toBe('werkstatt');
  });

  it('keeps the choice for this page load when storage is unavailable', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new DOMException('blocked', 'SecurityError');
    });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('blocked', 'SecurityError');
    });

    expect(school.demoSchool()).toBe('programme');
    expect(() => school.chooseDemoSchool('werkstatt')).not.toThrow();
    expect(school.demoSchool()).toBe('werkstatt');
  });
});

describe('takeDemoParam', () => {
  it('reads ?demo= and removes it from the address, hash and other params kept', () => {
    history.replaceState(null, '', '/studio-schedule/?demo=werkstatt&ref=x#/schedule');

    expect(school.takeDemoParam(new URL(location.href))).toBe('werkstatt');
    expect(location.pathname).toBe('/studio-schedule/');
    expect(location.search).toBe('?ref=x');
    expect(location.hash).toBe('#/schedule');
  });

  it('reads programme', () => {
    history.replaceState(null, '', '/?demo=programme');
    expect(school.takeDemoParam(new URL(location.href))).toBe('programme');
    expect(location.search).toBe('');
  });

  it('removes a value that names no school and returns nothing', () => {
    history.replaceState(null, '', '/?demo=elsewhere');
    expect(school.takeDemoParam(new URL(location.href))).toBeUndefined();
    expect(location.search).toBe('');
  });

  it('leaves an address without the param alone', () => {
    history.replaceState(null, '', '/?ref=x#/about');
    const replace = vi.spyOn(history, 'replaceState');

    expect(school.takeDemoParam(new URL(location.href))).toBeUndefined();
    expect(replace).not.toHaveBeenCalled();
  });

  it('does not store the school by itself', () => {
    history.replaceState(null, '', '/?demo=werkstatt');
    school.takeDemoParam(new URL(location.href));
    expect(localStorage.getItem('studio-schedule-demo-school')).toBeNull();
  });
});

describe('switchDemoSchool', () => {
  it('stores the school, enters the demo and reloads at the home page', () => {
    history.replaceState(null, '', '/#/schedule?view=week');
    let reloadedAt = '';
    const reload = vi.fn(() => {
      // What the reload loads: the address as switchDemoSchool left it.
      reloadedAt = location.pathname + location.hash;
    });

    school.switchDemoSchool('werkstatt', reload);

    expect(reload).toHaveBeenCalledOnce();
    expect(reloadedAt).toMatch(/^\/(#\/)?$/);
    expect(school.demoSchool()).toBe('werkstatt');
    expect(localStorage.getItem('studio-schedule-demo-school')).toBe('werkstatt');
    expect(entered.hasEnteredDemo()).toBe(true);
  });
});
