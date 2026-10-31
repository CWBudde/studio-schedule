import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import type { LessonParticipant, LessonWaitlistEntry } from '@/api/queries';
import { formatDateTime } from '@/lib/schoolTime';
import type { ParticipantsReadyProps } from '../slots';
import { Participants } from './Participants';

const id = (n: number) => `0190a000-0000-7000-8000-${String(n).padStart(12, '0')}`;

const booking = (
  n: number,
  name: string,
  extra: Partial<LessonParticipant> = {},
): LessonParticipant => ({
  bookingId: id(700 + n),
  personId: id(100 + n),
  name,
  guest: false,
  status: 'booked',
  source: 'self',
  trial: false,
  late: false,
  bookedAt: '2026-10-20T10:00:00Z',
  payment: { status: 'pending' },
  ...extra,
});

// Fictional participants of a lesson.
const mia = booking(1, 'Mia Sample', {
  contactEmail: 'mia.sample@example.com',
  contactPhone: '+49 (0) 30 123-45',
  payment: {
    status: 'paid',
    pass: { id: id(901), kind: 'visits', name: '10 visits', visitsRemaining: 4 },
  },
});
const noah = booking(2, 'Noah Example', {
  status: 'attended',
  trial: true,
  trialNote: 'First time on pointe',
  payment: { status: 'free' },
});
const lina = booking(3, 'Lina Guest', { guest: true, status: 'no_show' });
const ben = booking(4, 'Ben Canceled', {
  status: 'canceled',
  late: true,
  canceledAt: '2026-10-25T09:30:00Z',
  contactEmail: 'ben@example.com',
});
const waiting: LessonWaitlistEntry = {
  entryId: id(801),
  personId: id(201),
  name: 'Wanda Wait',
  status: 'waiting',
  position: 1,
  joinedAt: '2026-10-21T10:00:00Z',
  stayOnList: false,
};
const withdrawn: LessonWaitlistEntry = {
  entryId: id(802),
  personId: id(202),
  name: 'Walt Gone',
  status: 'withdrawn',
  joinedAt: '2026-10-21T11:00:00Z',
  stayOnList: false,
};

const timeZone = 'Europe/Berlin';

function readyProps(over: Partial<ParticipantsReadyProps> = {}): ParticipantsReadyProps {
  return {
    state: 'ready',
    headingId: 'participants-heading',
    counts: { active: 3, capacity: 10, freePlaces: 7, held: 0, waiting: 0 },
    stale: false,
    onRetry: vi.fn(),
    timeZone,
    canEdit: true,
    canAdd: true,
    canAttend: true,
    office: true,
    active: [mia, noah, lina],
    canceled: [],
    waitlist: [],
    onAdd: vi.fn(),
    allAttended: { show: true, pending: false, run: vi.fn() },
    mark: vi.fn(),
    marking: () => false,
    open: vi.fn(),
    dialogs: <div role="dialog" aria-label="The dialogs" />,
    ...over,
  };
}

const bookedTable = () => screen.getByRole('table', { name: 'Booked participants' });
const rowOf = (name: string) => {
  const row = within(bookedTable()).getByText(name).closest('tr');
  if (!row) throw new Error(`no row for ${name}`);
  return within(row);
};

describe('Werkstatt Participants', () => {
  it('shows the heading, the counts and the column headers', () => {
    render(<Participants {...readyProps()} />);

    expect(screen.getByRole('region', { name: 'Participants' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { level: 3, name: 'Participants' })).toBeInTheDocument();
    expect(screen.getByText('3 of 10 booked · 7 places free')).toBeInTheDocument();
    const headers = within(bookedTable())
      .getAllByRole('columnheader')
      .map((h) => h.textContent);
    expect(headers).toEqual([
      'Present',
      'Name',
      'Email',
      'Phone',
      'Pass',
      'Trial lesson',
      'Actions',
    ]);
  });

  it('shows a row per booking with its contacts, pass, markers and trial note', () => {
    render(<Participants {...readyProps()} />);

    expect(within(bookedTable()).getAllByRole('row')).toHaveLength(4);
    const miaRow = rowOf('Mia Sample');
    expect(miaRow.getByRole('link', { name: 'mia.sample@example.com' })).toHaveAttribute(
      'href',
      'mailto:mia.sample@example.com',
    );
    expect(miaRow.getByRole('link', { name: '+49 (0) 30 123-45' })).toHaveAttribute(
      'href',
      'tel:+4903012345',
    );
    expect(miaRow.getByText('Paid · 10 visits, 4 left')).toBeInTheDocument();
    expect(miaRow.queryByText('Trial')).not.toBeInTheDocument();

    const noahRow = rowOf('Noah Example');
    expect(noahRow.getByText('Trial')).toBeInTheDocument();
    expect(noahRow.getByText('Note: First time on pointe')).toBeInTheDocument();
    expect(noahRow.getByText('Free')).toBeInTheDocument();
    expect(noahRow.queryAllByRole('link')).toHaveLength(0);

    expect(rowOf('Lina Guest').getByText('Guest')).toBeInTheDocument();
  });

  it('shows "Deleted person" for an anonymized participant', () => {
    render(
      <Participants
        {...readyProps({ active: [booking(5, 'Deleted person', { anonymized: true })] })}
      />,
    );
    expect(within(bookedTable()).getByText('Deleted person')).toBeInTheDocument();
  });

  it('checks the present box of an attended booking, and checking or unchecking marks', async () => {
    const user = userEvent.setup();
    const props = readyProps();
    render(<Participants {...props} />);

    const miaBox = screen.getByRole('checkbox', { name: 'Mia Sample attended' });
    const noahBox = screen.getByRole('checkbox', { name: 'Noah Example attended' });
    expect(miaBox).not.toBeChecked();
    expect(noahBox).toBeChecked();
    expect(screen.getByRole('checkbox', { name: 'Lina Guest attended' })).not.toBeChecked();

    await user.click(miaBox);
    expect(props.mark).toHaveBeenLastCalledWith(mia, 'attended');
    await user.click(noahBox);
    expect(props.mark).toHaveBeenLastCalledWith(noah, 'booked');
    expect(props.mark).toHaveBeenCalledTimes(2);
  });

  it('marks a no-show, shows it, and resets a recorded attendance', async () => {
    const user = userEvent.setup();
    const props = readyProps();
    render(<Participants {...props} />);

    const miaNoShow = screen.getByRole('button', { name: 'Mia Sample did not come' });
    expect(miaNoShow).toHaveAttribute('aria-pressed', 'false');
    expect(screen.queryByRole('button', { name: 'Reset the attendance of Mia Sample' })).toBeNull();
    await user.click(miaNoShow);
    expect(props.mark).toHaveBeenLastCalledWith(mia, 'no_show');

    const linaNoShow = screen.getByRole('button', { name: 'Lina Guest did not come' });
    expect(linaNoShow).toHaveAttribute('aria-pressed', 'true');
    expect(rowOf('Lina Guest').getByText('No-show')).toBeInTheDocument();
    // Pressing it again records nothing.
    await user.click(linaNoShow);
    expect(props.mark).toHaveBeenCalledTimes(1);

    await user.click(screen.getByRole('button', { name: 'Reset the attendance of Lina Guest' }));
    expect(props.mark).toHaveBeenLastCalledWith(lina, 'booked');
    await user.click(screen.getByRole('button', { name: 'Reset the attendance of Noah Example' }));
    expect(props.mark).toHaveBeenLastCalledWith(noah, 'booked');
  });

  it('disables only the attendance controls of a booking being recorded', () => {
    render(<Participants {...readyProps({ marking: (b) => b === mia.bookingId })} />);

    expect(screen.getByRole('checkbox', { name: 'Mia Sample attended' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Mia Sample did not come' })).toBeDisabled();
    expect(screen.getByRole('checkbox', { name: 'Noah Example attended' })).toBeEnabled();
    expect(screen.getByRole('button', { name: 'Noah Example did not come' })).toBeEnabled();
  });

  it('opens the trial, remove and history dialogs of a participant', async () => {
    const user = userEvent.setup();
    const props = readyProps();
    render(<Participants {...props} />);

    await user.click(screen.getByRole('button', { name: 'Trial lesson of Mia Sample' }));
    expect(props.open).toHaveBeenLastCalledWith(mia, 'trial');
    await user.click(screen.getByRole('button', { name: 'Remove Noah Example' }));
    expect(props.open).toHaveBeenLastCalledWith(noah, 'remove');
    await user.click(screen.getByRole('button', { name: 'Booking history of Lina Guest' }));
    expect(props.open).toHaveBeenLastCalledWith(lina, 'history');
    expect(screen.getByRole('dialog', { name: 'The dialogs' })).toBeInTheDocument();
  });

  it('adds a participant and marks all attended from below the table', async () => {
    const user = userEvent.setup();
    const props = readyProps();
    render(<Participants {...props} />);

    const add = screen.getByRole('button', { name: 'Add participant' });
    // Below the table, not above it.
    expect(bookedTable().compareDocumentPosition(add)).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
    await user.click(add);
    expect(props.onAdd).toHaveBeenCalledOnce();
    await user.click(screen.getByRole('button', { name: 'All attended' }));
    expect(props.allAttended.run).toHaveBeenCalledOnce();
  });

  it('disables "All attended" while it runs, and hides it and adding where they do not apply', () => {
    const { rerender } = render(
      <Participants
        {...readyProps({ allAttended: { show: true, pending: true, run: vi.fn() } })}
      />,
    );
    expect(screen.getByRole('button', { name: 'All attended' })).toBeDisabled();

    rerender(
      <Participants
        {...readyProps({
          canAdd: false,
          allAttended: { show: false, pending: false, run: vi.fn() },
        })}
      />,
    );
    expect(screen.queryByRole('button', { name: 'Add participant' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'All attended' })).toBeNull();
  });

  it('without editing, hides trial edit and remove and says the day is over', () => {
    render(<Participants {...readyProps({ canEdit: false })} />);

    expect(
      screen.getByText('The day of this lesson is over: the office makes changes now.'),
    ).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /^Trial lesson of / })).toBeNull();
    expect(screen.queryByRole('button', { name: /^Remove / })).toBeNull();
    // The trial marker itself stays.
    expect(rowOf('Noah Example').getByText('Trial')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Booking history of Mia Sample' })).toBeEnabled();
  });

  it('without attendance, shows the status as text instead of the controls', () => {
    render(<Participants {...readyProps({ canAttend: false })} />);

    expect(screen.queryAllByRole('checkbox')).toHaveLength(0);
    expect(screen.queryByRole('button', { name: /did not come$/ })).toBeNull();
    expect(screen.queryByRole('button', { name: /^Reset the attendance/ })).toBeNull();
    expect(within(bookedTable()).getAllByRole('columnheader')[0]).toHaveTextContent('Status');
    expect(rowOf('Mia Sample').getByText('Booked')).toBeInTheDocument();
    expect(rowOf('Noah Example').getByText('Attended')).toBeInTheDocument();
    expect(rowOf('Lina Guest').getByText('No-show')).toBeInTheDocument();
  });

  it('shows the history only to the office, and no actions column without any action', () => {
    const { rerender } = render(<Participants {...readyProps({ office: false })} />);
    expect(screen.queryByRole('button', { name: /^Booking history of / })).toBeNull();
    expect(screen.getByRole('button', { name: 'Remove Mia Sample' })).toBeInTheDocument();

    rerender(<Participants {...readyProps({ office: false, canEdit: false })} />);
    expect(
      within(bookedTable())
        .getAllByRole('columnheader')
        .map((h) => h.textContent),
    ).not.toContain('Actions');
  });

  it('says nobody is booked without bookings', () => {
    render(<Participants {...readyProps({ active: [] })} />);

    expect(screen.getByText('Nobody is booked.')).toBeInTheDocument();
    expect(screen.queryByRole('table')).toBeNull();
    expect(screen.getByRole('button', { name: 'Add participant' })).toBeInTheDocument();
  });

  it('shows the held places and the waiting count', () => {
    render(
      <Participants
        {...readyProps({ counts: { active: 9, capacity: 10, freePlaces: 0, held: 1, waiting: 2 } })}
      />,
    );
    expect(
      screen.getByText('9 of 10 booked · 0 places free · 1 place held by an invite · 2 waiting'),
    ).toBeInTheDocument();
  });

  it('lists the canceled bookings with when they were canceled, and their history', async () => {
    const user = userEvent.setup();
    const props = readyProps({ canceled: [ben] });
    render(<Participants {...props} />);

    const table = within(screen.getByRole('table', { name: 'Canceled bookings' }));
    expect(
      screen.getByRole('heading', { level: 4, name: 'Canceled bookings' }),
    ).toBeInTheDocument();
    expect(table.getByText('Ben Canceled')).toBeInTheDocument();
    expect(table.getByText('Late')).toBeInTheDocument();
    expect(
      table.getByText(formatDateTime('2026-10-25T09:30:00Z', timeZone, 'en')),
    ).toBeInTheDocument();
    expect(table.getByRole('link', { name: 'ben@example.com' })).toHaveAttribute(
      'href',
      'mailto:ben@example.com',
    );
    expect(table.queryByRole('checkbox')).toBeNull();
    expect(table.queryByRole('button', { name: 'Remove Ben Canceled' })).toBeNull();
    await user.click(table.getByRole('button', { name: 'Booking history of Ben Canceled' }));
    expect(props.open).toHaveBeenLastCalledWith(ben, 'history');
  });

  it('lists the waiting list with each entry’s place or outcome', () => {
    render(<Participants {...readyProps({ waitlist: [waiting, withdrawn] })} />);

    expect(screen.getByRole('heading', { level: 4, name: 'Waiting list' })).toBeInTheDocument();
    const list = within(screen.getByRole('list', { name: 'Waiting list' }));
    const items = list.getAllByRole('listitem');
    expect(items).toHaveLength(2);
    expect(items[0]).toHaveTextContent('Wanda Wait');
    expect(items[0]).toHaveTextContent('Position 1');
    expect(items[1]).toHaveTextContent('Walt Gone');
    expect(items[1]).toHaveTextContent('Withdrawn');
  });

  it('offers a retry while the lists may be out of date', async () => {
    const user = userEvent.setup();
    const props = readyProps({ stale: true });
    render(<Participants {...props} />);

    const status = within(screen.getByRole('status'));
    expect(
      status.getByText('The participants could not be updated; this may not be the latest state.'),
    ).toBeInTheDocument();
    await user.click(status.getByRole('button', { name: 'Try again' }));
    expect(props.onRetry).toHaveBeenCalledOnce();
  });

  it('shows the error of a failed first load with a retry', async () => {
    const user = userEvent.setup();
    const onRetry = vi.fn();
    render(<Participants state="error" headingId="participants-heading" onRetry={onRetry} />);

    expect(screen.getByRole('region', { name: 'Participants' })).toBeInTheDocument();
    expect(screen.getByText('The participants could not be loaded.')).toBeInTheDocument();
    expect(screen.queryByRole('table')).toBeNull();
    await user.click(screen.getByRole('button', { name: 'Try again' }));
    expect(onRetry).toHaveBeenCalledOnce();
  });
});
