import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { DayHeadingProps } from '../slots';
import { DayHeading } from './DayHeading';

afterEach(() => {
  vi.restoreAllMocks();
});

/** Monday 2026-10-26, with no neighbours unless given. */
function renderHeading(props: Partial<DayHeadingProps> = {}) {
  return render(<DayHeading id="day" date="2026-10-26" tone="later" {...props} />);
}

describe('Werkstatt DayHeading', () => {
  it('names a later day by its date, in normal case (uppercase is CSS only)', () => {
    renderHeading();

    const heading = screen.getByRole('heading', { level: 2, name: 'Monday, October 26' });
    expect(heading).toHaveAttribute('id', 'day');
    expect(heading).not.toHaveAttribute('aria-current');
  });

  it('says today and marks it as the current date', () => {
    renderHeading({ tone: 'today' });

    expect(screen.getByRole('heading', { name: 'Today, Monday, October 26' })).toHaveAttribute(
      'aria-current',
      'date',
    );
  });

  it('says tomorrow', () => {
    renderHeading({ tone: 'tomorrow' });

    expect(
      screen.getByRole('heading', { name: 'Tomorrow, Monday, October 26' }),
    ).not.toHaveAttribute('aria-current');
  });

  it('links to the previous and the next day', () => {
    renderHeading({ prevId: 'prev', nextId: 'next' });

    expect(screen.getByRole('link', { name: 'Previous day' })).toHaveAttribute('href', '#prev');
    expect(screen.getByRole('link', { name: 'Next day' })).toHaveAttribute('href', '#next');
    // The links stay outside the heading, whose name is the date only.
    expect(screen.getByRole('heading', { name: 'Monday, October 26' })).toBeInTheDocument();
  });

  it('has no link where there is no neighbouring day', () => {
    renderHeading({ nextId: 'next' });

    expect(screen.queryByRole('link', { name: 'Previous day' })).not.toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Next day' })).toBeInTheDocument();
  });

  it('scrolls to and focuses the neighbouring heading without changing the route', async () => {
    const user = userEvent.setup();
    const scroll = vi.spyOn(Element.prototype, 'scrollIntoView');
    render(
      <>
        <DayHeading id="prev" date="2026-10-25" tone="past" nextId="day" />
        <DayHeading id="day" date="2026-10-26" tone="today" prevId="prev" nextId="next" />
        <DayHeading id="next" date="2026-10-27" tone="tomorrow" prevId="day" />
      </>,
    );
    const hash = window.location.hash;

    const [, toNext] = screen.getAllByRole('link', { name: 'Next day' });
    await user.click(toNext!);

    const next = screen.getByRole('heading', { name: /^Tomorrow/ });
    expect(scroll).toHaveBeenCalledTimes(1);
    expect(scroll.mock.contexts[0]).toBe(next);
    expect(scroll).toHaveBeenCalledWith({ block: 'start' });
    expect(next).toHaveFocus();
    expect(window.location.hash).toBe(hash);

    const [toPrev] = screen.getAllByRole('link', { name: 'Previous day' });
    await user.click(toPrev!);
    expect(screen.getByRole('heading', { name: 'Sunday, October 25' })).toHaveFocus();
    expect(window.location.hash).toBe(hash);
  });
});
