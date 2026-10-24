import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import i18n from '@/i18n';
import { ids, meOffice, mockApiGet, problem, terms } from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { TermsPage } from './TermsPage';

afterEach(() => {
  vi.restoreAllMocks();
});

const asOffice = {
  '/api/v1/auth/me': { body: meOffice },
  '/api/v1/terms': { body: { items: terms } },
};

/** The body row of the terms table whose first cell is `name`. */
async function row(name: string) {
  const table = within(await screen.findByRole('table', { name: 'Terms' }));
  const tr = (await table.findByRole('cell', { name })).closest('tr');
  if (!tr) throw new Error(`no row ${name}`);
  return within(tr);
}

describe('TermsPage', () => {
  it('lists the terms with their dates and links to their classes', async () => {
    mockApiGet(asOffice);

    renderInRouter(<TermsPage />, { at: '/admin/terms' });

    expect(await screen.findByRole('heading', { level: 1, name: 'Terms' })).toBeInTheDocument();
    const autumn = await row('Autumn 2026');
    expect(autumn.getByText('Aug 31, 2026 – Jan 29, 2027')).toBeInTheDocument();
    expect(autumn.getByRole('link', { name: 'Classes of Autumn 2026' })).toHaveAttribute(
      'href',
      `/admin/terms/${ids.termAutumn}/classes`,
    );
    expect((await row('Spring 2027')).getByText('Feb 1 – Jul 16, 2027')).toBeInTheDocument();
    // Terms are only listed here; they are added and changed elsewhere.
    expect(screen.queryByRole('button', { name: /^Add/ })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /^(Edit|Delete) / })).not.toBeInTheDocument();
  });

  it('opens the classes of a term', async () => {
    mockApiGet(asOffice);
    const user = userEvent.setup();

    const { router } = renderInRouter(<TermsPage />, { at: '/admin/terms' });
    await user.click(await screen.findByRole('link', { name: 'Classes of Spring 2027' }));

    await vi.waitFor(() =>
      expect(router.state.location.pathname).toBe(`/admin/terms/${ids.termSpring}/classes`),
    );
  });

  it('says when there are no terms', async () => {
    mockApiGet({ ...asOffice, '/api/v1/terms': { body: { items: [] } } });

    renderInRouter(<TermsPage />, { at: '/admin/terms' });

    expect(await screen.findByText('There are no terms yet.')).toBeInTheDocument();
  });

  it('says when the terms could not be loaded, and loads them again', async () => {
    let fail = true;
    mockApiGet({
      ...asOffice,
      '/api/v1/terms': () => (fail ? problem(500, 'boom') : { body: { items: terms } }),
    });
    const user = userEvent.setup();

    renderInRouter(<TermsPage />, { at: '/admin/terms' });
    expect(await screen.findByText('Could not load the data.')).toBeInTheDocument();
    fail = false;
    await user.click(screen.getByRole('button', { name: 'Retry' }));

    expect(await row('Autumn 2026')).toBeTruthy();
  });

  it('renders in German', async () => {
    mockApiGet(asOffice);
    await i18n.changeLanguage('de');

    renderInRouter(<TermsPage />, { at: '/admin/terms' });

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Kurszeiträume' }),
    ).toBeInTheDocument();
    expect(await screen.findByRole('link', { name: 'Kurse von Autumn 2026' })).toBeInTheDocument();
  });
});
