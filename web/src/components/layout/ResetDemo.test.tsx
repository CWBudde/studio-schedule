import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { setBackendStatus } from '@/api/backendStatus';
import type { DemoTab } from '@/api/transport';
import { mockApiGet, signedOut } from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { ResetDemo } from './ResetDemo';

// As in the web demo: the transport offers demo controls, and this tab holds
// the demo's lock unless a test says otherwise.
const controls = vi.hoisted(() => {
  const state = { tab: 'active' as DemoTab };
  return {
    state,
    reset: vi.fn<() => Promise<void>>(),
    takeOver: vi.fn(),
    subscribeNotices: vi.fn(() => () => undefined),
    getTab: () => state.tab,
    subscribeTab: () => () => undefined,
  };
});
vi.mock('@/api/transport', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/transport')>()),
  demoControls: controls,
}));

afterEach(() => {
  vi.restoreAllMocks();
  controls.reset.mockReset();
  controls.state.tab = 'active';
  setBackendStatus({ state: 'ready' });
});

describe('ResetDemo', () => {
  it('is there while this tab holds the demo, mailbox or not, even while it starts', async () => {
    // No mailbox (yet): the button does not wait for it.
    mockApiGet({ ...signedOut, '/api/v1/demo/mailbox': { status: 404, body: undefined } });
    setBackendStatus({ state: 'loading' });

    renderInRouter(<ResetDemo />);

    expect(await screen.findByRole('button', { name: 'Reset demo' })).toBeInTheDocument();
  });

  it.each<DemoTab>(['pending', 'elsewhere'])(
    'is absent while this tab does not hold the demo (%s)',
    async (tab) => {
      mockApiGet(signedOut);
      controls.state.tab = tab;

      renderInRouter(
        <>
          <h1>Page</h1>
          <ResetDemo />
        </>,
      );

      expect(await screen.findByRole('heading', { name: 'Page' })).toBeInTheDocument();
      expect(screen.queryByRole('button', { name: 'Reset demo' })).not.toBeInTheDocument();
    },
  );

  it('asks first and does nothing when canceled', async () => {
    mockApiGet(signedOut);
    const user = userEvent.setup();

    renderInRouter(<ResetDemo />);

    await user.click(await screen.findByRole('button', { name: 'Reset demo' }));
    const dialog = await screen.findByRole('alertdialog', { name: 'Reset the demo?' });
    expect(dialog).toHaveTextContent('signs you out');

    await user.click(screen.getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
    expect(controls.reset).not.toHaveBeenCalled();
  });

  it('resets the demo when confirmed', async () => {
    mockApiGet(signedOut);
    // The page reloads when the reset is done, so the promise never settles here.
    controls.reset.mockReturnValue(new Promise(() => undefined));
    const user = userEvent.setup();

    renderInRouter(<ResetDemo />);

    await user.click(await screen.findByRole('button', { name: 'Reset demo' }));
    await screen.findByRole('alertdialog', { name: 'Reset the demo?' });
    await user.click(screen.getByRole('button', { name: 'Reset demo' }));

    expect(controls.reset).toHaveBeenCalledTimes(1);
    expect(await screen.findByRole('button', { name: 'Resetting…' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled();
  });
});
