import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Toaster } from '@/components/ui/sonner';
import i18n from '@/i18n';
import {
  autumnClassEntries,
  balletMembers,
  catalogFixtures,
  conflict,
  ids,
  meOffice,
  mockApiDelete,
  mockApiGet,
  mockApiPost,
  mockApiPut,
  type MockRequest,
  persons,
  problem,
  staffEntries,
  terms,
} from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { ClassesPage } from './ClassesPage';

afterEach(() => {
  vi.restoreAllMocks();
});

/** The office's fixtures; only the Monday ballet class has members. */
const asOffice = {
  ...catalogFixtures,
  '/api/v1/auth/me': { body: meOffice },
  '/api/v1/terms': { body: { items: terms } },
  '/api/v1/admin/staff': { body: { items: staffEntries } },
  '/api/v1/admin/persons': { body: { items: persons } },
  '/api/v1/admin/terms/{termId}/classes': { body: { items: autumnClassEntries } },
  '/api/v1/admin/classes/{classId}/members': (init: MockRequest) => ({
    body: { items: init.params?.path?.classId === ids.classBallet ? balletMembers : [] },
  }),
};

function renderPage() {
  render(<Toaster />);
  return renderInRouter(<ClassesPage termId={ids.termAutumn} />, {
    at: '/admin/terms/$termId/classes',
  });
}

type User = ReturnType<typeof userEvent.setup>;

/** Opens the members of the Monday ballet class; returns the sheet. */
async function openBalletMembers(user: User) {
  const card = within(await screen.findByRole('article', { name: 'Ballet, Monday, 18:00–19:30' }));
  await user.click(card.getByRole('button', { name: 'Members of Ballet, Monday 18:00' }));
  return within(await screen.findByRole('dialog', { name: 'Members of Ballet, Monday 18:00' }));
}

/** Picks the person whose option matches `name` in the sheet's person field. */
async function pickPerson(user: User, sheet: ReturnType<typeof within>, name: RegExp) {
  await user.click(sheet.getByRole('combobox', { name: 'Person' }));
  const list = within(await screen.findByRole('listbox', { name: 'Persons' }));
  await user.click(await list.findByRole('option', { name }));
}

describe('ClassMembersSheet', () => {
  it('lists the members of a class with the date they joined', async () => {
    mockApiGet(asOffice);
    const user = userEvent.setup();

    renderPage();
    const sheet = await openBalletMembers(user);

    expect(await sheet.findByRole('heading', { name: '2 members' })).toBeInTheDocument();
    const items = within(sheet.getByRole('list', { name: 'Members' })).getAllByRole('listitem');
    expect(items.map((li) => li.textContent)).toEqual([
      expect.stringContaining('Mia Sample'),
      expect.stringContaining('Noah Sample'),
    ]);
    const [mia, noah] = items.map((li) => within(li));
    expect(mia?.getByText('mia.sample@example.org')).toBeInTheDocument();
    expect(mia?.getByText('Member since Aug 31, 2026')).toBeInTheDocument();
    expect(noah?.queryByText(/^Member since/)).not.toBeInTheDocument();
  });

  it('says when a class has no members', async () => {
    mockApiGet(asOffice);
    const user = userEvent.setup();

    renderPage();
    const card = within(
      await screen.findByRole('article', { name: 'Contemporary, Tuesday, 17:00–18:00' }),
    );
    await user.click(card.getByRole('button', { name: 'Members of Contemporary, Tuesday 17:00' }));
    const sheet = within(
      await screen.findByRole('dialog', { name: 'Members of Contemporary, Tuesday 17:00' }),
    );

    expect(await sheet.findByText('This class has no members yet.')).toBeInTheDocument();
    expect(sheet.getByRole('heading', { name: '0 members' })).toBeInTheDocument();
  });

  it('adds a member with the date they joined', async () => {
    let members = balletMembers;
    const alex = { id: ids.personAlex, givenName: 'Alex', familyName: 'Example' };
    mockApiGet({
      ...asOffice,
      '/api/v1/admin/classes/{classId}/members': () => ({ body: { items: members } }),
    });
    const post = mockApiPost({
      '/api/v1/classes/{classId}/members': () => {
        members = [{ person: alex, since: '2026-10-19' }, ...balletMembers];
        return { status: 201, body: members[0] };
      },
    });
    const user = userEvent.setup();

    renderPage();
    const sheet = await openBalletMembers(user);
    await pickPerson(user, sheet, /Alex Example/);
    expect(sheet.getByRole('combobox', { name: 'Person' })).toHaveTextContent('Alex Example');
    await user.type(sheet.getByLabelText('Member since (optional)'), '2026-10-19');
    await user.click(sheet.getByRole('button', { name: 'Add member' }));

    expect(
      await screen.findByText('Alex Example has been added to the class.'),
    ).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/classes/{classId}/members', {
      params: { path: { classId: ids.classBallet } },
      body: { personId: ids.personAlex, since: '2026-10-19' },
    });
    expect(await sheet.findByRole('heading', { name: '3 members' })).toBeInTheDocument();
    // The form is ready for the next person.
    expect(sheet.getByRole('combobox', { name: 'Person' })).toHaveTextContent('No person');
    expect(sheet.getByLabelText('Member since (optional)')).toHaveValue('');
  });

  it('leaves out an unknown date', async () => {
    mockApiGet(asOffice);
    const post = mockApiPost({
      '/api/v1/classes/{classId}/members': () => ({
        status: 201,
        body: { person: { id: ids.personAlex, givenName: 'Alex', familyName: 'Example' } },
      }),
    });
    const user = userEvent.setup();

    renderPage();
    const sheet = await openBalletMembers(user);
    await pickPerson(user, sheet, /Alex Example/);
    await user.click(sheet.getByRole('button', { name: 'Add member' }));

    await waitFor(() =>
      expect(post).toHaveBeenCalledWith('/api/v1/classes/{classId}/members', {
        params: { path: { classId: ids.classBallet } },
        body: { personId: ids.personAlex },
      }),
    );
  });

  it('asks for a person before sending', async () => {
    mockApiGet(asOffice);
    const post = mockApiPost({});
    const user = userEvent.setup();

    renderPage();
    const sheet = await openBalletMembers(user);
    await user.click(sheet.getByRole('button', { name: 'Add member' }));

    expect(sheet.getByRole('combobox', { name: 'Person' })).toHaveAccessibleDescription(
      'Please choose a person.',
    );
    expect(post).not.toHaveBeenCalled();
  });

  it('says when the person is a member already', async () => {
    mockApiGet(asOffice);
    mockApiPost({
      '/api/v1/classes/{classId}/members': conflict('duplicate', 'already a member'),
    });
    const user = userEvent.setup();

    renderPage();
    const sheet = await openBalletMembers(user);
    await sheet.findByRole('list', { name: 'Members' });
    await pickPerson(user, sheet, /Mia Sample/);
    await user.click(sheet.getByRole('button', { name: 'Add member' }));

    expect(await sheet.findByRole('alert')).toHaveTextContent(
      'Mia Sample is a member of this class already.',
    );
    const person = sheet.getByRole('combobox', { name: 'Person' });
    expect(person).toHaveAttribute('aria-invalid', 'true');
    expect(person).toHaveAccessibleDescription('This person is a member of the class already.');
    expect(screen.queryByText('already a member')).not.toBeInTheDocument();
  });

  it('says when the class is full', async () => {
    mockApiGet(asOffice);
    mockApiPost({
      '/api/v1/classes/{classId}/members': conflict('full', 'the class is full'),
    });
    const user = userEvent.setup();

    renderPage();
    const sheet = await openBalletMembers(user);
    await sheet.findByRole('list', { name: 'Members' });
    await pickPerson(user, sheet, /Alex Example/);
    await user.click(sheet.getByRole('button', { name: 'Add member' }));

    expect(await sheet.findByRole('alert')).toHaveTextContent(
      'The class is full: it has as many members as places. Raise its capacity first.',
    );
    expect(sheet.getByRole('combobox', { name: 'Person' })).not.toHaveAttribute(
      'aria-invalid',
      'true',
    );
    expect(screen.queryByText('the class is full')).not.toBeInTheDocument();
  });

  it('says when the class is retired', async () => {
    mockApiGet(asOffice);
    mockApiPost({
      '/api/v1/classes/{classId}/members': problem(422, 'the class is retired', [
        { location: 'path.classId' },
      ]),
    });
    const user = userEvent.setup();

    renderPage();
    const sheet = await openBalletMembers(user);
    await pickPerson(user, sheet, /Alex Example/);
    await user.click(sheet.getByRole('button', { name: 'Add member' }));

    expect(await sheet.findByRole('alert')).toHaveTextContent(
      'The class is retired and takes no members. Reactivate it first.',
    );
  });

  it('marks a person the API does not know', async () => {
    mockApiGet(asOffice);
    mockApiPost({
      '/api/v1/classes/{classId}/members': problem(422, 'unknown person', [
        { location: 'body.personId' },
      ]),
    });
    const user = userEvent.setup();

    renderPage();
    const sheet = await openBalletMembers(user);
    await pickPerson(user, sheet, /Alex Example/);
    await user.click(sheet.getByRole('button', { name: 'Add member' }));

    expect(await sheet.findByRole('alert')).toHaveTextContent('Please check your entries.');
    expect(sheet.getByRole('combobox', { name: 'Person' })).toHaveAccessibleDescription(
      'This person no longer exists; pick another.',
    );
    expect(sheet.getByLabelText('Member since (optional)')).not.toHaveAttribute(
      'aria-invalid',
      'true',
    );
  });

  it('says so when the member was removed meanwhile', async () => {
    let members = balletMembers;
    mockApiGet({
      ...asOffice,
      '/api/v1/admin/classes/{classId}/members': () => ({ body: { items: members } }),
    });
    mockApiDelete({
      '/api/v1/classes/{classId}/members/{personId}': () => {
        members = balletMembers.slice(0, 1);
        return problem(404, 'not a member');
      },
    });
    const user = userEvent.setup();

    renderPage();
    const sheet = await openBalletMembers(user);
    await user.click(await sheet.findByRole('button', { name: 'Remove Noah Sample' }));
    const question = within(
      sheet.getByRole('group', { name: 'Remove Noah Sample from Ballet, Monday 18:00?' }),
    );
    await user.click(question.getByRole('button', { name: 'Remove' }));

    expect(
      await screen.findByText('Noah Sample is no longer a member of this class.'),
    ).toBeInTheDocument();
    await waitFor(() => expect(sheet.queryByText('Noah Sample')).not.toBeInTheDocument());
  });

  it('asks before removing a member, and removes them', async () => {
    let members = balletMembers;
    mockApiGet({
      ...asOffice,
      '/api/v1/admin/classes/{classId}/members': () => ({ body: { items: members } }),
    });
    const del = mockApiDelete({
      '/api/v1/classes/{classId}/members/{personId}': () => {
        members = balletMembers.slice(0, 1);
        return { status: 204 };
      },
    });
    const user = userEvent.setup();

    renderPage();
    const sheet = await openBalletMembers(user);
    const remove = await sheet.findByRole('button', { name: 'Remove Noah Sample' });

    // Keeping the member closes the question.
    await user.click(remove);
    let question = within(
      sheet.getByRole('group', { name: 'Remove Noah Sample from Ballet, Monday 18:00?' }),
    );
    expect(question.getByRole('button', { name: 'Keep' })).toHaveFocus();
    await user.click(question.getByRole('button', { name: 'Keep' }));
    expect(sheet.queryByRole('group', { name: /^Remove Noah Sample/ })).not.toBeInTheDocument();
    expect(del).not.toHaveBeenCalled();

    await user.click(remove);
    question = within(
      sheet.getByRole('group', { name: 'Remove Noah Sample from Ballet, Monday 18:00?' }),
    );
    await user.click(question.getByRole('button', { name: 'Remove' }));

    expect(
      await screen.findByText('Noah Sample has been removed from the class.'),
    ).toBeInTheDocument();
    expect(del).toHaveBeenCalledWith('/api/v1/classes/{classId}/members/{personId}', {
      params: { path: { classId: ids.classBallet, personId: ids.personNoah } },
    });
    expect(await sheet.findByRole('heading', { name: '1 member' })).toBeInTheDocument();
    expect(sheet.queryByText('Noah Sample')).not.toBeInTheDocument();
  });

  it('shows a paused member and ends the pause', async () => {
    let members: typeof balletMembers = [{ ...balletMembers[0]!, pausedUntil: '2026-12-01' }];
    mockApiGet({
      ...asOffice,
      '/api/v1/admin/classes/{classId}/members': () => ({ body: { items: members } }),
    });
    const put = mockApiPut({
      '/api/v1/classes/{classId}/members/{personId}': () => {
        members = [balletMembers[0]!];
        return { body: balletMembers[0] };
      },
    });
    const user = userEvent.setup();

    renderPage();
    const sheet = await openBalletMembers(user);
    expect(await sheet.findByText('Paused until Dec 1, 2026')).toBeInTheDocument();
    await user.click(sheet.getByRole('button', { name: 'End the pause of Mia Sample' }));

    expect(
      await screen.findByText(
        'The pause of Mia Sample has ended: they are booked into the coming lessons again.',
      ),
    ).toBeInTheDocument();
    expect(put).toHaveBeenCalledWith('/api/v1/classes/{classId}/members/{personId}', {
      params: { path: { classId: ids.classBallet, personId: ids.personMia } },
      body: { since: '2026-08-31' },
    });
    await waitFor(() => expect(sheet.queryByText(/^Paused until/)).not.toBeInTheDocument());
  });

  it('says when the members could not be loaded, and loads them again', async () => {
    let fail = true;
    mockApiGet({
      ...asOffice,
      '/api/v1/admin/classes/{classId}/members': () =>
        fail ? problem(500, 'boom') : { body: { items: balletMembers } },
    });
    const user = userEvent.setup();

    renderPage();
    const sheet = await openBalletMembers(user);
    expect(await sheet.findByText('Could not load the data.')).toBeInTheDocument();
    fail = false;
    await user.click(sheet.getByRole('button', { name: 'Retry' }));

    expect(await sheet.findByRole('heading', { name: '2 members' })).toBeInTheDocument();
  });

  it('renders in German', async () => {
    mockApiGet(asOffice);
    await i18n.changeLanguage('de');
    const user = userEvent.setup();

    renderPage();
    const card = within(
      await screen.findByRole('article', { name: 'Ballet, Montag, 18:00–19:30' }),
    );
    await user.click(card.getByRole('button', { name: 'Mitglieder von Ballet, Montag 18:00' }));
    const sheet = within(
      await screen.findByRole('dialog', { name: 'Mitglieder von Ballet, Montag 18:00' }),
    );

    expect(await sheet.findByRole('heading', { name: '2 Kursmitglieder' })).toBeInTheDocument();
    expect(sheet.getByText('Mitglied seit 31. Aug. 2026')).toBeInTheDocument();
    expect(sheet.getByRole('button', { name: 'Kursmitglied hinzufügen' })).toBeInTheDocument();
    expect(sheet.getByRole('button', { name: 'Noah Sample entfernen' })).toBeInTheDocument();
  });
});
