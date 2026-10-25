import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Toaster } from '@/components/ui/sonner';
import i18n from '@/i18n';
import {
  conflict,
  ids,
  meAdmin,
  meOffice,
  mockApiDelete,
  mockApiGet,
  mockApiPost,
  mockApiPut,
  type MockRequest,
  personDetails,
  persons,
  problem,
  publicSettings,
} from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { PersonPage } from './PersonPage';

afterEach(() => {
  vi.restoreAllMocks();
});

/** The persons whose name or email contains every word of the request's `q`. */
function searchPersons(init: MockRequest) {
  const words = String(init.params?.query?.q ?? '')
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean);
  return {
    body: {
      items: persons.filter((p) =>
        words.every((w) =>
          `${p.givenName} ${p.familyName} ${p.email ?? ''}`.toLowerCase().includes(w),
        ),
      ),
    },
  };
}

const fixtures = (me = meOffice) => ({
  '/api/v1/settings/public': { body: publicSettings },
  '/api/v1/auth/me': { body: me },
  '/api/v1/admin/persons': searchPersons,
  '/api/v1/admin/persons/{personId}': (init: MockRequest) => {
    const detail = personDetails[init.params?.path?.personId ?? ''];
    return detail ? { body: detail } : problem(404, 'resource not found');
  },
});

function renderPage(personId: string) {
  render(<Toaster />);
  return renderInRouter(<PersonPage personId={personId} />, { at: '/admin/persons/$personId' });
}

const region = async (name: string) => within(await screen.findByRole('region', { name }));

describe('PersonPage', () => {
  it('shows the data, guardians, account and classes of a child', async () => {
    mockApiGet(fixtures());

    renderPage(ids.personNoah);

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Noah Sample' }),
    ).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'All persons' })).toHaveAttribute(
      'href',
      '/admin/persons',
    );
    const details = await region('Details');
    expect(details.getByText('April 9, 2015')).toBeInTheDocument();

    const guardians = await region('Guardians');
    expect(guardians.getByRole('link', { name: 'Mia Sample' })).toHaveAttribute(
      'href',
      `/admin/persons/${ids.personMia}`,
    );

    const account = await region('Account');
    expect(account.getByText('Noah Sample has no account and cannot sign in.')).toBeInTheDocument();
    expect(account.getByRole('button', { name: 'Create account' })).toBeInTheDocument();

    const classes = await region('Classes');
    expect(
      classes.getByRole('link', { name: 'Ballet, Monday 18:00 (Autumn 2026)' }),
    ).toHaveAttribute('href', `/admin/terms/${ids.termAutumn}/classes`);
  });

  it('shows the account, children and notes of a guardian', async () => {
    mockApiGet(fixtures());

    renderPage(ids.personMia);

    const details = await region('Details');
    expect(details.getByText('mia.sample@example.org')).toBeInTheDocument();
    expect(details.getByText('+49 000 0000201')).toBeInTheDocument();
    expect(details.getByText('Prefers to be called in the evening.')).toBeInTheDocument();
    expect((await region('Children')).getByRole('link', { name: 'Noah Sample' })).toHaveAttribute(
      'href',
      `/admin/persons/${ids.personNoah}`,
    );
    expect(
      (await region('Guardians')).getByText('Mia Sample has no guardian.'),
    ).toBeInTheDocument();
    const account = await region('Account');
    expect(account.getByText('Member')).toBeInTheDocument();
    expect(account.getByText('Active')).toBeInTheDocument();
    expect(account.queryByRole('button', { name: 'Create account' })).not.toBeInTheDocument();
  });

  it('shows the staff link, a pending account and a retired class', async () => {
    mockApiGet(fixtures());

    renderPage(ids.personAlex);

    const staff = await region('Staff');
    expect(staff.getByText('Alex Example')).toBeInTheDocument();
    expect(staff.getByText('Instructor')).toBeInTheDocument();
    const account = await region('Account');
    expect(account.getByText('Unconfirmed')).toBeInTheDocument();
    expect(
      account.getByText(
        'The account becomes active once Alex confirms the email address with the link from the invitation or the registration.',
      ),
    ).toBeInTheDocument();
    const classes = await region('Classes');
    expect(
      classes.getByRole('link', { name: 'Jazz, Saturday 10:00 (Autumn 2026)' }),
    ).toBeInTheDocument();
    expect(classes.getByText('Retired')).toBeInTheDocument();
    expect(classes.getByText('Member since Aug 31, 2026')).toBeInTheDocument();
  });

  it('says when the person does not exist', async () => {
    mockApiGet(fixtures());

    renderPage('0190a000-0000-7000-8000-999999999999');

    expect(await screen.findByText('This person does not exist.')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'All persons' })).toBeInTheDocument();
  });

  it('edits every field in a sheet', async () => {
    mockApiGet(fixtures());
    const put = mockApiPut({
      '/api/v1/admin/persons/{personId}': ({ body }) => ({
        body: { id: ids.personMia, ...(body as object) },
      }),
    });
    const user = userEvent.setup();

    renderPage(ids.personMia);
    await user.click(
      (await region('Details')).getByRole('button', { name: 'Edit the details of Mia Sample' }),
    );
    const sheet = within(await screen.findByRole('dialog', { name: 'Edit Mia Sample' }));
    expect(sheet.getByLabelText('Email (optional)')).toHaveValue('mia.sample@example.org');
    expect(sheet.getByLabelText('Notes (optional)')).toHaveValue(
      'Prefers to be called in the evening.',
    );
    await user.clear(sheet.getByLabelText('Phone (optional)'));
    await user.type(sheet.getByLabelText('Phone (optional)'), '+49 000 0000222');
    await user.clear(sheet.getByLabelText('Notes (optional)'));
    await user.click(sheet.getByRole('button', { name: 'Save changes' }));

    expect(await screen.findByText('Mia Sample has been saved.')).toBeInTheDocument();
    expect(put).toHaveBeenCalledWith('/api/v1/admin/persons/{personId}', {
      params: { path: { personId: ids.personMia } },
      body: {
        givenName: 'Mia',
        familyName: 'Sample',
        email: 'mia.sample@example.org',
        phone: '+49 000 0000222',
      },
    });
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  });

  it('edits the member number and says when another person has it', async () => {
    mockApiGet(fixtures());
    const put = mockApiPut({ '/api/v1/admin/persons/{personId}': conflict('duplicate') });
    const user = userEvent.setup();

    renderPage(ids.personMia);
    await user.click(
      (await region('Details')).getByRole('button', { name: 'Edit the details of Mia Sample' }),
    );
    const sheet = within(await screen.findByRole('dialog', { name: 'Edit Mia Sample' }));
    await user.type(sheet.getByLabelText('Member number (optional)'), ' M-0042 ');
    await user.click(sheet.getByRole('button', { name: 'Save changes' }));

    expect(await sheet.findByText('Another person has this member number.')).toBeInTheDocument();
    expect(put.mock.calls[0]?.[1]).toMatchObject({ body: { memberNo: 'M-0042' } });
  });

  it('shows a merged person as a record kept for its receipts, and the persons merged into one', async () => {
    const mia = personDetails[ids.personMia]!;
    const tombstone = '0190a000-0000-7000-8000-000000000699';
    mockApiGet({
      ...fixtures(),
      '/api/v1/admin/persons/{personId}': (init: MockRequest) =>
        init.params?.path?.personId === tombstone
          ? {
              body: {
                ...mia,
                id: tombstone,
                memberNo: undefined,
                account: undefined,
                dependents: [],
                mergedInto: { id: ids.personMia, givenName: 'Mia', familyName: 'Sample' },
              },
            }
          : {
              body: {
                ...mia,
                memberNo: 'M-0042',
                merged: [{ id: tombstone, givenName: 'Mia', familyName: 'Sampel' }],
              },
            },
    });

    renderPage(tombstone);

    expect(await screen.findByText('Merged into another person')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Open Mia Sample' })).toHaveAttribute(
      'href',
      `/admin/persons/${ids.personMia}`,
    );
    expect(screen.queryByRole('button', { name: /Delete|Edit/ })).not.toBeInTheDocument();
  });

  it('lists the duplicates merged into a person and its member number', async () => {
    const mia = personDetails[ids.personMia]!;
    mockApiGet({
      ...fixtures(),
      '/api/v1/admin/persons/{personId}': {
        body: {
          ...mia,
          memberNo: 'M-0042',
          merged: [
            { id: '0190a000-0000-7000-8000-000000000699', givenName: 'Mia', familyName: 'Sampel' },
          ],
        },
      },
    });

    renderPage(ids.personMia);

    const details = await region('Details');
    expect(details.getByText('M-0042')).toBeInTheDocument();
    expect(details.getByRole('link', { name: 'Mia Sampel' })).toBeInTheDocument();
  });

  describe('guardians', () => {
    /** Picks a person in the guardian field of the add form. */
    async function pick(user: ReturnType<typeof userEvent.setup>, search: string, name: RegExp) {
      const form = within(await screen.findByRole('form', { name: 'Add a guardian' }));
      await user.click(form.getByRole('combobox', { name: 'Guardian' }));
      await user.type(
        await screen.findByRole('combobox', { name: 'Search by name or email' }),
        search,
      );
      await user.click(await screen.findByRole('option', { name }));
      return form;
    }

    it('links a guardian', async () => {
      mockApiGet(fixtures());
      const post = mockApiPost({
        '/api/v1/admin/persons/{personId}/guardians': { status: 201, body: persons[0] },
      });
      const user = userEvent.setup();

      renderPage(ids.personNoah);
      const form = await pick(user, 'alex', /Alex Example/);
      await user.click(form.getByRole('button', { name: 'Add guardian' }));

      expect(
        await screen.findByText('Alex Example is now a guardian of Noah Sample.'),
      ).toBeInTheDocument();
      expect(post).toHaveBeenCalledWith('/api/v1/admin/persons/{personId}/guardians', {
        params: { path: { personId: ids.personNoah } },
        body: { guardianId: ids.personAlex },
      });
      await waitFor(() =>
        expect(form.getByRole('combobox', { name: 'Guardian' })).toHaveTextContent('No person'),
      );
    });

    it('says when the guardian is linked already', async () => {
      mockApiGet(fixtures());
      mockApiPost({
        '/api/v1/admin/persons/{personId}/guardians': conflict('duplicate'),
      });
      const user = userEvent.setup();

      renderPage(ids.personNoah);
      const form = await pick(user, 'mia', /Mia Sample/);
      await user.click(form.getByRole('button', { name: 'Add guardian' }));

      expect(await form.findByRole('alert')).toHaveTextContent(
        'Mia Sample is a guardian of Noah Sample already.',
      );
      expect(form.getByRole('combobox', { name: 'Guardian' })).toHaveAttribute(
        'aria-invalid',
        'true',
      );
    });

    it('does not offer the person as their own guardian', async () => {
      mockApiGet(fixtures());
      const post = mockApiPost({});
      const user = userEvent.setup();

      renderPage(ids.personNoah);
      const form = await pick(user, 'noah', /Noah Sample/);
      await user.click(form.getByRole('button', { name: 'Add guardian' }));

      expect(await form.findByText('A person cannot be their own guardian.')).toBeInTheDocument();
      expect(post).not.toHaveBeenCalled();
    });

    it('does not offer a child of the person as their guardian', async () => {
      mockApiGet(fixtures());
      const post = mockApiPost({});
      const user = userEvent.setup();

      renderPage(ids.personMia);
      const form = await pick(user, 'noah', /Noah Sample/);
      await user.click(form.getByRole('button', { name: 'Add guardian' }));

      expect(
        await form.findByText('A child of this person cannot be their guardian.'),
      ).toBeInTheDocument();
      expect(post).not.toHaveBeenCalled();
    });

    it('marks the guardian the API refuses, e.g. a grandchild (a cycle)', async () => {
      mockApiGet(fixtures());
      mockApiPost({
        '/api/v1/admin/persons/{personId}/guardians': problem(422, 'validation failed', [
          { location: 'body.guardianId' },
        ]),
      });
      const user = userEvent.setup();

      renderPage(ids.personNoah);
      const form = await pick(user, 'alex', /Alex Example/);
      await user.click(form.getByRole('button', { name: 'Add guardian' }));

      expect(await form.findByRole('alert')).toHaveTextContent('Please check your entries.');
      const field = form.getByRole('combobox', { name: 'Guardian' });
      expect(field).toHaveAttribute('aria-invalid', 'true');
      expect(field).toHaveAccessibleDescription(
        'This person cannot be a guardian here: they no longer exist, or they are a child or grandchild of this person.',
      );
    });

    it('removes a guardian after asking', async () => {
      mockApiGet(fixtures());
      const del = mockApiDelete({
        '/api/v1/admin/persons/{personId}/guardians/{guardianId}': { status: 204 },
      });
      const user = userEvent.setup();

      renderPage(ids.personNoah);
      const guardians = await region('Guardians');
      await user.click(guardians.getByRole('button', { name: 'Remove Mia Sample as guardian' }));
      const question = within(
        guardians.getByRole('group', { name: 'Remove Mia Sample as guardian of Noah Sample?' }),
      );
      expect(
        question.getByText('Noah then has no guardian left but stays registered.'),
      ).toBeInTheDocument();
      expect(question.getByRole('button', { name: 'Keep' })).toHaveFocus();
      await user.click(question.getByRole('button', { name: 'Remove' }));

      expect(
        await screen.findByText('Mia Sample is no longer a guardian of Noah Sample.'),
      ).toBeInTheDocument();
      expect(del).toHaveBeenCalledWith('/api/v1/admin/persons/{personId}/guardians/{guardianId}', {
        params: { path: { personId: ids.personNoah, guardianId: ids.personMia } },
      });
    });
  });

  describe('delete', () => {
    it('deletes a person after asking and returns to the list', async () => {
      mockApiGet(fixtures());
      const del = mockApiDelete({ '/api/v1/admin/persons/{personId}': { status: 204 } });
      const user = userEvent.setup();

      const { router } = renderPage(ids.personNoah);
      await user.click(await screen.findByRole('button', { name: 'Delete Noah Sample' }));
      const dialog = within(
        await screen.findByRole('alertdialog', { name: 'Delete Noah Sample?' }),
      );
      expect(del).not.toHaveBeenCalled();
      await user.click(dialog.getByRole('button', { name: 'Delete' }));

      expect(await screen.findByText('Noah Sample has been deleted.')).toBeInTheDocument();
      expect(del).toHaveBeenCalledWith('/api/v1/admin/persons/{personId}', {
        params: { path: { personId: ids.personNoah } },
      });
      await waitFor(() => expect(router.state.location.pathname).toBe('/admin/persons'));
    });

    it('says why a person something refers to stays', async () => {
      mockApiGet(fixtures());
      mockApiDelete({ '/api/v1/admin/persons/{personId}': conflict('in-use') });
      const user = userEvent.setup();

      const { router } = renderPage(ids.personNoah);
      await user.click(await screen.findByRole('button', { name: 'Delete Noah Sample' }));
      const dialog = within(
        await screen.findByRole('alertdialog', { name: 'Delete Noah Sample?' }),
      );
      await user.click(dialog.getByRole('button', { name: 'Delete' }));

      expect(await dialog.findByRole('alert')).toHaveTextContent(
        'Noah Sample still has an account, a staff link, bookings, class memberships, passes they own or paid for, guardians or children, so they cannot be deleted. Remove those first; passes are never deleted, so a person with passes stays.',
      );
      expect(router.state.location.pathname).not.toBe('/admin/persons');
    });
  });

  describe('account', () => {
    it('creates an account and sends the invitation', async () => {
      mockApiGet(fixtures());
      const post = mockApiPost({
        '/api/v1/admin/persons/{personId}/account': ({ body }) => ({
          status: 201,
          body: { id: 'account-new', status: 'pending', ...(body as object) },
        }),
      });
      const user = userEvent.setup();

      renderPage(ids.personNoah);
      await user.click((await region('Account')).getByRole('button', { name: 'Create account' }));
      const sheet = within(
        await screen.findByRole('dialog', { name: 'Create an account for Noah Sample' }),
      );
      await user.type(sheet.getByLabelText('Email'), 'noah.sample@example.org');
      await user.click(sheet.getByRole('combobox', { name: 'Role' }));
      const options = (await screen.findAllByRole('option')).map((o) => o.textContent);
      // Only an admin may create an admin.
      expect(options).toEqual(['Member', 'Instructor', 'Office']);
      await user.click(screen.getByRole('option', { name: 'Member' }));
      await user.click(sheet.getByRole('button', { name: 'Create and invite' }));

      expect(
        await screen.findByText(
          'Noah Sample has an account now. The invitation has been sent to noah.sample@example.org.',
        ),
      ).toBeInTheDocument();
      expect(post).toHaveBeenCalledWith('/api/v1/admin/persons/{personId}/account', {
        params: { path: { personId: ids.personNoah } },
        body: { email: 'noah.sample@example.org', role: 'member' },
      });
      await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    });

    it('starts with the person’s email and offers admins every role', async () => {
      mockApiGet({
        ...fixtures(meAdmin),
        '/api/v1/admin/persons/{personId}': {
          body: { ...personDetails[ids.personMia]!, account: undefined },
        },
      });
      const user = userEvent.setup();

      renderPage(ids.personMia);
      await user.click((await region('Account')).getByRole('button', { name: 'Create account' }));
      const sheet = within(
        await screen.findByRole('dialog', { name: 'Create an account for Mia Sample' }),
      );
      expect(sheet.getByLabelText('Email')).toHaveValue('mia.sample@example.org');
      await user.click(sheet.getByRole('combobox', { name: 'Role' }));
      expect((await screen.findAllByRole('option')).map((o) => o.textContent)).toEqual([
        'Member',
        'Instructor',
        'Office',
        'Administrator',
      ]);
    });

    it('says when the email belongs to another account', async () => {
      mockApiGet(fixtures());
      mockApiPost({ '/api/v1/admin/persons/{personId}/account': conflict('duplicate') });
      const user = userEvent.setup();

      renderPage(ids.personNoah);
      await user.click((await region('Account')).getByRole('button', { name: 'Create account' }));
      const sheet = within(
        await screen.findByRole('dialog', { name: 'Create an account for Noah Sample' }),
      );
      await user.type(sheet.getByLabelText('Email'), 'mia.sample@example.org');
      await user.click(sheet.getByRole('button', { name: 'Create and invite' }));

      expect(await sheet.findByRole('alert')).toHaveTextContent(
        'Another account already uses this email address. Choose another one.',
      );
      expect(sheet.getByLabelText('Email')).toHaveAttribute('aria-invalid', 'true');
      expect(
        sheet.getByText('Another account already uses this email address.'),
      ).toBeInTheDocument();
    });

    it('says when the person got an account meanwhile and asks for the person again', async () => {
      const get = mockApiGet(fixtures());
      mockApiPost({ '/api/v1/admin/persons/{personId}/account': conflict('has-account') });
      const user = userEvent.setup();

      renderPage(ids.personNoah);
      await user.click((await region('Account')).getByRole('button', { name: 'Create account' }));
      const sheet = within(
        await screen.findByRole('dialog', { name: 'Create an account for Noah Sample' }),
      );
      await user.type(sheet.getByLabelText('Email'), 'noah.sample@example.org');
      const asked = () =>
        (get.mock.calls as unknown[][]).filter(
          ([path]) => path === '/api/v1/admin/persons/{personId}',
        ).length;
      const before = asked();
      await user.click(sheet.getByRole('button', { name: 'Create and invite' }));

      expect(await sheet.findByRole('alert')).toHaveTextContent(
        'Noah Sample has an account already, perhaps created meanwhile. Close this form to see it.',
      );
      // Not the email's fault.
      expect(sheet.getByLabelText('Email')).not.toHaveAttribute('aria-invalid');
      await waitFor(() => expect(asked()).toBeGreaterThan(before));
    });
  });

  it('speaks German', async () => {
    await i18n.changeLanguage('de');
    mockApiGet(fixtures());

    renderPage(ids.personNoah);

    expect(
      await screen.findByRole('region', { name: 'Erziehungsberechtigte' }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole('link', { name: 'Ballet, Montag 18:00 (Autumn 2026)' }),
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Konto anlegen' })).toBeInTheDocument();
  });
});
