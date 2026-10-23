import { QueryClientProvider } from '@tanstack/react-query';
import { act, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import i18n from '@/i18n';
import { renderWithQuery } from '@/test/utils';
import { ReleaseCountdown } from './ReleaseCountdown';

const SECOND = 1000;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;

// The client clock runs 2 hours ahead of the server's clock in every test.
const skew = 2 * HOUR;

/** Renders the countdown for a response that arrived `age` ms ago. */
function renderCountdown(serverNow: string, nextReleaseAt: string, age = 0) {
  const receivedAt = Date.parse(serverNow) + skew;
  vi.setSystemTime(receivedAt + age);
  const result = renderWithQuery(
    <ReleaseCountdown
      nextReleaseAt={nextReleaseAt}
      now={serverNow}
      receivedAt={receivedAt}
      timeZone="Europe/Berlin"
    />,
  );
  const invalidate = vi.spyOn(result.queryClient, 'invalidateQueries');
  return { ...result, invalidate };
}

function advance(ms: number) {
  act(() => {
    vi.advanceTimersByTime(ms);
  });
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe('ReleaseCountdown', () => {
  it('counts down by the server clock, not the skewed client clock', () => {
    renderCountdown('2026-10-23T10:00:00Z', '2026-10-25T17:00:00Z');
    advance(0);

    const timer = screen.getByRole('timer');
    expect(timer).toHaveTextContent('Next release in 2 days and 7 hours');
    expect(timer).toHaveAttribute('aria-live', 'off');
    // 17:00 UTC is 18:00 in Berlin (CET from that morning on).
    expect(screen.getByText('Release: Sun, Oct 25, 18:00')).toBeInTheDocument();

    advance(MINUTE);
    expect(timer).toHaveTextContent('Next release in 2 days and 6 hours');

    advance(23 * HOUR + 59 * MINUTE);
    expect(timer).toHaveTextContent('Next release in 1 day and 7 hours');

    advance(30 * HOUR);
    expect(timer).toHaveTextContent('Next release in 1 hour');
  });

  it('counts from the time the response arrived, not from the render', () => {
    // The schedule came from the cache and is 5 minutes old.
    renderCountdown('2026-10-25T15:00:00Z', '2026-10-25T17:00:00Z', 5 * MINUTE);
    advance(0);

    expect(screen.getByRole('timer')).toHaveTextContent('Next release in 1 hour and 55 minutes');
  });

  it('ticks every second in the last hour and refetches the schedule at the release', () => {
    const { invalidate } = renderCountdown('2026-10-25T15:58:30Z', '2026-10-25T17:00:00Z');
    advance(0);

    const timer = screen.getByRole('timer');
    expect(timer).toHaveTextContent('Next release in 1 hour and 2 minutes');

    advance(MINUTE);
    expect(timer).toHaveTextContent('Next release in 1 hour and 1 minute');

    advance(MINUTE);
    expect(timer).toHaveTextContent('Next release in 59:30');

    advance(SECOND);
    expect(timer).toHaveTextContent('Next release in 59:29');

    advance(59 * MINUTE + 28 * SECOND);
    expect(timer).toHaveTextContent('Next release in 0:01');
    expect(invalidate).not.toHaveBeenCalled();

    advance(SECOND);
    expect(screen.getByText('New lessons are being released…')).toBeInTheDocument();
    expect(invalidate).toHaveBeenCalledTimes(1);
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['schedule'] });

    // While the release does not arrive (the server is late or the request
    // failed), it asks again every 30 seconds.
    advance(29 * SECOND);
    expect(invalidate).toHaveBeenCalledTimes(1);
    advance(SECOND);
    expect(invalidate).toHaveBeenCalledTimes(2);
  });

  it('starts over when the schedule brings the next release', () => {
    const { rerender, queryClient, invalidate } = renderCountdown(
      '2026-10-25T16:59:59Z',
      '2026-10-25T17:00:00Z',
    );
    advance(SECOND);
    expect(screen.getByText('New lessons are being released…')).toBeInTheDocument();
    expect(invalidate).toHaveBeenCalledTimes(1);

    rerender(
      <QueryClientProvider client={queryClient}>
        <ReleaseCountdown
          nextReleaseAt="2026-11-01T17:00:00Z"
          now="2026-10-25T17:00:02Z"
          receivedAt={Date.now()}
          timeZone="Europe/Berlin"
        />
      </QueryClientProvider>,
    );
    advance(0);

    expect(screen.getByRole('timer')).toHaveTextContent('Next release in 7 days');
    expect(screen.queryByText('New lessons are being released…')).not.toBeInTheDocument();
    advance(10 * MINUTE);
    expect(invalidate).toHaveBeenCalledTimes(1);
  });

  it('clears its timer on unmount', () => {
    const { unmount } = renderCountdown('2026-10-23T10:00:00Z', '2026-10-25T17:00:00Z');
    advance(0);
    expect(vi.getTimerCount()).toBeGreaterThan(0);

    unmount();

    expect(vi.getTimerCount()).toBe(0);
  });

  it('renders in German', async () => {
    await i18n.changeLanguage('de');
    renderCountdown('2026-10-23T10:00:00Z', '2026-10-25T17:00:00Z');
    advance(0);

    expect(screen.getByRole('timer')).toHaveTextContent(
      'Nächste Freigabe in 2 Tagen und 7 Stunden',
    );
    expect(screen.getByText('Freigabe: So., 25. Okt., 18:00')).toBeInTheDocument();
  });
});
