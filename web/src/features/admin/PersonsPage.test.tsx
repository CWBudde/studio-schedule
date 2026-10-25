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
import { personsSearchFrom, personsSearchState } from './personsSearch';

afterEach(() => {
  vi.restoreAllMocks();
});

/** The words of a person search's body. */
const queryOf = (init: MockRequest) => (init.body as { query?: string } | undefined)?.query ?? '';

/** The persons whose name or email contains every word of the request's `query`. */
function searchPersons(init: MockRequest) {
  const words = queryOf(init).toLowerCase().split(/\s+/).filter(Boolean);
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
};

/** Signs in as the office; `search` answers the person search. */
function mockOffice(search: Parameters<typeof mockApiPost>[0][string] = searchPersons) {
  const post = mockApiPost({ '/api/v1/admin/persons/search': search });
  return { get: mockApiGet(asOffice), post };
}

function renderPage(search: Record<string, string> = {}) {
  render(<Toaster />);
  return renderInRouter(<PersonsPage />, { at: '/admin/persons', search });
}

/** The search the router's current history entry keeps. */
const searchOf = (router: { state: { location: { state: object } } }) =>
  personsSearchFrom(router.state.location.state);

/** The names in the persons table, in display order. */
const names = () =>
  within(screen.getByRole('table', { name: 'Persons' }))
    .getAllByRole('row')
    .slice(1)
    .map((tr) => within(tr).getAllByRole('cell')[0]?.textContent);

describe('PersonsPage', () => {
  it('lists the persons in the order of the API, each a link to their page', async () => {
    mockOffice();

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
    const { post } = mockOffice();
    const user = userEvent.setup();

    renderPage();
    await screen.findByRole('table', { name: 'Persons' });
    await user.type(screen.getByRole('searchbox', { name: 'Search persons' }), 'sample');

    await waitFor(() => expect(names()).toEqual(['Mia Sample', 'Noah Sample']));
    expect(post).toHaveBeenCalledWith('/api/v1/admin/persons/search', {
      body: { query: 'sample' },
      signal: expect.any(AbortSignal),
    });
  });

  it('keeps the search in the history state, never in the URL', async () => {
    mockOffice();
    const user = userEvent.setup();

    const { router } = renderPage();
    await screen.findByRole('table', { name: 'Persons' });
    await user.type(screen.getByRole('searchbox', { name: 'Search persons' }), 'mia');

    await waitFor(() => expect(searchOf(router)).toBe('mia'));
    await waitFor(() => expect(names()).toEqual(['Mia Sample']));
    expect(router.state.location.search).toEqual({});
    expect(router.state.location.href).toBe('/admin/persons');
    await user.clear(screen.getByRole('searchbox', { name: 'Search persons' }));
    await waitFor(() => expect(searchOf(router)).toBe(''));
    expect(router.state.location.href).toBe('/admin/persons');
  });

  it('restores the search when going back from a person', async () => {
    mockOffice();
    const user = userEvent.setup();

    const { router } = renderPage();
    await screen.findByRole('table', { name: 'Persons' });
    await user.type(screen.getByRole('searchbox', { name: 'Search persons' }), 'noah');
    await waitFor(() => expect(searchOf(router)).toBe('noah'));
    await router.navigate({
      to: '/admin/persons/$personId',
      params: { personId: persons[2]!.id },
    });
    await waitFor(() =>
      expect(screen.queryByRole('searchbox', { name: 'Search persons' })).not.toBeInTheDocument(),
    );

    router.history.back();

    expect(await screen.findByRole('searchbox', { name: 'Search persons' })).toHaveValue('noah');
    await waitFor(() => expect(names()).toEqual(['Noah Sample']));
  });

  it('ignores a search in the URL', async () => {
    const { post } = mockOffice();

    renderPage({ q: 'noah' });

    await screen.findByRole('table', { name: 'Persons' });
    expect(screen.getByRole('searchbox', { name: 'Search persons' })).toHaveValue('');
    expect(names()).toEqual(['Alex Example', 'Mia Sample', 'Noah Sample']);
    expect(post).not.toHaveBeenCalledWith(
      '/api/v1/admin/persons/search',
      expect.objectContaining({ body: { query: 'noah' } }),
    );
  });

  it('keeps typing while its own search lands in the URL', async () => {
    mockOffice();

    const { router } = renderPage();
    const box = await screen.findByRole('searchbox', { name: 'Search persons' });
    const navigate = router.navigate.bind(router);
    // The next letter comes while the search "mi" is on its way into the state.
    vi.spyOn(router, 'navigate').mockImplementationOnce((options) => {
      fireEvent.change(box, { target: { value: 'mia' } });
      return navigate(options);
    });
    fireEvent.change(box, { target: { value: 'mi' } });

    // Once "mi" has landed, the box still says "mia" …
    await waitFor(() => expect(searchOf(router)).toBe('mi'));
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 20));
    });
    expect(box).toHaveValue('mia');
    // … and its search follows.
    await waitFor(() => expect(searchOf(router)).toBe('mia'));
    expect(box).toHaveValue('mia');
  });

  it('follows the history state when it changes elsewhere', async () => {
    mockOffice();

    const { router } = renderPage();
    const box = await screen.findByRole('searchbox', { name: 'Search persons' });
    await router.navigate({
      to: '/admin/persons',
      state: { personsSearch: personsSearchState('noah') },
    });
    await waitFor(() => expect(box).toHaveValue('noah'));
    await router.navigate({
      to: '/admin/persons',
      state: { personsSearch: personsSearchState('mia') },
    });

    await waitFor(() => expect(box).toHaveValue('mia'));
    await waitFor(() => expect(names()).toEqual(['Mia Sample']));
    // The navigation's link carries no state: the search is gone.
    await router.navigate({ to: '/admin/persons' });
    await waitFor(() => expect(box).toHaveValue(''));
  });

  it('shows a failed search as failed, not the rows of the last one', async () => {
    mockOffice((init) => (queryOf(init) === 'boom' ? problem(503, 'down') : searchPersons(init)));
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
    mockOffice();
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
    mockOffice({ body: { items: many } });

    renderPage();

    expect(
      await screen.findByText('Showing the first 50 persons. Type more to narrow the list.'),
    ).toBeInTheDocument();
  });

  it('says when the persons could not be loaded and asks again', async () => {
    let fail = true;
    mockOffice((init) => (fail ? problem(503, 'down') : searchPersons(init)));
    const user = userEvent.setup();

    renderPage();
    expect(await screen.findByText('Could not load the data.')).toBeInTheDocument();
    fail = false;
    await user.click(screen.getByRole('button', { name: 'Retry' }));

    expect(await screen.findByRole('table', { name: 'Persons' })).toBeInTheDocument();
  });

  it('adds a person in a sheet and opens their page', async () => {
    mockOffice();
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
    mockOffice();
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
    expect(post).not.toHaveBeenCalledWith('/api/v1/admin/persons', expect.anything());
  });

  it.each([
    ['birth date must not be in the future', 'The birth date cannot be in the future.'],
    ['invalid birth date', 'Please check this entry.'],
  ])('explains a refused birth date (%s)', async (message, shown) => {
    mockOffice();
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
    mockOffice();
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
