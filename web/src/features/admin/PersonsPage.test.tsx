import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Toaster } from '@/components/ui/sonner';
import {
  meOffice,
  mockApiGet,
  mockApiPost,
  type MockRequest,
  persons,
  problem,
  publicSettings,
} from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { PersonsPage } from './PersonsPage';

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

const asOffice = {
  '/api/v1/settings/public': { body: publicSettings },
  '/api/v1/auth/me': { body: meOffice },
  '/api/v1/admin/persons': searchPersons,
};

function renderPage(search: Record<string, string> = {}) {
  render(<Toaster />);
  return renderInRouter(<PersonsPage />, { at: '/admin/persons', search });
}

/** The names in the persons table, in display order. */
const names = () =>
  within(screen.getByRole('table', { name: 'Persons' }))
    .getAllByRole('row')
    .slice(1)
    .map((tr) => within(tr).getAllByRole('cell')[0]?.textContent);

describe('PersonsPage', () => {
  it('lists the persons in the order of the API, each a link to their page', async () => {
    mockApiGet(asOffice);

    renderPage();

    expect(await screen.findByRole('heading', { level: 1, name: 'Persons' })).toBeInTheDocument();
    await screen.findByRole('table', { name: 'Persons' });
    expect(names()).toEqual(['Alex Example', 'Mia Sample', 'Noah Sample']);
    expect(screen.getByRole('link', { name: 'Mia Sample' })).toHaveAttribute(
      'href',
      `/admin/persons/${persons[1]!.id}`,
    );
    expect(screen.getByText('mia.sample@example.org')).toBeInTheDocument();
    // The API searches; the table has no search of its own.
    expect(screen.getAllByRole('searchbox')).toHaveLength(1);
  });

  it('asks the API once the typing pauses', async () => {
    const get = mockApiGet(asOffice);
    const user = userEvent.setup();

    renderPage();
    await screen.findByRole('table', { name: 'Persons' });
    await user.type(screen.getByRole('searchbox', { name: 'Search persons' }), 'sample');

    await waitFor(() => expect(names()).toEqual(['Mia Sample', 'Noah Sample']));
    expect(get).toHaveBeenCalledWith('/api/v1/admin/persons', {
      params: { query: { q: 'sample' } },
      signal: expect.any(AbortSignal),
    });
  });

  it('keeps the search in the URL', async () => {
    mockApiGet(asOffice);
    const user = userEvent.setup();

    const { router } = renderPage();
    await screen.findByRole('table', { name: 'Persons' });
    await user.type(screen.getByRole('searchbox', { name: 'Search persons' }), 'mia');

    await waitFor(() => expect(router.state.location.search).toEqual({ q: 'mia' }));
    await waitFor(() => expect(names()).toEqual(['Mia Sample']));
    await user.clear(screen.getByRole('searchbox', { name: 'Search persons' }));
    await waitFor(() => expect(router.state.location.search).toEqual({}));
  });

  it('starts with the search of the URL', async () => {
    mockApiGet(asOffice);

    renderPage({ q: 'noah' });

    expect(await screen.findByRole('searchbox', { name: 'Search persons' })).toHaveValue('noah');
    await waitFor(() => expect(names()).toEqual(['Noah Sample']));
  });

  it('keeps typing while its own search lands in the URL', async () => {
    mockApiGet(asOffice);

    const { router } = renderPage();
    const box = await screen.findByRole('searchbox', { name: 'Search persons' });
    const navigate = router.navigate.bind(router);
    // The next letter comes while the search "mi" is on its way into the URL.
    vi.spyOn(router, 'navigate').mockImplementationOnce((options) => {
      fireEvent.change(box, { target: { value: 'mia' } });
      return navigate(options);
    });
    fireEvent.change(box, { target: { value: 'mi' } });

    // Once "mi" has landed, the box still says "mia" …
    await waitFor(() => expect(router.state.location.search).toEqual({ q: 'mi' }));
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 20));
    });
    expect(box).toHaveValue('mia');
    // … and its search follows.
    await waitFor(() => expect(router.state.location.search).toEqual({ q: 'mia' }));
    expect(box).toHaveValue('mia');
  });

  it('follows the URL when it changes elsewhere', async () => {
    mockApiGet(asOffice);

    const { router } = renderPage({ q: 'noah' });
    const box = await screen.findByRole('searchbox', { name: 'Search persons' });
    expect(box).toHaveValue('noah');
    await router.navigate({ to: '/admin/persons', search: { q: 'mia' } });

    await waitFor(() => expect(box).toHaveValue('mia'));
    await waitFor(() => expect(names()).toEqual(['Mia Sample']));
  });

  it('shows a failed search as failed, not the rows of the last one', async () => {
    mockApiGet({
      ...asOffice,
      '/api/v1/admin/persons': (init) =>
        init.params?.query?.q === 'boom' ? problem(503, 'down') : searchPersons(init),
    });
    const user = userEvent.setup();

    renderPage();
    await screen.findByRole('table', { name: 'Persons' });
    const box = screen.getByRole('searchbox', { name: 'Search persons' });
    expect(box).toHaveAttribute('maxLength', '100');
    await user.type(box, 'boom');

    expect(await screen.findByText('Could not load the data.')).toBeInTheDocument();
    expect(screen.queryByRole('table', { name: 'Persons' })).not.toBeInTheDocument();
  });

  it('says when nobody matches', async () => {
    mockApiGet(asOffice);
    const user = userEvent.setup();

    renderPage();
    await screen.findByRole('table', { name: 'Persons' });
    await user.type(screen.getByRole('searchbox', { name: 'Search persons' }), 'zz');

    expect(await screen.findByText('Nobody matches “zz”.')).toBeInTheDocument();
  });

  it('says that only the first matches are shown at the limit', async () => {
    const many = Array.from({ length: 50 }, (_, i) => ({
      id: `p-${i}`,
      givenName: `Person${i}`,
      familyName: 'Many',
    }));
    mockApiGet({ ...asOffice, '/api/v1/admin/persons': { body: { items: many } } });

    renderPage();

    expect(
      await screen.findByText('Showing the first 50 persons. Type more to narrow the list.'),
    ).toBeInTheDocument();
  });

  it('says when the persons could not be loaded and asks again', async () => {
    let fail = true;
    mockApiGet({
      ...asOffice,
      '/api/v1/admin/persons': (init) => (fail ? problem(503, 'down') : searchPersons(init)),
    });
    const user = userEvent.setup();

    renderPage();
    expect(await screen.findByText('Could not load the data.')).toBeInTheDocument();
    fail = false;
    await user.click(screen.getByRole('button', { name: 'Retry' }));

    expect(await screen.findByRole('table', { name: 'Persons' })).toBeInTheDocument();
  });

  it('adds a person in a sheet and opens their page', async () => {
    mockApiGet(asOffice);
    const post = mockApiPost({
      '/api/v1/admin/persons': ({ body }) => ({
        status: 201,
        body: { id: 'person-new', ...(body as object) },
      }),
    });
    const user = userEvent.setup();

    const { router } = renderPage();
    await user.click(await screen.findByRole('button', { name: 'Add person' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Add a person' }));
    await user.type(sheet.getByLabelText('First name'), ' Paul ');
    await user.type(sheet.getByLabelText('Last name'), 'Sample');
    await user.type(sheet.getByLabelText('Email (optional)'), 'paul.sample@example.org');
    await user.type(sheet.getByLabelText('Phone (optional)'), '+49 000 0000301');
    await user.type(sheet.getByLabelText('Notes (optional)'), 'Second parent of Noah.');
    await user.click(sheet.getByRole('button', { name: 'Add person' }));

    expect(await screen.findByText('Paul Sample has been added.')).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/admin/persons', {
      body: {
        givenName: 'Paul',
        familyName: 'Sample',
        email: 'paul.sample@example.org',
        phone: '+49 000 0000301',
        notes: 'Second parent of Noah.',
      },
    });
    await waitFor(() => expect(router.state.location.pathname).toBe('/admin/persons/person-new'));
  });

  it('checks the fields before sending', async () => {
    mockApiGet(asOffice);
    const post = mockApiPost({});
    const user = userEvent.setup();

    renderPage();
    await user.click(await screen.findByRole('button', { name: 'Add person' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Add a person' }));
    await user.type(sheet.getByLabelText('Email (optional)'), 'not-an-email');
    await user.type(sheet.getByLabelText('Birth date (optional)'), '2999-01-01');
    await user.click(sheet.getByRole('button', { name: 'Add person' }));

    expect(await sheet.findAllByText('Please fill in this field.')).toHaveLength(2);
    expect(sheet.getByText('Please enter a valid email address.')).toBeInTheDocument();
    expect(sheet.getByText('The birth date cannot be in the future.')).toBeInTheDocument();
    expect(post).not.toHaveBeenCalled();
  });

  it.each([
    ['birth date must not be in the future', 'The birth date cannot be in the future.'],
    ['invalid birth date', 'Please check this entry.'],
  ])('explains a refused birth date (%s)', async (message, shown) => {
    mockApiGet(asOffice);
    mockApiPost({
      '/api/v1/admin/persons': problem(422, 'validation failed', [
        { location: 'body.birthDate', message },
      ]),
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(await screen.findByRole('button', { name: 'Add person' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Add a person' }));
    await user.type(sheet.getByLabelText('First name'), 'Paul');
    await user.type(sheet.getByLabelText('Last name'), 'Sample');
    await user.type(sheet.getByLabelText('Birth date (optional)'), '2020-01-01');
    await user.click(sheet.getByRole('button', { name: 'Add person' }));

    expect(await sheet.findByText(shown)).toBeInTheDocument();
    expect(sheet.getByLabelText('Birth date (optional)')).toHaveAttribute('aria-invalid', 'true');
  });

  it('marks the fields the API refuses', async () => {
    mockApiGet(asOffice);
    mockApiPost({
      '/api/v1/admin/persons': problem(422, 'validation failed', [{ location: 'body.email' }]),
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(await screen.findByRole('button', { name: 'Add person' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Add a person' }));
    await user.type(sheet.getByLabelText('First name'), 'Paul');
    await user.type(sheet.getByLabelText('Last name'), 'Sample');
    await user.type(sheet.getByLabelText('Email (optional)'), 'paul@example.org');
    await user.click(sheet.getByRole('button', { name: 'Add person' }));

    expect(await sheet.findByText('Please check your entries.')).toBeInTheDocument();
    expect(sheet.getByLabelText('Email (optional)')).toHaveAttribute('aria-invalid', 'true');
    expect(sheet.getByLabelText('First name')).not.toHaveAttribute('aria-invalid');
  });
});
