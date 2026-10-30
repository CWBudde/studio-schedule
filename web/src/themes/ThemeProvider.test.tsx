import { act, screen, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { setBackendStatus } from '@/api/backendStatus';
import { mockApiGet, publicSettings, signedOut } from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import type { Theme, ThemeId } from './slots';
import { THEME_CACHE_KEY } from './themeCache';
import { ThemeProvider } from './ThemeProvider';
import { useSlot } from './useSlot';

afterEach(() => {
  vi.restoreAllMocks();
  setBackendStatus({ state: 'ready' });
  localStorage.removeItem(THEME_CACHE_KEY);
  delete document.documentElement.dataset.theme;
});

/** A stand-in for Werkstatt's chunk, which does not exist yet: a Shell of its own. */
const werkstatt: Theme = {
  id: 'werkstatt',
  slots: {
    Shell: ({ children }: { children: ReactNode }) => <div>Werkstatt shell {children}</div>,
  },
};

/** A page in the active theme's Shell. */
function Page() {
  const Shell = useSlot('Shell');
  return (
    // eslint-disable-next-line react-hooks/static-components
    <Shell
      banners={null}
      announcements={null}
      header={{ nav: [], tools: null, account: null, language: null }}
      footer={null}
    >
      <p>The page</p>
    </Shell>
  );
}

/** A promise settled from outside: a theme chunk that is still on its way. */
function deferred<T>() {
  let resolve: (value: T) => void = () => undefined;
  let reject: (reason: unknown) => void = () => undefined;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function settingsWith(theme: ThemeId) {
  return {
    ...signedOut,
    '/api/v1/settings/public': { body: { ...publicSettings, theme } },
  };
}

function render(load: (id: ThemeId) => Promise<Theme>) {
  return renderInRouter(
    <ThemeProvider load={load}>
      <Page />
    </ThemeProvider>,
  );
}

const programmeChrome = () => screen.queryByRole('link', { name: 'Skip to content' });
const loading = () => screen.queryByRole('status');

describe('ThemeProvider', () => {
  it('shows Programme without a cached theme or settings, at once', async () => {
    setBackendStatus({ state: 'loading' });
    const load = vi.fn<(id: ThemeId) => Promise<Theme>>();

    render(load);

    expect(await screen.findByText('The page')).toBeInTheDocument();
    expect(programmeChrome()).toBeInTheDocument();
    expect(document.documentElement.dataset.theme).toBe('programme');
    expect(load).not.toHaveBeenCalled();
  });

  it("uses the cached theme until the settings arrive, then the school's", async () => {
    localStorage.setItem(THEME_CACHE_KEY, 'werkstatt');
    setBackendStatus({ state: 'loading' });
    mockApiGet(settingsWith('programme'));
    const load = vi.fn(() => Promise.resolve(werkstatt));

    render(load);

    expect(await screen.findByText(/Werkstatt shell/)).toBeInTheDocument();
    expect(load).toHaveBeenCalledWith('werkstatt');
    expect(document.documentElement.dataset.theme).toBe('werkstatt');

    act(() => setBackendStatus({ state: 'ready' }));

    expect(await screen.findByRole('link', { name: 'Skip to content' })).toBeInTheDocument();
    expect(screen.queryByText(/Werkstatt shell/)).not.toBeInTheDocument();
    expect(screen.getByText('The page')).toBeInTheDocument();
    expect(document.documentElement.dataset.theme).toBe('programme');
    expect(localStorage.getItem(THEME_CACHE_KEY)).toBe('programme');
  });

  it("ends on the school's Programme while the cached theme's chunk still loads", async () => {
    localStorage.setItem(THEME_CACHE_KEY, 'werkstatt');
    setBackendStatus({ state: 'loading' });
    mockApiGet(settingsWith('programme'));
    const chunk = deferred<Theme>();
    const load = vi.fn(() => chunk.promise);

    render(load);

    // Loading, and not in Programme's chrome.
    await waitFor(() => expect(loading()).toHaveTextContent("Loading the school's look…"));
    expect(programmeChrome()).not.toBeInTheDocument();

    act(() => setBackendStatus({ state: 'ready' }));
    expect(await screen.findByRole('link', { name: 'Skip to content' })).toBeInTheDocument();

    // The late chunk does not take over.
    await act(async () => {
      chunk.resolve(werkstatt);
      await chunk.promise;
    });
    expect(programmeChrome()).toBeInTheDocument();
    expect(screen.queryByText(/Werkstatt shell/)).not.toBeInTheDocument();
    expect(document.documentElement.dataset.theme).toBe('programme');
    expect(localStorage.getItem(THEME_CACHE_KEY)).toBe('programme');
    expect(load).toHaveBeenCalledTimes(1);
  });

  it("loads the school's theme over a cached Programme, without Programme's chrome meanwhile", async () => {
    localStorage.setItem(THEME_CACHE_KEY, 'programme');
    setBackendStatus({ state: 'loading' });
    mockApiGet(settingsWith('werkstatt'));
    const chunk = deferred<Theme>();
    const load = vi.fn(() => chunk.promise);

    render(load);
    expect(await screen.findByRole('link', { name: 'Skip to content' })).toBeInTheDocument();

    act(() => setBackendStatus({ state: 'ready' }));

    await waitFor(() => expect(loading()).toHaveTextContent("Loading the school's look…"));
    expect(programmeChrome()).not.toBeInTheDocument();
    expect(screen.queryByText('The page')).not.toBeInTheDocument();
    expect(load).toHaveBeenCalledWith('werkstatt');

    await act(async () => {
      chunk.resolve(werkstatt);
      await chunk.promise;
    });

    expect(screen.getByText(/Werkstatt shell/)).toHaveTextContent('The page');
    expect(loading()).not.toBeInTheDocument();
    expect(document.documentElement.dataset.theme).toBe('werkstatt');
    expect(localStorage.getItem(THEME_CACHE_KEY)).toBe('werkstatt');
  });

  it('shows Programme when the theme fails to load, and does not cache it', async () => {
    mockApiGet(settingsWith('werkstatt'));
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    const load = vi.fn(() => Promise.reject(new Error('offline')));

    render(load);

    await waitFor(() =>
      expect(warn).toHaveBeenCalledWith(expect.stringContaining('werkstatt'), expect.any(Error)),
    );
    expect(load).toHaveBeenCalledWith('werkstatt');
    expect(programmeChrome()).toBeInTheDocument();
    expect(screen.getByText('The page')).toBeInTheDocument();
    expect(document.documentElement.dataset.theme).toBe('programme');
    // The school's theme is still Werkstatt: the next start tries it again.
    expect(localStorage.getItem(THEME_CACHE_KEY)).toBeNull();
  });

  it('takes its theme off the page when it unmounts', async () => {
    const { unmount } = render(vi.fn());
    await waitFor(() => expect(document.documentElement.dataset.theme).toBe('programme'));

    unmount();

    expect(document.documentElement.dataset.theme).toBeUndefined();
  });
});
