import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Toaster } from '@/components/ui/sonner';
import i18n from '@/i18n';
import {
  autumnClassEntries,
  catalogFixtures,
  conflict,
  ids,
  meOffice,
  mockApiDelete,
  mockApiGet,
  mockApiPost,
  mockApiPut,
  type MockRequest,
  problem,
  staffEntries,
  terms,
} from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { ClassesPage } from './ClassesPage';

afterEach(() => {
  vi.restoreAllMocks();
});

const asOffice = {
  ...catalogFixtures,
  '/api/v1/auth/me': { body: meOffice },
  '/api/v1/terms': { body: { items: terms } },
  '/api/v1/admin/staff': { body: { items: staffEntries } },
  '/api/v1/admin/terms/{termId}/classes': (init: MockRequest) => ({
    body: { items: init.params?.path?.termId === ids.termAutumn ? autumnClassEntries : [] },
  }),
};

function renderPage(termId: string = ids.termAutumn) {
  render(<Toaster />);
  return renderInRouter(<ClassesPage termId={termId} />, { at: '/admin/terms/$termId/classes' });
}

/** The card of a class, named "<course type>, <weekday>, <hh:mm–hh:mm>". */
const card = async (name: string) => within(await screen.findByRole('article', { name }));

/** Picks `option` in the select `label` of `scope`. */
async function choose(
  user: ReturnType<typeof userEvent.setup>,
  scope: ReturnType<typeof within>,
  label: string,
  option: string,
) {
  await user.click(scope.getByRole('combobox', { name: label }));
  await user.click(await screen.findByRole('option', { name: option }));
}

/** A ballet class on Wednesday 09:30 as the API answers a create or update. */
const answer = ({ body }: MockRequest) => ({
  status: 201,
  body: {
    id: 'class-new',
    termId: ids.termAutumn,
    instructor: { id: ids.staffAlex, displayName: 'Alex Example' },
    ...(body as object),
  },
});

describe('ClassesPage', () => {
  it('shows the classes of the term by weekday, retired ones marked', async () => {
    mockApiGet(asOffice);

    renderPage();

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Classes – Autumn 2026' }),
    ).toBeInTheDocument();
    expect(screen.getByText('Aug 31, 2026 – Jan 29, 2027')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'All terms' })).toHaveAttribute('href', '/admin/terms');

    const days = screen.getAllByRole('heading', { level: 2 }).map((h) => h.textContent);
    expect(days).toEqual(['Monday', 'Tuesday', 'Saturday', 'Sunday']);

    const ballet = await card('Ballet, Monday, 18:00–19:30');
    expect(ballet.getByText('Room A · North Studio')).toBeInTheDocument();
    expect(ballet.getByRole('button', { name: 'Edit Ballet, Monday 18:00' })).toBeInTheDocument();
    expect(ballet.getByRole('button', { name: 'Delete Ballet, Monday 18:00' })).toBeInTheDocument();
    expect(ballet.queryByText('Retired')).not.toBeInTheDocument();
    expect(ballet.queryByRole('button', { name: /^Reactivate/ })).not.toBeInTheDocument();

    const retired = await card('Jazz, Saturday, 10:00–11:00');
    expect(retired.getByText('Retired')).toBeInTheDocument();
    expect(
      retired.getByRole('button', { name: 'Reactivate Jazz, Saturday 10:00' }),
    ).toBeInTheDocument();
  });

  it('adds a class with the capacity of its room', async () => {
    mockApiGet(asOffice);
    const post = mockApiPost({ '/api/v1/terms/{termId}/classes': answer });
    const user = userEvent.setup();

    renderPage();
    await user.click(await screen.findByRole('button', { name: 'Add class' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Add a class' }));
    await choose(user, sheet, 'Course type', 'Ballet');
    await choose(user, sheet, 'Room', 'North Studio · Room A');
    expect(sheet.getByLabelText('Capacity')).toHaveValue(16);
    await choose(user, sheet, 'Instructor', 'Alex Example');
    // Former staff are not offered for a new class.
    await user.click(sheet.getByRole('combobox', { name: 'Accompanist' }));
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual([
      'None',
      'Sam Pianist',
    ]);
    await user.click(screen.getByRole('option', { name: 'Sam Pianist' }));
    await choose(user, sheet, 'Level', 'Advanced');
    await choose(user, sheet, 'Weekday', 'Wednesday');
    await user.type(sheet.getByLabelText('Start time'), '09:30');
    await user.type(sheet.getByLabelText('Duration (minutes)'), '75');
    await user.click(sheet.getByRole('button', { name: 'Add class' }));

    expect(await screen.findByText('Ballet, Wednesday 09:30 has been added.')).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/terms/{termId}/classes', {
      params: { path: { termId: ids.termAutumn } },
      body: {
        courseTypeId: ids.ballet,
        roomId: ids.roomA,
        instructorId: ids.staffAlex,
        accompanistId: ids.staffSam,
        level: 'advanced',
        weekday: 3,
        startTime: '09:30',
        durationMinutes: 75,
        capacity: 16,
        retired: false,
      },
    });
  });

  it('fills in the capacity of the room until the capacity is changed', async () => {
    mockApiGet(asOffice);
    const user = userEvent.setup();

    renderPage();
    await user.click(await screen.findByRole('button', { name: 'Add class' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Add a class' }));
    const capacity = sheet.getByLabelText('Capacity');
    expect(capacity).toHaveValue(null);

    await choose(user, sheet, 'Room', 'North Studio · Room A');
    expect(capacity).toHaveValue(16);
    await choose(user, sheet, 'Room', 'South Studio · Room B');
    expect(capacity).toHaveValue(12);

    await user.clear(capacity);
    await user.type(capacity, '8');
    await choose(user, sheet, 'Room', 'North Studio · Room A');
    expect(capacity).toHaveValue(8);
  });

  it('checks the entries before sending them', async () => {
    mockApiGet(asOffice);
    const post = mockApiPost({});
    const user = userEvent.setup();

    renderPage();
    await user.click(await screen.findByRole('button', { name: 'Add class' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Add a class' }));
    await user.click(sheet.getByRole('button', { name: 'Add class' }));

    expect(sheet.getByRole('combobox', { name: 'Course type' })).toHaveAccessibleDescription(
      'Please choose one.',
    );
    expect(sheet.getByLabelText('Start time')).toHaveAccessibleDescription(
      /Enter a time such as 18:00\.$/,
    );
    expect(sheet.getByLabelText('Duration (minutes)')).toHaveAccessibleDescription(
      'Enter a whole number of minutes from 1 to 1440.',
    );

    expect(post).not.toHaveBeenCalled();
  });

  it('checks that the class ends by midnight', async () => {
    mockApiGet(asOffice);
    const post = mockApiPost({});
    const user = userEvent.setup();

    renderPage();
    await user.click(await screen.findByRole('button', { name: 'Add class' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Add a class' }));
    await choose(user, sheet, 'Course type', 'Jazz');
    await choose(user, sheet, 'Room', 'South Studio · Room B');
    await choose(user, sheet, 'Instructor', 'Riley Sample');
    await choose(user, sheet, 'Level', 'All levels');
    await choose(user, sheet, 'Weekday', 'Friday');
    await user.type(sheet.getByLabelText('Start time'), '23:30');
    await user.type(sheet.getByLabelText('Duration (minutes)'), '45');
    await user.click(sheet.getByRole('button', { name: 'Add class' }));
    expect(sheet.getByLabelText('Duration (minutes)')).toHaveAccessibleDescription(
      'The class must end by midnight.',
    );
    expect(post).not.toHaveBeenCalled();
  });

  it('edits a class and marks the capacity the API refuses', async () => {
    mockApiGet(asOffice);
    let refuse = true;
    const put = mockApiPut({
      '/api/v1/classes/{classId}': (init) =>
        refuse
          ? problem(422, 'capacity below the bookings of a future lesson', [
              { location: 'body.capacity' },
            ])
          : { body: { ...answer(init).body, id: ids.classBallet } },
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(
      (await card('Ballet, Monday, 18:00–19:30')).getByRole('button', {
        name: 'Edit Ballet, Monday 18:00',
      }),
    );
    const sheet = within(await screen.findByRole('dialog', { name: 'Edit Ballet, Monday 18:00' }));
    expect(sheet.getByRole('combobox', { name: 'Course type' })).toHaveTextContent('Ballet');
    expect(sheet.getByRole('combobox', { name: 'Room' })).toHaveTextContent(
      'North Studio · Room A',
    );
    expect(sheet.getByRole('combobox', { name: 'Instructor' })).toHaveTextContent('Alex Example');
    expect(sheet.getByRole('combobox', { name: 'Accompanist' })).toHaveTextContent('Sam Pianist');
    expect(sheet.getByRole('combobox', { name: 'Level' })).toHaveTextContent('Beginner');
    expect(sheet.getByRole('combobox', { name: 'Weekday' })).toHaveTextContent('Monday');
    expect(sheet.getByLabelText('Start time')).toHaveValue('18:00');
    expect(sheet.getByLabelText('Duration (minutes)')).toHaveValue(90);
    expect(sheet.getByLabelText('Capacity')).toHaveValue(12);

    // A room keeps the capacity of a class that exists.
    await choose(user, sheet, 'Room', 'South Studio · Room B');
    expect(sheet.getByLabelText('Capacity')).toHaveValue(12);
    await user.clear(sheet.getByLabelText('Start time'));
    await user.type(sheet.getByLabelText('Start time'), '18:15');
    await user.clear(sheet.getByLabelText('Capacity'));
    await user.type(sheet.getByLabelText('Capacity'), '2');
    await user.click(sheet.getByRole('button', { name: 'Save changes' }));

    expect(await sheet.findByRole('alert')).toHaveTextContent('Please check your entries.');
    expect(sheet.getByLabelText('Capacity')).toHaveAccessibleDescription(
      /The class has more members, or a coming lesson more bookings, than that\.$/,
    );
    expect(put).toHaveBeenCalledWith('/api/v1/classes/{classId}', {
      params: { path: { classId: ids.classBallet } },
      body: {
        courseTypeId: ids.ballet,
        roomId: ids.roomB,
        instructorId: ids.staffAlex,
        accompanistId: ids.staffSam,
        level: 'beginner',
        weekday: 1,
        startTime: '18:15',
        durationMinutes: 90,
        capacity: 2,
        retired: false,
      },
    });

    refuse = false;
    await user.clear(sheet.getByLabelText('Capacity'));
    await user.type(sheet.getByLabelText('Capacity'), '14');
    await user.click(sheet.getByRole('button', { name: 'Save changes' }));
    expect(await screen.findByText('Ballet, Monday 18:15 has been saved.')).toBeInTheDocument();
  });

  it('offers a former instructor only for the class they teach', async () => {
    mockApiGet(asOffice);
    const put = mockApiPut({ '/api/v1/classes/{classId}': answer });
    const user = userEvent.setup();

    renderPage();
    await user.click(
      (await card('Jazz, Saturday, 10:00–11:00')).getByRole('button', {
        name: 'Edit Jazz, Saturday 10:00',
      }),
    );
    const sheet = within(await screen.findByRole('dialog', { name: 'Edit Jazz, Saturday 10:00' }));
    const instructor = sheet.getByRole('combobox', { name: 'Instructor' });
    expect(instructor).toHaveTextContent('Quinn Former (inactive)');
    expect(sheet.getByRole('combobox', { name: 'Accompanist' })).toHaveTextContent('None');
    await user.click(sheet.getByRole('button', { name: 'Save changes' }));

    // A retired class stays retired.
    await waitFor(() =>
      expect(put).toHaveBeenCalledWith('/api/v1/classes/{classId}', {
        params: { path: { classId: ids.classRetired } },
        body: expect.objectContaining({ instructorId: ids.staffQuinn, retired: true }),
      }),
    );
    expect((put.mock.calls[0]?.[1] as MockRequest | undefined)?.body).not.toHaveProperty(
      'accompanistId',
    );
  });

  it('deletes a class without history', async () => {
    mockApiGet(asOffice);
    const remove = mockApiDelete({ '/api/v1/classes/{classId}': { status: 204 } });
    const user = userEvent.setup();

    renderPage();
    await user.click(
      (await card('Contemporary, Tuesday, 17:00–18:00')).getByRole('button', {
        name: 'Delete Contemporary, Tuesday 17:00',
      }),
    );
    const dialog = within(
      await screen.findByRole('alertdialog', { name: 'Delete Contemporary, Tuesday 17:00?' }),
    );
    expect(
      screen.getByRole('alertdialog', { name: 'Delete Contemporary, Tuesday 17:00?' }),
    ).toHaveAccessibleDescription(/is retired instead/);
    await user.click(dialog.getByRole('button', { name: 'Delete' }));

    expect(
      await screen.findByText('Contemporary, Tuesday 17:00 has been deleted.'),
    ).toBeInTheDocument();
    expect(remove).toHaveBeenCalledWith('/api/v1/classes/{classId}', {
      params: { path: { classId: ids.classContemporary } },
    });
  });

  it('retires a class with history instead of deleting it', async () => {
    mockApiGet(asOffice);
    mockApiDelete({ '/api/v1/classes/{classId}': conflict('in-use') });
    const put = mockApiPut({ '/api/v1/classes/{classId}': answer });
    const user = userEvent.setup();

    renderPage();
    await user.click(
      (await card('Contemporary, Tuesday, 17:00–18:00')).getByRole('button', {
        name: 'Delete Contemporary, Tuesday 17:00',
      }),
    );
    const dialog = within(
      await screen.findByRole('alertdialog', { name: 'Delete Contemporary, Tuesday 17:00?' }),
    );
    await user.click(dialog.getByRole('button', { name: 'Delete' }));
    expect(await dialog.findByRole('alert')).toHaveTextContent(
      'Contemporary, Tuesday 17:00 has members, bookings or lessons that took place, so it cannot be deleted. Retire it instead.',
    );
    await user.click(dialog.getByRole('button', { name: 'Retire instead' }));

    expect(
      await screen.findByText('Contemporary, Tuesday 17:00 has been retired.'),
    ).toBeInTheDocument();
    expect(put).toHaveBeenCalledWith('/api/v1/classes/{classId}', {
      params: { path: { classId: ids.classContemporary } },
      body: {
        courseTypeId: ids.contemporary,
        roomId: ids.roomB,
        instructorId: ids.staffRiley,
        level: 'intermediate',
        weekday: 2,
        startTime: '17:00',
        durationMinutes: 60,
        capacity: 10,
        retired: true,
      },
    });
  });

  it('reactivates a retired class', async () => {
    mockApiGet(asOffice);
    const put = mockApiPut({ '/api/v1/classes/{classId}': answer });
    const user = userEvent.setup();

    renderPage();
    await user.click(
      (await card('Jazz, Saturday, 10:00–11:00')).getByRole('button', {
        name: 'Reactivate Jazz, Saturday 10:00',
      }),
    );

    expect(
      await screen.findByText('Jazz, Saturday 10:00 has been reactivated.'),
    ).toBeInTheDocument();
    expect(put).toHaveBeenCalledWith('/api/v1/classes/{classId}', {
      params: { path: { classId: ids.classRetired } },
      body: {
        courseTypeId: ids.jazz,
        roomId: ids.roomA,
        instructorId: ids.staffQuinn,
        level: 'beginner',
        weekday: 6,
        startTime: '10:00',
        durationMinutes: 60,
        capacity: 16,
        retired: false,
      },
    });
  });

  it('says when a term has no classes yet', async () => {
    mockApiGet(asOffice);

    renderPage(ids.termSpring);

    expect(await screen.findByText('No classes in this term')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Add class' })).toBeInTheDocument();
  });

  it('says when the term does not exist', async () => {
    mockApiGet(asOffice);

    renderPage('0190a000-0000-7000-8000-000000000999');

    expect(await screen.findByText('This term does not exist.')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'All terms' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Add class' })).not.toBeInTheDocument();
  });

  it('says when the classes could not be loaded, and loads them again', async () => {
    let fail = true;
    mockApiGet({
      ...asOffice,
      '/api/v1/admin/terms/{termId}/classes': () =>
        fail ? problem(500, 'boom') : { body: { items: autumnClassEntries } },
    });
    const user = userEvent.setup();

    renderPage();
    expect(await screen.findByText('Could not load the classes.')).toBeInTheDocument();
    fail = false;
    await user.click(screen.getByRole('button', { name: 'Retry' }));

    expect(await card('Ballet, Monday, 18:00–19:30')).toBeTruthy();
  });

  it('renders in German', async () => {
    mockApiGet(asOffice);
    await i18n.changeLanguage('de');
    const user = userEvent.setup();

    renderPage();

    const retired = await card('Jazz, Samstag, 10:00–11:00');
    expect(retired.getByText('Stillgelegt')).toBeInTheDocument();
    expect(
      retired.getByRole('button', { name: 'Jazz, Samstag 10:00 reaktivieren' }),
    ).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Kurs hinzufügen' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Kurs hinzufügen' }));
    expect(sheet.getByRole('combobox', { name: 'Korrepetition' })).toBeInTheDocument();
    expect(sheet.getByRole('combobox', { name: 'Lehrkraft' })).toBeInTheDocument();
    expect(sheet.getByRole('combobox', { name: 'Niveau' })).toBeInTheDocument();
    expect(sheet.getByRole('combobox', { name: 'Raum' })).toBeInTheDocument();
  });
});
