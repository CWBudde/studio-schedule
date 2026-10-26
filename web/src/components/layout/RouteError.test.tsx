import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { RouteError } from './RouteError';

afterEach(() => vi.restoreAllMocks());

describe('RouteError', () => {
  it('shows a translated card, not the error, and retries on demand', async () => {
    const reset = vi.fn();
    const user = userEvent.setup();
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined);
    const error = new Error('boom');

    render(<RouteError error={error} reset={reset} />);

    expect(screen.getByRole('alert')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Something went wrong' })).toBeInTheDocument();
    expect(screen.queryByText('boom')).toBeNull();
    expect(logged).toHaveBeenCalledWith('route render failed', error);

    await user.click(screen.getByRole('button', { name: 'Try again' }));
    expect(reset).toHaveBeenCalledTimes(1);
  });

  it('offers only a reload when a code chunk failed to load', () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    const error = new TypeError(
      'Failed to fetch dynamically imported module: https://school.example.org/assets/x.js',
    );

    render(<RouteError error={error} reset={vi.fn()} />);

    expect(screen.getByText(/Reload the page to get the latest version/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Try again' })).toBeNull();
    expect(screen.getByRole('button', { name: 'Reload page' })).toBeInTheDocument();
  });
});
