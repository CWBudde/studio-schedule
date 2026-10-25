import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Toaster } from '@/components/ui/sonner';
import i18n from '@/i18n';
import {
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
  searchQueryOf,
  expectOnlySearches,
} from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { StaffPage } from './StaffPage';

beforeEach(() => {
  mockApiPost({ '/api/v1/admin/persons/search': searchPersons });
});

afterEach(() => {
  vi.restoreAllMocks();
});

/** The persons whose name or email contains every word of the request's `query`. */
function searchPersons(init: MockRequest) {
  const words = searchQueryOf(init).toLowerCase().split(/\s+/).filter(Boolean);
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

const asOffice = {
  '/api/v1/auth/me': { body: meOffice },
  '/api/v1/admin/staff': { body: { items: staffEntries } },
};

function renderPage() {
  render(<Toaster />);
  return renderInRouter(<StaffPage />);
}

/** The body row of the staff table whose first cell is `name`. */
async function row(name: string, table = 'Staff') {
  const rows = within(await screen.findByRole('table', { name: table }));
  // The first match: the display name comes before a linked person of the same name.
  const [cell] = await rows.findAllByRole('cell', { name: new RegExp(`^${name}`) });
  const tr = cell?.closest('tr');
  if (!tr) throw new Error(`no row ${name}`);
  return within(tr);
}

/** The first cells of the staff table's body rows, in display order. */
const names = () =>
  within(screen.getByRole('table', { name: 'Staff' }))
    .getAllByRole('row')
    .slice(1)
    .map((tr) => within(tr).getAllByRole('cell')[0]?.textContent);

describe('StaffPage', () => {
  it('lists all staff with role, person and status, inactive ones included', async () => {
    mockApiGet(asOffice);

    renderPage();

    expect(await screen.findByRole('heading', { level: 1, name: 'Staff' })).toBeInTheDocument();
    const alex = await row('Alex Example');
    expect(alex.getByText('Instructor')).toBeInTheDocument();
    // The display name and the linked person's name.
    expect(alex.getAllByRole('cell', { name: 'Alex Example' })).toHaveLength(2);
    expect(alex.getByText('Active')).toBeInTheDocument();
    expect((await row('Quinn Former')).getByText('Inactive')).toBeInTheDocument();
    expect((await row('Sam Pianist')).getByText('Accompanist')).toBeInTheDocument();
    expect(names()).toEqual(['Alex Example', 'Quinn Former', 'Riley Sample', 'Sam Pianist']);
  });

  it('shows only the active or the inactive staff', async () => {
    mockApiGet(asOffice);
    const user = userEvent.setup();

    renderPage();
    await row('Alex Example');
    const show = within(screen.getByRole('radiogroup', { name: 'Show' }));
    expect(show.getByRole('radio', { name: 'All' })).toBeChecked();

    await user.click(show.getByRole('radio', { name: 'Inactive' }));
    expect(names()).toEqual(['Quinn Former']);

    await user.click(show.getByRole('radio', { name: 'Active' }));
    expect(names()).toEqual(['Alex Example', 'Riley Sample', 'Sam Pianist']);
  });

  it('adds an accompanist linked to a person', async () => {
    mockApiGet(asOffice);
    const post = mockApiPost({
      '/api/v1/staff': ({ body }) => ({
        status: 201,
        body: { id: 'staff-new', ...(body as object) },
      }),
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(await screen.findByRole('button', { name: 'Add staff member' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Add a staff member' }));
    expect(sheet.getByRole('switch', { name: 'Active' })).toBeChecked();
    await user.type(sheet.getByLabelText('Display name'), ' Mia Keys ');
    await user.click(sheet.getByRole('combobox', { name: 'Role' }));
    await user.click(await screen.findByRole('option', { name: 'Accompanist' }));
    await user.click(sheet.getByRole('combobox', { name: 'Person (optional)' }));
    await user.type(
      await screen.findByRole('combobox', { name: 'Search by name or email' }),
      'mia',
    );
    await waitFor(() => expect(screen.getAllByRole('option')).toHaveLength(1));
    await user.click(screen.getByRole('option', { name: /Mia Sample/ }));
    expect(sheet.getByRole('combobox', { name: 'Person (optional)' })).toHaveTextContent(
      'Mia Sample',
    );
    await user.click(sheet.getByRole('button', { name: 'Add staff member' }));

    expect(await screen.findByText('Mia Keys has been added.')).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/staff', {
      body: { displayName: 'Mia Keys', kind: 'accompanist', personId: ids.personMia, active: true },
    });
  });

  it('adds an instructor without a person', async () => {
    mockApiGet(asOffice);
    const post = mockApiPost({
      '/api/v1/staff': ({ body }) => ({
        status: 201,
        body: { id: 'staff-new', ...(body as object) },
      }),
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(await screen.findByRole('button', { name: 'Add staff member' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Add a staff member' }));
    expect(sheet.getByRole('combobox', { name: 'Role' })).toHaveTextContent('Instructor');
    await user.type(sheet.getByLabelText('Display name'), 'Kim Guest');
    await user.click(sheet.getByRole('button', { name: 'Add staff member' }));

    expect(await screen.findByText('Kim Guest has been added.')).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/staff', {
      body: { displayName: 'Kim Guest', kind: 'instructor', active: true },
    });
  });

  it('checks the entries before sending them', async () => {
    mockApiGet(asOffice);
    const post = mockApiPost({});
    const user = userEvent.setup();

    renderPage();
    await user.click(await screen.findByRole('button', { name: 'Add staff member' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Add a staff member' }));
    await user.click(sheet.getByRole('button', { name: 'Add staff member' }));

    expect(sheet.getByLabelText('Display name')).toHaveAccessibleDescription(
      /Please fill in this field\.$/,
    );
    expectOnlySearches(post);
  });

  it('deactivates a staff member and keeps the linked person', async () => {
    mockApiGet(asOffice);
    const put = mockApiPut({
      '/api/v1/staff/{staffId}': ({ body }) => ({
        body: { id: ids.staffAlex, ...(body as object) },
      }),
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(
      (await row('Alex Example')).getByRole('button', { name: 'Edit Alex Example' }),
    );
    const sheet = within(await screen.findByRole('dialog', { name: 'Edit Alex Example' }));
    expect(sheet.getByLabelText('Display name')).toHaveValue('Alex Example');
    expect(sheet.getByRole('combobox', { name: 'Role' })).toHaveTextContent('Instructor');
    expect(sheet.getByRole('combobox', { name: 'Person (optional)' })).toHaveTextContent(
      'Alex Example',
    );
    await user.click(sheet.getByRole('switch', { name: 'Active' }));
    expect(sheet.getByRole('switch', { name: 'Active' })).not.toBeChecked();
    await user.click(sheet.getByRole('button', { name: 'Save changes' }));

    expect(await screen.findByText('Alex Example has been saved.')).toBeInTheDocument();
    expect(put).toHaveBeenCalledWith('/api/v1/staff/{staffId}', {
      params: { path: { staffId: ids.staffAlex } },
      body: {
        displayName: 'Alex Example',
        kind: 'instructor',
        personId: ids.personAlex,
        active: false,
      },
    });
  });

  it('removes the link to a person', async () => {
    mockApiGet(asOffice);
    const put = mockApiPut({
      '/api/v1/staff/{staffId}': ({ body }) => ({
        body: { id: ids.staffAlex, ...(body as object) },
      }),
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(
      (await row('Alex Example')).getByRole('button', { name: 'Edit Alex Example' }),
    );
    const sheet = within(await screen.findByRole('dialog', { name: 'Edit Alex Example' }));
    await user.click(sheet.getByRole('button', { name: 'Remove the person' }));
    await user.click(sheet.getByRole('button', { name: 'Save changes' }));

    await waitFor(() => expect(put).toHaveBeenCalledOnce());
    expect(put).toHaveBeenCalledWith('/api/v1/staff/{staffId}', {
      params: { path: { staffId: ids.staffAlex } },
      body: { displayName: 'Alex Example', kind: 'instructor', active: true },
    });
  });

  it('says when the linked person no longer exists (422)', async () => {
    mockApiGet(asOffice);
    mockApiPut({
      '/api/v1/staff/{staffId}': problem(422, 'unknown person', [{ location: 'body.personId' }]),
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(
      (await row('Alex Example')).getByRole('button', { name: 'Edit Alex Example' }),
    );
    const sheet = within(await screen.findByRole('dialog', { name: 'Edit Alex Example' }));
    await user.click(sheet.getByRole('button', { name: 'Save changes' }));

    expect(await sheet.findByRole('alert')).toHaveTextContent('Please check your entries.');
    const person = sheet.getByRole('combobox', { name: 'Person (optional)' });
    expect(person).toHaveAttribute('aria-invalid', 'true');
    expect(person).toHaveAccessibleDescription(/This person no longer exists; pick another\.$/);
    expect(sheet.getByLabelText('Display name')).not.toHaveAttribute('aria-invalid', 'true');
  });

  it('suggests deactivating a staff member who still teaches', async () => {
    mockApiGet(asOffice);
    const del = mockApiDelete({ '/api/v1/staff/{staffId}': conflict('in-use', 'staff in use') });
    const user = userEvent.setup();

    renderPage();
    await user.click(
      (await row('Riley Sample')).getByRole('button', { name: 'Delete Riley Sample' }),
    );
    const dialog = within(await screen.findByRole('alertdialog', { name: 'Delete Riley Sample?' }));
    await user.click(dialog.getByRole('button', { name: 'Delete' }));

    expect(await dialog.findByRole('alert')).toHaveTextContent(
      'Riley Sample still teaches classes or lessons, so they cannot be deleted. Deactivate them instead: they leave the public lists but stay on their lessons.',
    );
    expect(del).toHaveBeenCalledWith('/api/v1/staff/{staffId}', {
      params: { path: { staffId: ids.staffRiley } },
    });
  });

  it('deactivates a staff member who still teaches in one click', async () => {
    let deactivated = false;
    mockApiGet({
      ...asOffice,
      '/api/v1/admin/staff': () => ({
        body: {
          items: staffEntries.map((s) =>
            s.id === ids.staffAlex && deactivated ? { ...s, active: false } : s,
          ),
        },
      }),
    });
    mockApiDelete({ '/api/v1/staff/{staffId}': conflict('in-use', 'staff in use') });
    const put = mockApiPut({
      '/api/v1/staff/{staffId}': ({ body }) => {
        deactivated = true;
        return { body: { id: ids.staffAlex, ...(body as object) } };
      },
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(
      (await row('Alex Example')).getByRole('button', { name: 'Delete Alex Example' }),
    );
    const dialog = within(await screen.findByRole('alertdialog', { name: 'Delete Alex Example?' }));
    await user.click(dialog.getByRole('button', { name: 'Delete' }));
    await user.click(await dialog.findByRole('button', { name: 'Deactivate instead' }));

    expect(await screen.findByText('Alex Example has been deactivated.')).toBeInTheDocument();
    // The name, role and person stay as they are.
    expect(put).toHaveBeenCalledWith('/api/v1/staff/{staffId}', {
      params: { path: { staffId: ids.staffAlex } },
      body: {
        displayName: 'Alex Example',
        kind: 'instructor',
        personId: ids.personAlex,
        active: false,
      },
    });
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
    expect(await (await row('Alex Example')).findByText('Inactive')).toBeInTheDocument();
  });

  it('offers no deactivation for inactive staff who still teach', async () => {
    mockApiGet(asOffice);
    mockApiDelete({ '/api/v1/staff/{staffId}': conflict('in-use', 'staff in use') });
    const user = userEvent.setup();

    renderPage();
    await user.click(
      (await row('Quinn Former')).getByRole('button', { name: 'Delete Quinn Former' }),
    );
    const dialog = within(await screen.findByRole('alertdialog', { name: 'Delete Quinn Former?' }));
    await user.click(dialog.getByRole('button', { name: 'Delete' }));

    expect(dialog.getByText(/^The staff member is deleted for good\./)).not.toHaveTextContent(
      'deactivate',
    );
    expect(await dialog.findByRole('alert')).toHaveTextContent(
      'Quinn Former still teaches classes or lessons, so they cannot be deleted. They are inactive already: they stay on their lessons but not in the public lists.',
    );
    expect(dialog.queryByRole('button', { name: 'Deactivate instead' })).not.toBeInTheDocument();
  });

  it('deletes a staff member nothing refers to', async () => {
    let deleted = false;
    mockApiGet({
      ...asOffice,
      '/api/v1/admin/staff': () => ({
        body: {
          items: deleted ? staffEntries.filter((s) => s.id !== ids.staffQuinn) : staffEntries,
        },
      }),
    });
    mockApiDelete({
      '/api/v1/staff/{staffId}': () => {
        deleted = true;
        return { status: 204 };
      },
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(
      (await row('Quinn Former')).getByRole('button', { name: 'Delete Quinn Former' }),
    );
    const dialog = within(await screen.findByRole('alertdialog', { name: 'Delete Quinn Former?' }));
    await user.click(dialog.getByRole('button', { name: 'Delete' }));

    expect(await screen.findByText('Quinn Former has been deleted.')).toBeInTheDocument();
    await waitFor(() => expect(names()).not.toContain('Quinn Former'));
  });

  it('renders in German', async () => {
    mockApiGet(asOffice);
    await i18n.changeLanguage('de');

    renderPage();

    expect(await screen.findByRole('heading', { level: 1, name: 'Personal' })).toBeInTheDocument();
    const sam = await row('Sam Pianist', 'Personal');
    expect(sam.getByText('Korrepetition')).toBeInTheDocument();
    expect(sam.getByText('Aktiv')).toBeInTheDocument();
  });
});
