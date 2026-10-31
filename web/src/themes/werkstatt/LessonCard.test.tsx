import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import type { ScheduleLesson } from '@/api/client';
import type { QuickAction } from '@/features/schedule/booking/quickAction';
import i18n from '@/i18n';
import { courseTint } from './courseTint';
import { LessonCard } from './LessonCard';

const timeZone = 'Europe/Berlin';
/** Friday 2026-10-23, before the lesson. */
const now = Date.parse('2026-10-23T10:00:00Z');

/** Ballet on Monday 2026-10-26, 18:00–19:30 in Berlin: 8 places, 3 free. */
const ballet: ScheduleLesson = {
  id: '0190a000-0000-7000-8000-000000000101',
  classId: '0190a000-0000-7000-8000-000000000201',
  courseType: { id: '0190a000-0000-7000-8000-000000000031', name: 'Ballet' },
  room: { id: '0190a000-0000-7000-8000-000000000011', name: 'Room A' },
  location: { id: '0190a000-0000-7000-8000-000000000001', name: 'North Studio' },
  instructor: { id: '0190a000-0000-7000-8000-000000000021', displayName: 'Alex Example' },
  accompanist: { id: '0190a000-0000-7000-8000-000000000022', displayName: 'Sam Pianist' },
  startsAt: '2026-10-26T17:00:00Z',
  endsAt: '2026-10-26T18:30:00Z',
  bookingClosesAt: '2026-10-26T16:00:00Z',
  lateCancelFrom: '2026-10-25T17:00:00Z',
  status: 'scheduled',
  kind: 'regular',
  level: 'beginner',
  capacity: 8,
  visibility: 'public',
  freePlaces: 3,
};

const label = 'Ballet, Monday, October 26, 18:00–19:30';

const booked: ScheduleLesson['bookedFor'] = [
  {
    personId: '0190a000-0000-7000-8000-000000000331',
    name: 'Nora Example',
    bookingId: '0190a000-0000-7000-8000-000000000601',
  },
];

function write(kind: 'book' | 'cancel' | 'waitlist', pending = false) {
  return {
    kind,
    personId: '0190a000-0000-7000-8000-000000000331',
    run: vi.fn(),
    pending,
  } satisfies QuickAction;
}

function renderCard({
  lesson = ballet,
  quickAction = { kind: 'none' },
  past = false,
  name = label,
}: {
  lesson?: ScheduleLesson;
  quickAction?: QuickAction;
  past?: boolean;
  /** The card's name, in the test's language. */
  name?: string | RegExp;
} = {}) {
  const onOpen = vi.fn();
  render(
    <LessonCard
      lesson={lesson}
      timeZone={timeZone}
      now={now}
      past={past}
      quickAction={quickAction}
      onOpen={onOpen}
    />,
  );
  const card = within(screen.getByRole('group', { name }));
  return { onOpen, card };
}

describe('Werkstatt LessonCard', () => {
  it('shows the lesson: course type, level, time, place, participants and staff', () => {
    const { card } = renderCard();

    expect(card.getByText('Ballet')).toBeInTheDocument();
    expect(card.getByText('Beginner')).toBeInTheDocument();
    expect(card.getByText('18:00–19:30')).toHaveClass('text-primary');
    expect(card.getByText('Room A · North Studio')).toBeInTheDocument();
    // 8 places, 3 free: 5 taken, in green while places are free.
    expect(card.getByText('Participants: 5 / 8')).toHaveClass('text-(--werkstatt-green)');
    expect(card.getByText('Alex Example (Sam Pianist)')).toHaveClass('text-primary');
    expect(card.getByRole('button', { name: label })).toHaveAccessibleDescription(
      /Room A · North Studio.*Participants: 5 \/ 8.*Alex Example \(Sam Pianist\)/,
    );
  });

  it('tints the card by its course type', () => {
    renderCard();

    const group = screen.getByRole('group', { name: label });
    expect(group.style.getPropertyValue('--course-tint')).toBe(
      courseTint('0190a000-0000-7000-8000-000000000031'),
    );
  });

  it('shows a full lesson in red', () => {
    const { card } = renderCard({ lesson: { ...ballet, freePlaces: 0 } });

    expect(card.getByText('Participants: 8 / 8')).toHaveClass('text-destructive');
  });

  it('names the instructor alone without an accompanist', () => {
    const { card } = renderCard({ lesson: { ...ballet, accompanist: undefined } });

    expect(card.getByText('Alex Example')).toBeInTheDocument();
    expect(card.queryByText(/\(/)).not.toBeInTheDocument();
  });

  it('shows a canceled lesson as canceled, untinted and without participants', () => {
    renderCard({ lesson: { ...ballet, status: 'canceled' }, name: `${label}, Canceled` });

    const group = screen.getByRole('group', { name: `${label}, Canceled` });
    expect(group).toHaveAttribute('data-canceled');
    expect(group.style.getPropertyValue('--course-tint')).toBe('');
    expect(within(group).getByText('Canceled')).toBeInTheDocument();
    expect(within(group).getByText('Ballet')).toHaveClass('line-through');
    expect(within(group).queryByText(/Participants/)).not.toBeInTheDocument();
  });

  it('dims an ended lesson by colour: no tint, no primary, no participants', () => {
    const { card } = renderCard({ past: true });

    const group = screen.getByRole('group', { name: label });
    expect(group).toHaveAttribute('data-past');
    expect(group).not.toHaveClass('opacity-50');
    expect(group.style.getPropertyValue('--course-tint')).toBe('');
    expect(card.getByText('18:00–19:30')).not.toHaveClass('text-primary');
    expect(card.queryByText(/Participants/)).not.toBeInTheDocument();
  });

  it('marks the kind and the visibility', () => {
    renderCard({
      lesson: { ...ballet, kind: 'workshop', visibility: 'members', level: undefined },
      name: `${label}, Workshop, Members only`,
    });

    const group = within(screen.getByRole('group', { name: `${label}, Workshop, Members only` }));
    expect(group.getByText('Workshop')).toBeInTheDocument();
    expect(group.getByText('Members only')).toBeInTheDocument();
    expect(group.queryByText('Beginner')).not.toBeInTheDocument();
  });

  it('opens the sheet from the card body', async () => {
    const user = userEvent.setup();
    const { onOpen, card } = renderCard();

    await user.click(card.getByRole('button', { name: label }));
    expect(onOpen).toHaveBeenCalledTimes(1);
  });

  it('marks a booked lesson with a star and a frame', () => {
    const { card } = renderCard({ lesson: { ...ballet, bookedFor: booked } });

    expect(card.getByRole('img', { name: 'Booked' })).toBeInTheDocument();
    expect(screen.getByRole('group', { name: label })).toHaveAttribute('data-booked');
  });

  it('has no star when not booked', () => {
    const { card } = renderCard();

    expect(card.queryByRole('img', { name: 'Booked' })).not.toBeInTheDocument();
    expect(screen.getByRole('group', { name: label })).not.toHaveAttribute('data-booked');
  });

  it.each([
    ['book', 'Book'],
    ['cancel', 'Cancel'],
    ['waitlist', 'Waiting list'],
  ] as const)('%s: the square button "%s" runs the action, not the sheet', async (kind, name) => {
    const user = userEvent.setup();
    const quickAction = write(kind);
    const { onOpen, card } = renderCard({ quickAction });

    await user.click(card.getByRole('button', { name }));
    expect(quickAction.run).toHaveBeenCalledTimes(1);
    expect(onOpen).not.toHaveBeenCalled();
  });

  it('with more than one bookable person (open) the square button opens the sheet and books nobody', async () => {
    const user = userEvent.setup();
    const { onOpen, card } = renderCard({ quickAction: { kind: 'open' } });

    expect(card.queryByRole('button', { name: 'Book' })).not.toBeInTheDocument();
    await user.click(card.getByRole('button', { name: 'Details' }));
    expect(onOpen).toHaveBeenCalledTimes(1);
  });

  it('full: a disabled button', () => {
    const { card } = renderCard({
      quickAction: { kind: 'full' },
      lesson: { ...ballet, freePlaces: 0 },
    });

    expect(card.getByRole('button', { name: 'Full' })).toBeDisabled();
  });

  it('none: only the card body is a button', () => {
    const { card } = renderCard({ quickAction: { kind: 'none' } });

    expect(card.getAllByRole('button')).toEqual([card.getByRole('button', { name: label })]);
  });

  it('is busy and disabled while the action runs', async () => {
    const user = userEvent.setup();
    const quickAction = write('book', true);
    const { card } = renderCard({ quickAction });

    const button = card.getByRole('button', { name: 'Book' });
    expect(button).toBeDisabled();
    expect(button).toHaveAttribute('aria-busy', 'true');
    await user.click(button);
    expect(quickAction.run).not.toHaveBeenCalled();
  });

  const german = 'Ballet, Montag, 26. Oktober, 18:00–19:30';

  it('speaks German', async () => {
    await i18n.changeLanguage('de');
    const { card } = renderCard({ quickAction: write('book'), name: german });

    expect(card.getByRole('button', { name: 'Eintragen' })).toBeInTheDocument();
    expect(card.getByText('Teilnehmende: 5 / 8')).toBeInTheDocument();
    expect(card.getByText('Anfänger')).toBeInTheDocument();
  });

  it.each([
    ['cancel', write('cancel'), 'Austragen'],
    ['waitlist', write('waitlist'), 'Warteliste'],
    ['full', { kind: 'full' }, 'Voll'],
    ['open', { kind: 'open' }, 'Details'],
  ] as const)('speaks German: %s is "%s"', async (_kind, quickAction, name) => {
    await i18n.changeLanguage('de');
    const { card } = renderCard({ quickAction, name: german });

    expect(card.getByRole('button', { name })).toBeInTheDocument();
  });

  it('names the star in German', async () => {
    await i18n.changeLanguage('de');
    const { card } = renderCard({ lesson: { ...ballet, bookedFor: booked }, name: german });

    expect(card.getByRole('img', { name: 'Gebucht' })).toBeInTheDocument();
  });
});
