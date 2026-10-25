import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { DuplicatePair, MergeSummary } from '@/api/queries';
import { Toaster } from '@/components/ui/sonner';
import { type MockRequest, mockApiGet, mockApiPost, problem, publicSettings } from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { DuplicatesPage } from './DuplicatesPage';

afterEach(() => {
  vi.restoreAllMocks();
});

const lena: DuplicatePair = {
  a: {
    id: '0190a000-0000-7000-8000-000000000601',
    givenName: 'Lena',
    familyName: 'Quillfeather',
    email: 'lena.quillfeather@example.com',
    phone: '+49 000 0000201',
    memberNo: 'M-1042',
  },
  b: {
    id: '0190a000-0000-7000-8000-000000000602',
    givenName: 'Lena',
    familyName: 'Quillfeather',
    email: 'LENA.Quillfeather@example.com',
    birthDate: '1994-08-17',
    notes: 'Registered at the front desk.',
    accountEmail: 'lena@example.com',
  },
  strength: 'strong',
  reasons: ['email', 'name'],
};

const jonas: DuplicatePair = {
  a: { id: '0190a000-0000-7000-8000-000000000603', givenName: 'Jonas', familyName: 'Brightwater' },
  b: {
    id: '0190a000-0000-7000-8000-000000000604',
    givenName: 'Jonas',
    familyName: 'Brightwater',
    birthDate: '2014-05-12',
  },
  strength: 'weak',
  reasons: ['name'],
};

const summary = (applied: boolean): MergeSummary => ({
  applied,
  survivor: { ...lena.b, givenName: 'Lena', familyName: 'Quillfeather', memberNo: 'M-1042' },
  bookingsMoved: 2,
  canceled: [
    {
      lessonId: '0190a000-0000-7000-8000-000000000605',
      startsAt: '2026-10-26T16:00:00Z',
      courseTypeName: 'Classical Ballet',
      reason: 'booked',
    },
  ],
  history: 1,
  waitlistMoved: 0,
  waitlistWithdrawn: 0,
  membershipsMoved: 1,
  membershipsDropped: [],
  passesMoved: 1,
  paidPassesMoved: 0,
  linksMoved: 0,
  linksDropped: 0,
  staffMoved: 0,
  accountMoved: false,
  receipts: 1,
});

describe('DuplicatesPage', () => {
  it('lists the pairs, merges one after its preview, and dismisses another', async () => {
    let pairs = [lena, jonas];
    mockApiGet({
      '/api/v1/admin/duplicates': () => ({ body: { items: pairs } }),
      '/api/v1/settings/public': { body: publicSettings },
    });
    const merges: MockRequest[] = [];
    const dismissals: MockRequest[] = [];
    mockApiPost({
      '/api/v1/admin/persons/merge': (init) => {
        merges.push(init);
        const body = init.body as { apply?: boolean };
        if (body.apply) pairs = pairs.filter((p) => p !== lena);
        return { body: summary(body.apply === true) };
      },
      '/api/v1/admin/duplicates/dismissals': (init) => {
        dismissals.push(init);
        pairs = pairs.filter((p) => p !== jonas);
        return { status: 204 };
      },
    });
    const user = userEvent.setup();

    render(<Toaster />);
    renderInRouter(<DuplicatesPage />, { at: '/admin/persons' });

    const list = within(await screen.findByRole('list', { name: 'Possible duplicates' }));
    const first = within(
      list.getByRole('listitem', { name: 'Lena Quillfeather / Lena Quillfeather' }),
    );
    expect(first.getByText('Likely')).toBeInTheDocument();
    expect(first.getByText('same email · same name')).toBeInTheDocument();
    expect(first.getByText('M-1042')).toBeInTheDocument();
    expect(first.getByText('lena@example.com')).toBeInTheDocument();
    expect(list.getByText('Possible')).toBeInTheDocument();

    await user.click(
      first.getByRole('button', { name: 'Merge Lena Quillfeather / Lena Quillfeather' }),
    );

    // The person with the account stays by default; the other's member
    // number fills the survivor's empty one.
    const dialog = within(await screen.findByRole('dialog', { name: 'Merge two persons' }));
    expect(
      dialog.getByRole('radio', { name: /Keep Lena Quillfeather\s*Has the account/ }),
    ).toBeChecked();
    expect(await dialog.findByText('2 bookings move')).toBeInTheDocument();
    expect(
      dialog.getByText(
        '1 booking of a lesson that has started, which both have, is kept as history',
      ),
    ).toBeInTheDocument();
    expect(
      dialog.getByText(
        'Booking of Classical Ballet on Oct 26, 2026, 17:00 is canceled (a visit it cost comes back)',
      ),
    ).toBeInTheDocument();
    expect(merges[0]?.body).toEqual({
      survivorId: lena.b.id,
      mergedId: lena.a.id,
      take: { name: false, email: false, phone: true, birthDate: false, memberNo: true },
      notes: 'Registered at the front desk.',
      apply: false,
    });

    // The email of the duplicate is chosen instead.
    await user.click(dialog.getByRole('radio', { name: 'lena.quillfeather@example.com' }));
    expect(merges.at(-1)?.body).toMatchObject({ take: { email: true } });

    await user.click(dialog.getByRole('button', { name: 'Merge' }));

    expect(merges.find((m) => (m.body as { apply?: boolean }).apply)?.body).toMatchObject({
      take: { email: true },
    });
    expect(
      await screen.findByText('Lena Quillfeather was merged into Lena Quillfeather.'),
    ).toBeInTheDocument();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();

    await user.click(
      await screen.findByRole('button', {
        name: 'Jonas Brightwater / Jonas Brightwater are not the same',
      }),
    );
    expect(dismissals[0]?.body).toEqual({ personA: jonas.a.id, personB: jonas.b.id });
    expect(await screen.findByText('No possible duplicates.')).toBeInTheDocument();
  });

  it('lets the office shorten notes that are too long together', async () => {
    const long: DuplicatePair = {
      ...lena,
      a: { ...lena.a, notes: 'Imported from the old system.' },
    };
    mockApiGet({
      '/api/v1/admin/duplicates': { body: { items: [long] } },
      '/api/v1/settings/public': { body: publicSettings },
    });
    const merges: MockRequest[] = [];
    mockApiPost({
      '/api/v1/admin/persons/merge': (init) => {
        merges.push(init);
        const body = init.body as { notes?: string; apply?: boolean };
        // Pretend the limit is the length of one of the notes.
        if ((body.notes ?? '').length > 'Registered at the front desk.'.length) {
          return problem(422, 'validation failed', [{ location: 'body.notes' }]);
        }
        return { body: summary(body.apply === true) };
      },
    });
    const user = userEvent.setup();

    renderInRouter(<DuplicatesPage />, { at: '/admin/persons' });
    await user.click(
      await screen.findByRole('button', { name: 'Merge Lena Quillfeather / Lena Quillfeather' }),
    );

    // The notes of both, the survivor's first; too long for the server.
    const dialog = within(await screen.findByRole('dialog', { name: 'Merge two persons' }));
    const notes = dialog.getByRole('textbox', { name: 'Notes of the person that stays' });
    expect(notes).toHaveValue('Registered at the front desk.\n\nImported from the old system.');
    expect(
      await dialog.findByText(
        'The notes are too long: at most 2000 characters. Shorten them to merge.',
      ),
    ).toBeInTheDocument();
    expect(notes).toHaveAttribute('aria-invalid', 'true');
    expect(dialog.getByRole('button', { name: 'Merge' })).toBeDisabled();

    // Shortened, the preview comes, and the merge carries the edited notes.
    await user.clear(notes);
    await user.type(notes, 'Both: see the files.');
    await waitFor(() =>
      expect(merges.at(-1)?.body).toMatchObject({ notes: 'Both: see the files.', apply: false }),
    );
    expect(await dialog.findByText('2 bookings move')).toBeInTheDocument();
    await waitFor(() => expect(dialog.getByRole('button', { name: 'Merge' })).toBeEnabled());
    expect(notes).not.toHaveAttribute('aria-invalid');

    await user.click(dialog.getByRole('button', { name: 'Merge' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    expect(merges.find((m) => (m.body as { apply?: boolean }).apply)?.body).toMatchObject({
      notes: 'Both: see the files.',
    });
  });

  it('explains why two persons with accounts cannot be merged', async () => {
    const both: DuplicatePair = { ...lena, a: { ...lena.a, accountEmail: 'lena2@example.com' } };
    mockApiGet({
      '/api/v1/admin/duplicates': { body: { items: [both] } },
      '/api/v1/settings/public': { body: publicSettings },
    });
    mockApiPost({
      '/api/v1/admin/persons/merge': {
        ...problem(409, 'both persons have an account'),
        body: { ...problem(409, 'x').body, reason: 'two-accounts' },
      },
    });
    const user = userEvent.setup();

    renderInRouter(<DuplicatesPage />, { at: '/admin/persons' });
    await user.click(
      await screen.findByRole('button', { name: 'Merge Lena Quillfeather / Lena Quillfeather' }),
    );

    expect(
      await screen.findByText(
        /^Both persons have an account\. The account of the person that goes/,
      ),
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Merge' })).toBeDisabled();
  });
});
