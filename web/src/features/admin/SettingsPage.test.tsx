import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Toaster } from '@/components/ui/sonner';
import i18n from '@/i18n';
import {
  meAdmin,
  meOffice,
  mockApiDelete,
  mockApiGet,
  mockApiPut,
  problem,
  receiptSettings,
} from '@/test/mockApi';
import { renderWithQuery } from '@/test/utils';
import { SettingsPage } from './SettingsPage';

afterEach(() => {
  vi.restoreAllMocks();
});

const fixtures = (me = meAdmin, receipts: object = receiptSettings) => ({
  '/api/v1/auth/me': { body: me },
  '/api/v1/admin/settings/passes': { body: { redemptionMode: 'attendance' } },
  '/api/v1/admin/settings/receipts': { body: receipts },
  '/api/v1/admin/settings/receipts/logo': {
    body: new Blob(['png'], { type: 'image/png' }),
  },
});

function renderPage() {
  render(<Toaster />);
  return renderWithQuery(<SettingsPage />);
}

const passesCard = async () => within(await screen.findByRole('region', { name: 'Passes' }));

describe('SettingsPage', () => {
  it('lets an admin change when bookings use a pass', async () => {
    mockApiGet(fixtures());
    const put = mockApiPut({
      '/api/v1/admin/settings/passes': ({ body }) => ({ body }),
    });
    const user = userEvent.setup();

    renderPage();
    expect(await screen.findByRole('heading', { level: 1, name: 'Settings' })).toBeInTheDocument();
    const card = await passesCard();
    const select = await card.findByRole('combobox', { name: 'Use a pass' });
    expect(select).toHaveTextContent('On attendance');
    expect(card.getByRole('button', { name: 'Save changes' })).toBeDisabled();

    await user.click(select);
    await user.click(await screen.findByRole('option', { name: 'On booking' }));
    expect(select).toHaveAccessibleDescription(
      'A visit is used when the lesson is booked; a timely cancellation gives it back, a late one keeps it. Visits used already stay used.',
    );
    await user.click(card.getByRole('button', { name: 'Save changes' }));

    expect(await screen.findByText('The pass settings have been saved.')).toBeInTheDocument();
    expect(put).toHaveBeenCalledWith('/api/v1/admin/settings/passes', {
      body: { redemptionMode: 'booking' },
    });
  });

  it('says why saving failed', async () => {
    mockApiGet(fixtures());
    mockApiPut({ '/api/v1/admin/settings/passes': problem(500, 'boom') });
    const user = userEvent.setup();

    renderPage();
    const card = await passesCard();
    await user.click(await card.findByRole('combobox', { name: 'Use a pass' }));
    await user.click(await screen.findByRole('option', { name: 'On booking' }));
    await user.click(card.getByRole('button', { name: 'Save changes' }));

    expect(await card.findByRole('alert')).toHaveTextContent(
      'Something went wrong. Please try again.',
    );
  });

  it('shows the office the setting without a way to change it', async () => {
    mockApiGet(fixtures(meOffice));

    renderPage();

    const card = await passesCard();
    expect(await card.findByText('On attendance')).toBeInTheDocument();
    expect(card.getByText('Only administrators can change this.')).toBeInTheDocument();
    expect(card.queryByRole('combobox')).not.toBeInTheDocument();
    expect(card.queryByRole('button', { name: 'Save changes' })).not.toBeInTheDocument();
  });

  it('says when the settings cannot be loaded', async () => {
    mockApiGet({ ...fixtures(), '/api/v1/admin/settings/passes': problem(500, 'boom') });

    renderPage();

    expect(
      await (await passesCard()).findByText('The pass settings could not be loaded.'),
    ).toBeInTheDocument();
  });

  it('renders in German', async () => {
    mockApiGet(fixtures(meOffice));
    await i18n.changeLanguage('de');

    renderPage();

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Einstellungen' }),
    ).toBeInTheDocument();
    expect(
      await within(await screen.findByRole('region', { name: 'Karten' })).findByText(
        'Bei Anwesenheit',
      ),
    ).toBeInTheDocument();
  });
});

const receiptsCard = async () => within(await screen.findByRole('region', { name: 'Receipts' }));

describe('Receipt settings', () => {
  it('lets an admin change what receipts print', async () => {
    mockApiGet(fixtures());
    const put = mockApiPut({
      '/api/v1/admin/settings/receipts': ({ body }) => ({
        body: { ...(body as object), complete: true },
      }),
    });
    const user = userEvent.setup();

    renderPage();
    const card = await receiptsCard();
    const form = within(await card.findByRole('form', { name: 'Receipts' }));
    expect(form.getByLabelText('Issuer')).toHaveValue('Example Dance Studio');
    expect(form.getByRole('combobox', { name: 'Tax mode' })).toHaveTextContent('Standard VAT');
    await user.clear(form.getByLabelText('Tax number or VAT ID'));
    await user.type(form.getByLabelText('Tax number or VAT ID'), 'DE000000000');
    await user.type(form.getByLabelText('Footer (optional)'), 'IBAN DE00 0000 0000 0000 0000 00');
    await user.clear(form.getByLabelText('Number prefix'));
    await user.type(form.getByLabelText('Number prefix'), 'ds');
    await user.click(form.getByRole('button', { name: 'Save changes' }));

    expect(await screen.findByText('The receipt settings have been saved.')).toBeInTheDocument();
    expect(put).toHaveBeenCalledWith('/api/v1/admin/settings/receipts', {
      body: {
        issuerName: 'Example Dance Studio',
        issuerAddress: '1 Example Street\nSampletown',
        taxNumber: 'DE000000000',
        email: 'office@example.org',
        footer: 'IBAN DE00 0000 0000 0000 0000 00',
        taxMode: 'standard',
        receiptPrefix: 'DS',
      },
    });
  });

  it('checks the number prefix', async () => {
    mockApiGet(fixtures());
    const put = mockApiPut({});
    const user = userEvent.setup();

    renderPage();
    const form = within(await (await receiptsCard()).findByRole('form', { name: 'Receipts' }));
    await user.clear(form.getByLabelText('Number prefix'));
    await user.type(form.getByLabelText('Number prefix'), 'S-D');
    await user.click(form.getByRole('button', { name: 'Save changes' }));

    expect(form.getByLabelText('Number prefix')).toHaveAccessibleDescription(
      expect.stringContaining('Use 1 to 16 letters A–Z and digits.'),
    );
    expect(put).not.toHaveBeenCalled();
  });

  it('says what receipts still need', async () => {
    mockApiGet(fixtures(meAdmin, { complete: false, missing: 'taxMode', receiptPrefix: 'SD' }));

    renderPage();

    const alert = await (await receiptsCard()).findByRole('alert');
    expect(alert).toHaveTextContent(
      "Receipts can't be issued until the receipt settings are complete, so no pass can be sold.",
    );
    expect(alert).toHaveTextContent('Missing: Tax mode.');
  });

  it('shows the office the receipt settings without a way to change them', async () => {
    mockApiGet(fixtures(meOffice));

    renderPage();

    const card = await receiptsCard();
    expect(await card.findByText('00/000/00000')).toBeInTheDocument();
    expect(card.getByText('Standard VAT')).toBeInTheDocument();
    expect(card.queryByRole('form')).not.toBeInTheDocument();
    expect(await card.findByRole('img', { name: 'The logo printed on receipts' })).toHaveAttribute(
      'src',
      `data:image/png;base64,${btoa('png')}`,
    );
    expect(card.queryByText('Replace the logo')).not.toBeInTheDocument();
  });

  it('uploads, refuses and removes the logo', async () => {
    mockApiGet(fixtures());
    const put = mockApiPut({ '/api/v1/admin/settings/receipts/logo': { status: 204 } });
    const del = mockApiDelete({ '/api/v1/admin/settings/receipts/logo': { status: 204 } });
    const user = userEvent.setup();

    renderPage();
    const card = await receiptsCard();
    expect(
      await card.findByRole('img', { name: 'The logo printed on receipts' }),
    ).toBeInTheDocument();
    const input = card.getByLabelText('Replace the logo');

    await user.upload(
      input,
      new File([new Uint8Array(600 * 1024)], 'big.png', { type: 'image/png' }),
    );
    expect(card.getByText('Choose a PNG or JPEG image of at most 512 KB.')).toBeInTheDocument();
    expect(
      card.getByText('A PNG or JPEG of at most 512 KB, printed at the top of later receipts.'),
    ).toBeInTheDocument();
    expect(put).not.toHaveBeenCalled();

    await user.upload(input, new File(['png'], 'logo.png', { type: 'image/png' }));
    expect(await screen.findByText('The logo has been saved.')).toBeInTheDocument();
    expect(put).toHaveBeenCalledWith('/api/v1/admin/settings/receipts/logo', {
      body: { contentType: 'image/png', data: btoa('png') },
    });

    await user.click(card.getByRole('button', { name: 'Remove the logo' }));
    expect(await screen.findByText('The logo has been removed.')).toBeInTheDocument();
    expect(del).toHaveBeenCalledWith('/api/v1/admin/settings/receipts/logo');
  });
});

describe('Receipt logo', () => {
  it('says when the chosen file cannot be read', async () => {
    mockApiGet(fixtures());
    const put = mockApiPut({});
    const user = userEvent.setup();

    renderPage();
    const card = await receiptsCard();
    await card.findByRole('img', { name: 'The logo printed on receipts' });
    vi.spyOn(FileReader.prototype, 'readAsDataURL').mockImplementation(function (this: FileReader) {
      this.onerror?.(new ProgressEvent('error') as ProgressEvent<FileReader>);
    });
    await user.upload(
      card.getByLabelText('Replace the logo'),
      new File(['png'], 'logo.png', { type: 'image/png' }),
    );

    expect(await card.findByRole('alert')).toHaveTextContent(
      'The file could not be read; choose it again.',
    );
    expect(put).not.toHaveBeenCalled();
  });
});
