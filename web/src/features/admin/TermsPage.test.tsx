import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
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
  problem,
  terms,
} from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { TermsPage } from './TermsPage';

afterEach(() => {
  vi.restoreAllMocks();
});

const asOffice = {
  '/api/v1/auth/me': { body: meOffice },
  '/api/v1/terms': { body: { items: terms } },
};

function renderPage() {
  render(<Toaster />);
  return renderInRouter(<TermsPage />, { at: '/admin/terms' });
}

/** The body row of the terms table whose first cell is `name`. */
async function row(name: string) {
  const table = within(await screen.findByRole('table', { name: 'Terms' }));
  const tr = (await table.findByRole('cell', { name })).closest('tr');
  if (!tr) throw new Error(`no row ${name}`);
  return within(tr);
}

/** Replaces the value of the input `label` of `scope` with `value`. */
async function retype(
  user: ReturnType<typeof userEvent.setup>,
  scope: ReturnType<typeof within>,
  label: string,
  value: string,
) {
  await user.clear(scope.getByLabelText(label));
  await user.type(scope.getByLabelText(label), value);
}

describe('TermsPage', () => {
  it('lists the terms with their dates and links to their classes', async () => {
    mockApiGet(asOffice);

    renderPage();

    expect(await screen.findByRole('heading', { level: 1, name: 'Terms' })).toBeInTheDocument();
    const autumn = await row('Autumn 2026');
    expect(autumn.getByText('Aug 31, 2026 – Jan 29, 2027')).toBeInTheDocument();
    expect(autumn.getByRole('link', { name: 'Classes of Autumn 2026' })).toHaveAttribute(
      'href',
      `/admin/terms/${ids.termAutumn}/classes`,
    );
    expect(autumn.getByRole('button', { name: 'Edit Autumn 2026' })).toBeInTheDocument();
    expect(autumn.getByRole('button', { name: 'Delete Autumn 2026' })).toBeInTheDocument();
    expect((await row('Spring 2027')).getByText('Feb 1 – Jul 16, 2027')).toBeInTheDocument();
  });

  it('opens the classes of a term', async () => {
    mockApiGet(asOffice);
    const user = userEvent.setup();

    const { router } = renderPage();
    await user.click(await screen.findByRole('link', { name: 'Classes of Spring 2027' }));

    await vi.waitFor(() =>
      expect(router.state.location.pathname).toBe(`/admin/terms/${ids.termSpring}/classes`),
    );
  });

  it('adds a term', async () => {
    let created = false;
    const added = {
      id: 'term-new',
      name: 'Summer 2027',
      startsOn: '2027-07-19',
      endsOn: '2027-08-27',
    };
    mockApiGet({
      ...asOffice,
      '/api/v1/terms': () => ({ body: { items: created ? [...terms, added] : terms } }),
    });
    const post = mockApiPost({
      '/api/v1/terms': () => {
        created = true;
        return { status: 201, body: added };
      },
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(await screen.findByRole('button', { name: 'Add term' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Add a term' }));
    await user.type(sheet.getByLabelText('Name'), ' Summer 2027 ');
    await user.type(sheet.getByLabelText('First day'), '2027-07-19');
    await user.type(sheet.getByLabelText('Last day'), '2027-08-27');
    await user.click(sheet.getByRole('button', { name: 'Add term' }));

    expect(await screen.findByText('Summer 2027 has been added.')).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/terms', {
      body: { name: 'Summer 2027', startsOn: '2027-07-19', endsOn: '2027-08-27' },
    });
    expect((await row('Summer 2027')).getByText('Jul 19 – Aug 27, 2027')).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.queryByRole('dialog', { name: 'Add a term' })).not.toBeInTheDocument(),
    );
  });

  it('checks the entries before sending them', async () => {
    mockApiGet(asOffice);
    const post = mockApiPost({});
    const user = userEvent.setup();

    renderPage();
    await user.click(await screen.findByRole('button', { name: 'Add term' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Add a term' }));
    await user.click(sheet.getByRole('button', { name: 'Add term' }));

    expect(sheet.getByLabelText('Name')).toHaveAccessibleDescription(
      /Please fill in this field\.$/,
    );
    expect(sheet.getByLabelText('First day')).toHaveAccessibleDescription(
      'Please fill in this field.',
    );

    await user.type(sheet.getByLabelText('Name'), 'Summer 2027');
    await user.type(sheet.getByLabelText('First day'), '2027-08-27');
    await user.type(sheet.getByLabelText('Last day'), '2027-07-19');
    await user.click(sheet.getByRole('button', { name: 'Add term' }));

    expect(sheet.getByLabelText('Last day')).toHaveAccessibleDescription(
      /The last day cannot be before the first day\.$/,
    );
    expect(sheet.getByLabelText('First day')).not.toHaveAttribute('aria-invalid', 'true');
    expect(post).not.toHaveBeenCalled();
  });

  it('renames and shortens a term', async () => {
    mockApiGet(asOffice);
    const put = mockApiPut({
      '/api/v1/terms/{termId}': ({ body }) => ({
        body: { id: ids.termSpring, ...(body as object) },
      }),
    });
    const user = userEvent.setup();

    renderPage();
    await user.click((await row('Spring 2027')).getByRole('button', { name: 'Edit Spring 2027' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Edit Spring 2027' }));
    expect(sheet.getByLabelText('Name')).toHaveValue('Spring 2027');
    expect(sheet.getByLabelText('First day')).toHaveValue('2027-02-01');
    expect(sheet.getByLabelText('Last day')).toHaveValue('2027-07-16');
    await retype(user, sheet, 'Name', 'Spring Term 2027');
    await retype(user, sheet, 'Last day', '2027-06-30');
    await user.click(sheet.getByRole('button', { name: 'Save changes' }));

    expect(await screen.findByText('Spring Term 2027 has been saved.')).toBeInTheDocument();
    expect(put).toHaveBeenCalledWith('/api/v1/terms/{termId}', {
      params: { path: { termId: ids.termSpring } },
      body: { name: 'Spring Term 2027', startsOn: '2027-02-01', endsOn: '2027-06-30' },
    });
  });

  it('marks the date that would leave out a lesson that has to stay', async () => {
    mockApiGet(asOffice);
    mockApiPut({
      '/api/v1/terms/{termId}': problem(
        422,
        'the lesson of 2027-01-25 would lie outside the term, but it has bookings',
        [{ location: 'body.endsOn' }],
      ),
    });
    const user = userEvent.setup();

    renderPage();
    await user.click((await row('Autumn 2026')).getByRole('button', { name: 'Edit Autumn 2026' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Edit Autumn 2026' }));
    await retype(user, sheet, 'Last day', '2026-12-18');
    await user.click(sheet.getByRole('button', { name: 'Save changes' }));

    expect(await sheet.findByRole('alert')).toHaveTextContent('Please check your entries.');
    expect(sheet.getByLabelText('Last day')).toHaveAttribute('aria-invalid', 'true');
    expect(sheet.getByLabelText('Last day')).toHaveAccessibleDescription(
      /A lesson outside these dates has started, has bookings or was changed one by one, so the term cannot leave it out\.$/,
    );
    expect(sheet.getByLabelText('First day')).not.toHaveAttribute('aria-invalid', 'true');
    expect(sheet.getByLabelText('Name')).not.toHaveAttribute('aria-invalid', 'true');
    expect(screen.queryByText(/would lie outside the term/)).not.toBeInTheDocument();
  });

  it('deletes a term without classes', async () => {
    let deleted = false;
    mockApiGet({
      ...asOffice,
      '/api/v1/terms': () => ({ body: { items: deleted ? terms.slice(0, 2) : terms } }),
    });
    const del = mockApiDelete({
      '/api/v1/terms/{termId}': () => {
        deleted = true;
        return { status: 204 };
      },
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(
      (await row('Spring 2027')).getByRole('button', { name: 'Delete Spring 2027' }),
    );
    const dialog = within(await screen.findByRole('alertdialog', { name: 'Delete Spring 2027?' }));
    await user.click(dialog.getByRole('button', { name: 'Delete' }));

    expect(await screen.findByText('Spring 2027 has been deleted.')).toBeInTheDocument();
    expect(del).toHaveBeenCalledWith('/api/v1/terms/{termId}', {
      params: { path: { termId: ids.termSpring } },
    });
    await waitFor(() =>
      expect(
        within(screen.getByRole('table', { name: 'Terms' })).queryByText('Spring 2027'),
      ).not.toBeInTheDocument(),
    );
  });

  it('says why a term with classes cannot be deleted', async () => {
    mockApiGet(asOffice);
    mockApiDelete({ '/api/v1/terms/{termId}': conflict('in-use', 'term has classes') });
    const user = userEvent.setup();

    renderPage();
    await user.click(
      (await row('Autumn 2026')).getByRole('button', { name: 'Delete Autumn 2026' }),
    );
    const dialog = within(await screen.findByRole('alertdialog', { name: 'Delete Autumn 2026?' }));
    await user.click(dialog.getByRole('button', { name: 'Delete' }));

    expect(await dialog.findByRole('alert')).toHaveTextContent(
      'Autumn 2026 still has classes, so it cannot be deleted. Delete its classes first, or let the term end on its last day.',
    );
    expect(screen.getByRole('alertdialog')).toBeInTheDocument();
    expect(screen.queryByText('term has classes')).not.toBeInTheDocument();
  });

  it('says when there are no terms', async () => {
    mockApiGet({ ...asOffice, '/api/v1/terms': { body: { items: [] } } });

    renderPage();

    expect(await screen.findByText('There are no terms yet.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Add term' })).toBeInTheDocument();
  });

  it('says when the terms could not be loaded, and loads them again', async () => {
    let fail = true;
    mockApiGet({
      ...asOffice,
      '/api/v1/terms': () => (fail ? problem(500, 'boom') : { body: { items: terms } }),
    });
    const user = userEvent.setup();

    renderPage();
    expect(await screen.findByText('Could not load the data.')).toBeInTheDocument();
    fail = false;
    await user.click(screen.getByRole('button', { name: 'Retry' }));

    expect(await row('Autumn 2026')).toBeTruthy();
  });

  it('renders in German', async () => {
    mockApiGet(asOffice);
    await i18n.changeLanguage('de');
    const user = userEvent.setup();

    renderPage();

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Kurszeiträume' }),
    ).toBeInTheDocument();
    expect(await screen.findByRole('link', { name: 'Kurse von Autumn 2026' })).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Kurszeitraum hinzufügen' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Kurszeitraum hinzufügen' }));
    expect(sheet.getByLabelText('Erster Tag')).toBeInTheDocument();
    expect(sheet.getByLabelText('Letzter Tag')).toBeInTheDocument();
  });
});
