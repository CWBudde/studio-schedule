import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Toaster } from '@/components/ui/sonner';
import {
  conflict,
  ids,
  meAdmin,
  meOffice,
  mockApiGet,
  mockApiPost,
  type MockRequest,
  noahPasses,
  passTypes,
  personDetails,
  persons,
  problem,
  publicSettings,
  receiptSettings,
} from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { PersonPage } from './PersonPage';

afterEach(() => {
  vi.restoreAllMocks();
});

/** The persons whose name contains every word of the request's `q`. */
function searchPersons(init: MockRequest) {
  const words = String(init.params?.query?.q ?? '')
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean);
  return {
    body: {
      items: persons.filter((p) =>
        words.every((w) => `${p.givenName} ${p.familyName}`.toLowerCase().includes(w)),
      ),
    },
  };
}

const fixtures = (passes = noahPasses, me = meOffice, settings: object = receiptSettings) => ({
  '/api/v1/settings/public': { body: publicSettings },
  '/api/v1/auth/me': { body: me },
  '/api/v1/admin/settings/receipts': { body: settings },
  '/api/v1/admin/persons': searchPersons,
  '/api/v1/admin/persons/{personId}': (init: MockRequest) => {
    const detail = personDetails[init.params?.path?.personId ?? ''];
    return detail ? { body: detail } : problem(404, 'resource not found');
  },
  '/api/v1/admin/persons/{personId}/passes': (init: MockRequest) => ({
    body: { items: init.params?.path?.personId === ids.personNoah ? passes : [] },
  }),
  '/api/v1/admin/pass-types': { body: { items: passTypes } },
});

function renderPage(personId = ids.personNoah) {
  render(<Toaster />);
  return renderInRouter(<PersonPage personId={personId} />, { at: '/admin/persons/$personId' });
}

const passesRegion = async () => within(await screen.findByRole('region', { name: 'Passes' }));

/** Opens the sell or give dialog of the passes section. */
async function openDialog(user: ReturnType<typeof userEvent.setup>, button: string, title: string) {
  await user.click((await passesRegion()).getByRole('button', { name: button }));
  return within(await screen.findByRole('dialog', { name: title }));
}

/** Picks a pass type in the dialog's select. */
async function chooseType(
  user: ReturnType<typeof userEvent.setup>,
  dialog: ReturnType<typeof within>,
  option: string,
) {
  await user.click(dialog.getByRole('combobox', { name: 'Pass type' }));
  await user.click(await screen.findByRole('option', { name: option }));
}

describe('PersonPasses', () => {
  it("lists the person's passes with their visits, days, payer and state", async () => {
    mockApiGet(fixtures());

    renderPage();

    const section = await passesRegion();
    const list = within(await section.findByRole('list', { name: 'Passes of Noah Sample' }));
    const [usable, voided] = list.getAllByRole('listitem').map((li) => within(li));
    expect(usable!.getByText('10-visit pass')).toBeInTheDocument();
    expect(usable!.getByText('Usable')).toBeInTheDocument();
    expect(
      usable!.getByText('9 of 10 visits left · Oct 5, 2026 – Apr 2, 2027'),
    ).toBeInTheDocument();
    expect(
      usable!.getByText('Sold on Oct 5, 2026 for €140.00 · paid by Mia Sample'),
    ).toBeInTheDocument();
    expect(usable!.getByText('Paid for 1 booking')).toBeInTheDocument();
    expect(
      usable!.getByRole('link', { name: 'Receipt of 10-visit pass of Oct 5, 2026' }),
    ).toHaveAttribute('href', `/admin/receipts?receipt=${noahPasses[0]!.receiptId}`);
    expect(
      usable!.getByRole('button', { name: 'Void 10-visit pass of Oct 5, 2026' }),
    ).toBeInTheDocument();

    expect(voided!.getByText('Voided')).toBeInTheDocument();
    expect(voided!.getByText('Note: Sold to the wrong person.')).toBeInTheDocument();
    expect(voided!.queryByRole('button', { name: /^Void/ })).not.toBeInTheDocument();
  });

  it('says when the person has no passes', async () => {
    mockApiGet(fixtures([]));

    renderPage();

    expect(
      await (await passesRegion()).findByText('Noah Sample has no passes.'),
    ).toBeInTheDocument();
  });

  it('sells a pass to a child, paid by the guardian, at another price from another day', async () => {
    const post = mockApiPost({
      '/api/v1/admin/persons/{personId}/passes/sell': { status: 201, body: noahPasses[0] },
    });
    mockApiGet(fixtures());
    const user = userEvent.setup();

    renderPage();
    const dialog = await openDialog(user, 'Sell a pass', 'Sell a pass to Noah Sample');
    // Only the types on sale, with their prices.
    await user.click(dialog.getByRole('combobox', { name: 'Pass type' }));
    expect(
      await screen.findByRole('option', { name: '10-visit pass · €150.00' }),
    ).toBeInTheDocument();
    expect(screen.getByRole('option', { name: '30-day pass · €89.00' })).toBeInTheDocument();
    expect(screen.queryByRole('option', { name: /Single visit/ })).not.toBeInTheDocument();
    await user.click(screen.getByRole('option', { name: '10-visit pass · €150.00' }));

    // The guardian pays by default.
    expect(dialog.getByRole('combobox', { name: 'Paid by' })).toHaveTextContent('Mia Sample');
    expect(dialog.getByLabelText('Price paid (EUR, optional)')).toHaveAccessibleDescription(
      "Empty: the pass type's price, €150.00.",
    );
    await user.type(dialog.getByLabelText('First day (optional)'), '2026-11-01');
    await user.type(dialog.getByLabelText('Price paid (EUR, optional)'), '140');
    await user.click(dialog.getByRole('button', { name: 'Sell' }));

    expect(await screen.findByText('Noah Sample has bought 10-visit pass.')).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/admin/persons/{personId}/passes/sell', {
      params: { path: { personId: ids.personNoah } },
      body: {
        passTypeId: passTypes[0]!.id,
        payerId: ids.personMia,
        validFrom: '2026-11-01',
        price: 14000,
        paymentMethod: 'cash',
        recipientName: 'Mia Sample',
        recipientEmail: 'mia.sample@example.org',
      },
    });
    await waitFor(() =>
      expect(
        screen.queryByRole('dialog', { name: 'Sell a pass to Noah Sample' }),
      ).not.toBeInTheDocument(),
    );
  });

  it('sells to an adult who pays themselves, or to someone else who pays', async () => {
    const post = mockApiPost({
      '/api/v1/admin/persons/{personId}/passes/sell': { status: 201, body: noahPasses[0] },
    });
    mockApiGet(fixtures());
    const user = userEvent.setup();

    renderPage(ids.personMia);
    let dialog = await openDialog(user, 'Sell a pass', 'Sell a pass to Mia Sample');
    expect(dialog.getByRole('combobox', { name: 'Paid by' })).toHaveTextContent('Mia Sample');
    await chooseType(user, dialog, '30-day pass · €89.00');
    await user.click(dialog.getByRole('button', { name: 'Sell' }));
    expect(await screen.findByText('Mia Sample has bought 30-day pass.')).toBeInTheDocument();
    expect(post).toHaveBeenLastCalledWith('/api/v1/admin/persons/{personId}/passes/sell', {
      params: { path: { personId: ids.personMia } },
      body: {
        passTypeId: passTypes[1]!.id,
        paymentMethod: 'cash',
        recipientName: 'Mia Sample',
        recipientEmail: 'mia.sample@example.org',
      },
    });

    dialog = await openDialog(user, 'Sell a pass', 'Sell a pass to Mia Sample');
    await chooseType(user, dialog, '30-day pass · €89.00');
    await user.click(dialog.getByRole('combobox', { name: 'Paid by' }));
    await user.click(await screen.findByRole('option', { name: /Alex Example/ }));
    // The receipt names the new payer.
    expect(dialog.getByLabelText('Name on the receipt')).toHaveValue('Alex Example');
    expect(dialog.getByLabelText('Email (optional)')).toHaveValue('alex.example@example.com');
    await user.type(dialog.getByLabelText('Address (optional)'), '2 Sample Road{Enter}Sampletown');
    await user.click(dialog.getByRole('combobox', { name: 'Payment method' }));
    await user.click(await screen.findByRole('option', { name: 'Bank transfer' }));
    await user.type(dialog.getByLabelText('Note (optional)'), ' Birthday present ');
    await user.click(dialog.getByRole('button', { name: 'Sell' }));
    await waitFor(() =>
      expect(post).toHaveBeenLastCalledWith('/api/v1/admin/persons/{personId}/passes/sell', {
        params: { path: { personId: ids.personMia } },
        body: {
          passTypeId: passTypes[1]!.id,
          payerId: ids.personAlex,
          note: 'Birthday present',
          paymentMethod: 'transfer',
          recipientName: 'Alex Example',
          recipientEmail: 'alex.example@example.com',
          recipientAddress: '2 Sample Road\nSampletown',
        },
      }),
    );
  });

  it("asks for the payer's address on a receipt over 250 euros in the standard tax mode", async () => {
    mockApiGet(fixtures());
    const user = userEvent.setup();

    renderPage();
    const dialog = await openDialog(user, 'Sell a pass', 'Sell a pass to Noah Sample');
    await chooseType(user, dialog, '10-visit pass · €150.00');
    const address = dialog.getByLabelText('Address (optional)');
    expect(address).toHaveAccessibleDescription('Printed on the receipt, one line per line.');

    await user.type(dialog.getByLabelText('Price paid (EUR, optional)'), '260');
    expect(address).toHaveAccessibleDescription(
      'A receipt over €250.00 should name the payer’s address.',
    );
    await user.type(address, '1 Example Street');
    expect(address).toHaveAccessibleDescription('Printed on the receipt, one line per line.');
  });

  it('does not ask for the address when the school names no limit', async () => {
    const { addressRequiredAbove: _limit, ...smallBusiness } = receiptSettings;
    mockApiGet(fixtures(noahPasses, meOffice, { ...smallBusiness, taxMode: 'small_business' }));
    const user = userEvent.setup();

    renderPage();
    const dialog = await openDialog(user, 'Sell a pass', 'Sell a pass to Noah Sample');
    await chooseType(user, dialog, '10-visit pass · €150.00');
    await user.type(dialog.getByLabelText('Price paid (EUR, optional)'), '900');

    expect(dialog.getByLabelText('Address (optional)')).toHaveAccessibleDescription(
      'Printed on the receipt, one line per line.',
    );
  });

  it('says that receipts cannot be issued yet, with the way to the settings for an admin', async () => {
    mockApiGet(
      fixtures(noahPasses, meAdmin, { complete: false, missing: 'taxNumber', receiptPrefix: 'SD' }),
    );
    const user = userEvent.setup();

    renderPage();
    const dialog = await openDialog(user, 'Sell a pass', 'Sell a pass to Noah Sample');

    const alert = await dialog.findByRole('alert');
    expect(alert).toHaveTextContent(
      "Receipts can't be issued until the receipt settings are complete, so no pass can be sold.",
    );
    expect(alert).toHaveTextContent('Missing: Tax number or VAT ID.');
    expect(
      within(alert).getByRole('link', { name: 'Complete the receipt settings' }),
    ).toHaveAttribute('href', '/admin/settings');
  });

  it('asks the office to have an admin complete the receipt settings when the API refuses', async () => {
    mockApiPost({
      '/api/v1/admin/persons/{personId}/passes/sell': conflict('receipt-settings-incomplete'),
    });
    const get = mockApiGet(fixtures());
    const user = userEvent.setup();

    renderPage();
    const dialog = await openDialog(user, 'Sell a pass', 'Sell a pass to Noah Sample');
    await chooseType(user, dialog, '10-visit pass · €150.00');
    await user.click(dialog.getByRole('button', { name: 'Sell' }));

    const alert = await dialog.findByRole('alert');
    expect(alert).toHaveTextContent('Ask an administrator to complete them.');
    expect(within(alert).queryByRole('link')).not.toBeInTheDocument();
    // No general error besides, and the settings are asked again.
    expect(dialog.getAllByRole('alert')).toHaveLength(1);
    await waitFor(() =>
      expect(
        get.mock.calls.filter(
          (call) => (call as unknown[])[0] === '/api/v1/admin/settings/receipts',
        ),
      ).toHaveLength(2),
    );
  });

  it('shows other errors besides incomplete receipt settings', async () => {
    mockApiPost({
      '/api/v1/admin/persons/{personId}/passes/sell': problem(500, 'boom'),
    });
    mockApiGet(
      fixtures(noahPasses, meOffice, {
        complete: false,
        missing: 'taxNumber',
        receiptPrefix: 'SD',
      }),
    );
    const user = userEvent.setup();

    renderPage();
    const dialog = await openDialog(user, 'Sell a pass', 'Sell a pass to Noah Sample');
    await chooseType(user, dialog, '10-visit pass · €150.00');
    await user.click(dialog.getByRole('button', { name: 'Sell' }));

    await waitFor(() => expect(dialog.getAllByRole('alert')).toHaveLength(2));
    expect(dialog.getByText('Something went wrong. Please try again.')).toBeInTheDocument();
    expect(dialog.getByText('Missing: Tax number or VAT ID.')).toBeInTheDocument();
  });

  it('asks for the pass types only once a dialog opens', async () => {
    const get = mockApiGet(fixtures());
    const user = userEvent.setup();

    renderPage();
    await (await passesRegion()).findByRole('list', { name: 'Passes of Noah Sample' });
    expect(get).not.toHaveBeenCalledWith('/api/v1/admin/pass-types', expect.anything());

    await openDialog(user, 'Give a pass', 'Give Noah Sample a pass');
    await waitFor(() =>
      expect(get).toHaveBeenCalledWith('/api/v1/admin/pass-types', expect.anything()),
    );
  });

  it('checks the sale before sending it', async () => {
    const post = mockApiPost({});
    mockApiGet(fixtures());
    const user = userEvent.setup();

    renderPage();
    const dialog = await openDialog(user, 'Sell a pass', 'Sell a pass to Noah Sample');
    await user.click(dialog.getByRole('button', { name: 'Sell' }));
    expect(await dialog.findByText('Please choose one.')).toBeInTheDocument();

    await chooseType(user, dialog, '10-visit pass · €150.00');
    await user.type(dialog.getByLabelText('Price paid (EUR, optional)'), '1,999');
    await user.click(dialog.getByRole('button', { name: 'Sell' }));
    expect(dialog.getByLabelText('Price paid (EUR, optional)')).toHaveAccessibleDescription(
      expect.stringContaining('Enter an amount such as 12.50.'),
    );
    expect(post).not.toHaveBeenCalled();
  });

  it('says when the pass type was retired meanwhile', async () => {
    mockApiPost({
      '/api/v1/admin/persons/{personId}/passes/sell': problem(422, 'inactive', [
        { location: 'body.passTypeId' },
      ]),
    });
    mockApiGet(fixtures());
    const user = userEvent.setup();

    renderPage();
    const dialog = await openDialog(user, 'Sell a pass', 'Sell a pass to Noah Sample');
    await chooseType(user, dialog, '10-visit pass · €150.00');
    await user.click(dialog.getByRole('button', { name: 'Sell' }));

    expect(
      await dialog.findByText('This pass type is no longer sold; choose another one.'),
    ).toBeInTheDocument();
    expect(dialog.getByRole('alert')).toHaveTextContent('Please check your entries.');
  });

  it('says when the pass types or the currency cannot be loaded', async () => {
    let failing = true;
    mockApiGet({
      ...fixtures(),
      '/api/v1/admin/pass-types': () =>
        failing ? problem(500, 'boom') : { body: { items: passTypes } },
    });
    const user = userEvent.setup();

    renderPage();
    const dialog = await openDialog(user, 'Sell a pass', 'Sell a pass to Noah Sample');
    expect(
      await dialog.findByText('The pass types or the currency could not be loaded.'),
    ).toBeInTheDocument();

    failing = false;
    await user.click(dialog.getByRole('button', { name: 'Retry' }));
    expect(await dialog.findByRole('combobox', { name: 'Pass type' })).toBeInTheDocument();
  });

  it('gives a pass away with a note', async () => {
    const post = mockApiPost({
      '/api/v1/admin/persons/{personId}/passes/assign': { status: 201, body: noahPasses[0] },
    });
    mockApiGet(fixtures());
    const user = userEvent.setup();

    renderPage();
    const dialog = await openDialog(user, 'Give a pass', 'Give Noah Sample a pass');
    expect(dialog.queryByRole('combobox', { name: 'Paid by' })).not.toBeInTheDocument();
    await chooseType(user, dialog, '30-day pass');
    await user.type(dialog.getByLabelText('Note (optional)'), 'Workshop canceled');
    await user.click(dialog.getByRole('button', { name: 'Give pass' }));

    expect(await screen.findByText('Noah Sample has been given 30-day pass.')).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/admin/persons/{personId}/passes/assign', {
      params: { path: { personId: ids.personNoah } },
      body: { passTypeId: passTypes[1]!.id, note: 'Workshop canceled' },
    });
  });

  it('voids a pass after asking', async () => {
    const post = mockApiPost({
      '/api/v1/admin/passes/{passId}/void': {
        body: { ...noahPasses[0], status: 'voided', usable: false },
      },
    });
    mockApiGet(fixtures());
    const user = userEvent.setup();

    renderPage();
    await user.click(
      await (
        await passesRegion()
      ).findByRole('button', { name: 'Void 10-visit pass of Oct 5, 2026' }),
    );
    const dialog = within(await screen.findByRole('alertdialog', { name: 'Void 10-visit pass?' }));
    await user.click(dialog.getByRole('button', { name: 'Void pass' }));

    expect(await screen.findByText('10-visit pass has been voided.')).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/admin/passes/{passId}/void', {
      params: { path: { passId: noahPasses[0]!.id } },
    });
  });

  it('says when the pass was voided meanwhile', async () => {
    mockApiPost({ '/api/v1/admin/passes/{passId}/void': conflict('already-voided') });
    mockApiGet(fixtures());
    const user = userEvent.setup();

    renderPage();
    await user.click(
      await (
        await passesRegion()
      ).findByRole('button', { name: 'Void 10-visit pass of Oct 5, 2026' }),
    );
    const dialog = within(await screen.findByRole('alertdialog', { name: 'Void 10-visit pass?' }));
    await user.click(dialog.getByRole('button', { name: 'Void pass' }));

    expect(await dialog.findByRole('alert')).toHaveTextContent(
      '10-visit pass has been voided already.',
    );
    expect(dialog.queryByRole('button', { name: 'Void pass' })).not.toBeInTheDocument();
    expect(dialog.getByRole('button', { name: 'Keep' })).toBeEnabled();
  });
});
