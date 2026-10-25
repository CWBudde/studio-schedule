import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Toaster } from '@/components/ui/sonner';
import i18n from '@/i18n';
import {
  conflict,
  meOffice,
  mockApiDelete,
  mockApiGet,
  mockApiPost,
  mockApiPut,
  passTypes,
  problem,
  publicSettings,
} from '@/test/mockApi';
import { renderWithQuery } from '@/test/utils';
import { PassTypesPage } from './PassTypesPage';

afterEach(() => {
  vi.restoreAllMocks();
});

const asOffice = {
  '/api/v1/auth/me': { body: meOffice },
  '/api/v1/settings/public': { body: publicSettings },
  '/api/v1/admin/pass-types': { body: { items: passTypes } },
};

function renderPage() {
  render(<Toaster />);
  return renderWithQuery(<PassTypesPage />);
}

/** The body row of the pass types table whose first cell is `name`. */
async function row(name: string, tableName = 'Pass types') {
  const table = within(await screen.findByRole('table', { name: tableName }));
  const cell = await table.findByRole('cell', { name });
  const tr = cell.closest('tr');
  if (!tr) throw new Error(`no row ${name}`);
  return within(tr);
}

/** Opens the add sheet and returns it. */
async function openAdd(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole('button', { name: 'Add pass type' }));
  return within(await screen.findByRole('dialog', { name: 'Add a pass type' }));
}

describe('PassTypesPage', () => {
  it('lists the pass types with their terms, price, VAT and status', async () => {
    mockApiGet(asOffice);

    renderPage();

    expect(await screen.findByRole('heading', { level: 1, name: 'Passes' })).toBeInTheDocument();
    const tenVisits = await row('10-visit pass');
    expect(tenVisits.getByText('10 visits, valid 180 days')).toBeInTheDocument();
    expect(tenVisits.getByText('€150.00')).toBeInTheDocument();
    expect(tenVisits.getByText('19%')).toBeInTheDocument();
    expect(tenVisits.getByText('On sale')).toBeInTheDocument();
    expect(
      (await row('30-day pass')).getByText('Unlimited visits for 30 days'),
    ).toBeInTheDocument();
    const single = await row('Single visit');
    expect(single.getByText('1 visit, no expiry')).toBeInTheDocument();
    expect(single.getByText('7%')).toBeInTheDocument();
    expect(single.getByText('Retired')).toBeInTheDocument();
  });

  it('adds a visit pass with the price and VAT rate in minor units and basis points', async () => {
    mockApiGet(asOffice);
    const post = mockApiPost({
      '/api/v1/admin/pass-types': ({ body }) => ({
        status: 201,
        body: { id: 'new', createdAt: '2026-10-23T10:00:00Z', ...(body as object) },
      }),
    });
    const user = userEvent.setup();

    renderPage();
    const sheet = await openAdd(user);
    await user.type(sheet.getByLabelText('Name'), ' 5-visit pass ');
    await user.clear(sheet.getByLabelText('Visits'));
    await user.type(sheet.getByLabelText('Visits'), '5');
    await user.clear(sheet.getByLabelText('Valid for (days)'));
    await user.type(sheet.getByLabelText('Valid for (days)'), '0');
    await user.type(sheet.getByLabelText('Price (EUR)'), '79,90');
    await user.type(sheet.getByLabelText('VAT rate (%)'), '7.5');
    await user.click(sheet.getByRole('button', { name: 'Add pass type' }));

    expect(await screen.findByText('5-visit pass has been added.')).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/admin/pass-types', {
      body: {
        name: '5-visit pass',
        kind: 'visits',
        visits: 5,
        validityDays: 0,
        price: 7990,
        vatRate: 750,
        active: true,
      },
    });
  });

  it('adds a time pass without visits', async () => {
    mockApiGet(asOffice);
    const post = mockApiPost({
      '/api/v1/admin/pass-types': ({ body }) => ({
        status: 201,
        body: { id: 'new', ...(body as object) },
      }),
    });
    const user = userEvent.setup();

    renderPage();
    const sheet = await openAdd(user);
    await user.type(sheet.getByLabelText('Name'), 'Summer month');
    await user.click(sheet.getByRole('combobox', { name: 'Kind' }));
    await user.click(await screen.findByRole('option', { name: 'Time pass' }));
    expect(sheet.queryByLabelText('Visits')).not.toBeInTheDocument();
    await user.clear(sheet.getByLabelText('Duration (days)'));
    await user.type(sheet.getByLabelText('Duration (days)'), '31');
    await user.type(sheet.getByLabelText('Price (EUR)'), '95');
    await user.type(sheet.getByLabelText('VAT rate (%)'), '0');
    await user.click(sheet.getByRole('button', { name: 'Add pass type' }));

    expect(await screen.findByText('Summer month has been added.')).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/admin/pass-types', {
      body: {
        name: 'Summer month',
        kind: 'time',
        validityDays: 31,
        price: 9500,
        vatRate: 0,
        active: true,
      },
    });
  });

  it('checks visits, days, price and VAT rate before sending them', async () => {
    mockApiGet(asOffice);
    const post = mockApiPost({});
    const user = userEvent.setup();

    renderPage();
    const sheet = await openAdd(user);
    await user.type(sheet.getByLabelText('Name'), 'Broken');
    await user.clear(sheet.getByLabelText('Visits'));
    await user.type(sheet.getByLabelText('Visits'), '0');
    await user.clear(sheet.getByLabelText('Valid for (days)'));
    await user.type(sheet.getByLabelText('Valid for (days)'), '4000');
    await user.type(sheet.getByLabelText('Price (EUR)'), '12.345');
    await user.type(sheet.getByLabelText('VAT rate (%)'), '101');
    await user.click(sheet.getByRole('button', { name: 'Add pass type' }));

    expect(
      await sheet.findByText('Enter a whole number of visits from 1 to 1000.'),
    ).toBeInTheDocument();
    expect(sheet.getByLabelText('Visits')).toHaveAttribute('aria-invalid', 'true');
    expect(sheet.getByLabelText('Valid for (days)')).toHaveAccessibleDescription(
      expect.stringContaining('Enter a whole number of days from 0 (no expiry) to 3660.'),
    );
    expect(sheet.getByLabelText('Price (EUR)')).toHaveAccessibleDescription(
      expect.stringContaining('Enter an amount such as 12.50.'),
    );
    expect(sheet.getByLabelText('VAT rate (%)')).toHaveAccessibleDescription(
      expect.stringContaining('Enter a percentage from 0 to 100, such as 19 or 7.5.'),
    );
    expect(post).not.toHaveBeenCalled();
  });

  it('wants a time pass to last at least a day', async () => {
    mockApiGet(asOffice);
    const post = mockApiPost({});
    const user = userEvent.setup();

    renderPage();
    const sheet = await openAdd(user);
    await user.type(sheet.getByLabelText('Name'), 'Zero');
    await user.click(sheet.getByRole('combobox', { name: 'Kind' }));
    await user.click(await screen.findByRole('option', { name: 'Time pass' }));
    await user.clear(sheet.getByLabelText('Duration (days)'));
    await user.type(sheet.getByLabelText('Duration (days)'), '0');
    await user.type(sheet.getByLabelText('Price (EUR)'), '10');
    await user.type(sheet.getByLabelText('VAT rate (%)'), '19');
    await user.click(sheet.getByRole('button', { name: 'Add pass type' }));

    expect(
      await sheet.findByText('Enter a whole number of days from 1 to 3660.'),
    ).toBeInTheDocument();
    expect(post).not.toHaveBeenCalled();
  });

  it('edits a pass type with its amounts filled in', async () => {
    mockApiGet(asOffice);
    const put = mockApiPut({
      '/api/v1/admin/pass-types/{passTypeId}': ({ body }) => ({
        body: { ...passTypes[0], ...(body as object) },
      }),
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(
      (await row('10-visit pass')).getByRole('button', { name: 'Edit 10-visit pass' }),
    );
    const sheet = within(await screen.findByRole('dialog', { name: 'Edit 10-visit pass' }));
    expect(sheet.getByLabelText('Price (EUR)')).toHaveValue('150.00');
    expect(sheet.getByLabelText('VAT rate (%)')).toHaveValue('19');
    expect(sheet.getByLabelText('Visits')).toHaveValue(10);
    await user.clear(sheet.getByLabelText('Price (EUR)'));
    await user.type(sheet.getByLabelText('Price (EUR)'), '160');
    await user.click(sheet.getByRole('switch', { name: 'On sale' }));
    await user.click(sheet.getByRole('button', { name: 'Save changes' }));

    expect(await screen.findByText('10-visit pass has been saved.')).toBeInTheDocument();
    expect(put).toHaveBeenCalledWith('/api/v1/admin/pass-types/{passTypeId}', {
      params: { path: { passTypeId: passTypes[0]!.id } },
      body: {
        name: '10-visit pass',
        kind: 'visits',
        visits: 10,
        validityDays: 180,
        price: 16000,
        vatRate: 1900,
        active: false,
      },
    });
  });

  it('says when the name is taken', async () => {
    mockApiGet(asOffice);
    mockApiPost({ '/api/v1/admin/pass-types': conflict('duplicate') });
    const user = userEvent.setup();

    renderPage();
    const sheet = await openAdd(user);
    await user.type(sheet.getByLabelText('Name'), '30-DAY PASS');
    await user.type(sheet.getByLabelText('Price (EUR)'), '1');
    await user.type(sheet.getByLabelText('VAT rate (%)'), '19');
    await user.click(sheet.getByRole('button', { name: 'Add pass type' }));

    expect(await sheet.findByRole('alert')).toHaveTextContent(
      'Another pass type is already called “30-DAY PASS”.',
    );
    expect(sheet.getByLabelText('Name')).toHaveAttribute('aria-invalid', 'true');
  });

  it('retires a sold pass type that cannot be deleted', async () => {
    let retired = false;
    mockApiGet({
      ...asOffice,
      '/api/v1/admin/pass-types': () => ({
        body: {
          items: passTypes.map((p, i) => (i === 0 && retired ? { ...p, active: false } : p)),
        },
      }),
    });
    mockApiDelete({ '/api/v1/admin/pass-types/{passTypeId}': conflict('in-use') });
    const post = mockApiPost({
      '/api/v1/admin/pass-types/{passTypeId}/retire': () => {
        retired = true;
        return { body: { ...passTypes[0], active: false } };
      },
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(
      (await row('10-visit pass')).getByRole('button', { name: 'Delete 10-visit pass' }),
    );
    const dialog = within(
      await screen.findByRole('alertdialog', { name: 'Delete 10-visit pass?' }),
    );
    await user.click(dialog.getByRole('button', { name: 'Delete' }));
    expect(await dialog.findByRole('alert')).toHaveTextContent(
      '10-visit pass has been sold, so it cannot be deleted. Retire it instead',
    );
    await user.click(dialog.getByRole('button', { name: 'Retire instead' }));

    expect(await screen.findByText('10-visit pass has been retired.')).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/admin/pass-types/{passTypeId}/retire', {
      params: { path: { passTypeId: passTypes[0]!.id } },
    });
    await waitFor(async () =>
      expect((await row('10-visit pass')).getByText('Retired')).toBeInTheDocument(),
    );
  });

  it('says when the currency cannot be loaded and tries again', async () => {
    let failing = true;
    mockApiGet({
      ...asOffice,
      '/api/v1/settings/public': () => (failing ? problem(500, 'boom') : { body: publicSettings }),
    });
    const user = userEvent.setup();

    renderPage();
    expect(await screen.findByText('Could not load the data.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Add pass type' })).toBeDisabled();

    failing = false;
    await user.click(screen.getByRole('button', { name: 'Retry' }));
    expect((await row('10-visit pass')).getByText('€150.00')).toBeInTheDocument();
  });

  it('renders in German with amounts in German', async () => {
    mockApiGet(asOffice);
    await i18n.changeLanguage('de');

    renderPage();

    expect(await screen.findByRole('heading', { level: 1, name: 'Karten' })).toBeInTheDocument();
    const tenVisits = await row('10-visit pass', 'Kartenarten');
    expect(tenVisits.getByText(/^150,00\s€$/)).toBeInTheDocument();
    expect(tenVisits.getByText('10 Besuche, 180 Tage gültig')).toBeInTheDocument();
  });
});
