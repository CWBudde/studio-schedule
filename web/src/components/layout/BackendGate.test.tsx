import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { toast } from 'sonner';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { setBackendStatus } from '@/api/backendStatus';
import type { DemoNotice } from '@/api/transport';
import { BackendGate } from './BackendGate';
import { DemoNotices } from './DemoNotices';

const controls = vi.hoisted(() => ({
  reset: vi.fn<() => Promise<void>>(),
  takeOver: vi.fn(),
  subscribeNotices: vi.fn<(listener: (notice: DemoNotice) => void) => () => void>(),
  getTab: () => 'active',
  subscribeTab: () => () => undefined,
}));
vi.mock('@/api/transport', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/transport')>()),
  demoControls: controls,
}));

afterEach(() => {
  vi.restoreAllMocks();
  setBackendStatus({ state: 'ready' });
});

describe('BackendGate in the web demo', () => {
  it('shows the app while the demo runs in this tab', () => {
    render(<BackendGate>app</BackendGate>);
    expect(screen.getByText('app')).toBeInTheDocument();
  });

  it('offers to move the demo here while another tab runs it', async () => {
    setBackendStatus({ state: 'elsewhere' });
    const user = userEvent.setup();

    render(<BackendGate>app</BackendGate>);

    expect(
      screen.getByRole('heading', { name: 'The demo is open in another tab' }),
    ).toBeInTheDocument();
    expect(screen.queryByText('app')).not.toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Use here' }));
    expect(controls.takeOver).toHaveBeenCalledTimes(1);
  });

  it('offers a reset next to the reload after a crash', () => {
    setBackendStatus({ state: 'error', message: 'panic' });

    render(<BackendGate>app</BackendGate>);

    expect(screen.getByRole('button', { name: 'Reload' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Reset demo' })).toBeInTheDocument();
  });
});

describe('DemoNotices', () => {
  it('tells the visitor once that changes cannot be saved', () => {
    const warning = vi.spyOn(toast, 'warning');
    let deliver: (notice: DemoNotice) => void = () => undefined;
    controls.subscribeNotices.mockImplementation((listener) => {
      deliver = listener;
      return () => undefined;
    });

    render(<DemoNotices />);
    deliver('save-failed');

    expect(warning).toHaveBeenCalledWith(
      expect.stringContaining('cannot be saved in this browser'),
      expect.anything(),
    );
  });
});
