import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { openDownload } from '@/api/download';
import { Toaster } from '@/components/ui/sonner';
import i18n from '@/i18n';
import {
  conflict,
  meOffice,
  mockApiGet,
  mockApiPost,
  type MockRequest,
  problem,
  publicSettings,
  receiptCanceled,
  receiptCancellation,
  receiptSale,
  receipts,
} from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { ReceiptsPage } from './ReceiptsPage';

vi.mock('@/api/download', () => ({ openDownload: vi.fn(() => Promise.resolve()) }));

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date('2026-10-23T10:00:00Z'));
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.mocked(openDownload).mockClear();
});

const byId = Object.fromEntries(receipts.map((r) => [r.id, r]));

function mockOffice(
  list: (init: MockRequest) => { body: unknown } = () => ({
    body: { items: receipts, total: receipts.length, years: [2026, 2025] },
  }),
) {
  const post = mockApiPost({ '/api/v1/admin/receipts/search': list });
  mockApiGet({
    '/api/v1/settings/public': { body: publicSettings },
    '/api/v1/auth/me': { body: meOffice },
    '/api/v1/admin/receipts/{receiptId}': (init: MockRequest) => {
      const r = byId[init.params?.path?.receiptId ?? ''];
      return r ? { body: r } : problem(404, 'not found');
    },
  });
  return post;
}

function renderPage(receipt?: string) {
  render(<Toaster />);
  return renderInRouter(<ReceiptsPage />, { at: '/admin/receipts', search: { receipt } });
}

/** The table row of the receipt `number`. */
async function row(number: string) {
  const table = within(await screen.findByRole('table', { name: 'Receipts' }));
  const tr = (await table.findByText(number)).closest('tr');
  if (!tr) throw new Error(`no row ${number}`);
  return within(tr);
}

describe('ReceiptsPage', () => {
  it('lists the receipts with their date, recipient, kind, method and amount', async () => {
    mockOffice();

    renderPage();

    expect(await screen.findByRole('heading', { level: 1, name: 'Receipts' })).toBeInTheDocument();
    const sale = await row('SD-2026-0002');
    expect(sale.getByText('Oct 5, 2026')).toBeInTheDocument();
    expect(sale.getByText('Mia Sample')).toBeInTheDocument();
    expect(sale.getByText('Receipt')).toBeInTheDocument();
    expect(sale.getByText('Cash')).toBeInTheDocument();
    expect(sale.getByText('€140.00')).toBeInTheDocument();
    expect((await row('SD-2026-0001')).getByText('Canceled')).toBeInTheDocument();
    const storno = await row('SD-2026-0003');
    expect(storno.getByText('Cancellation')).toBeInTheDocument();
    expect(storno.getByText('-€18.00')).toBeInTheDocument();
  });

  it('filters by year and searches by number or name', async () => {
    const post = mockOffice();
    const user = userEvent.setup();

    renderPage();
    await row('SD-2026-0002');
    await user.click(screen.getByRole('combobox', { name: 'Year' }));
    // The years with receipts, as the API lists them.
    expect((await screen.findAllByRole('option')).map((o) => o.textContent)).toEqual([
      'All years',
      '2026',
      '2025',
    ]);
    await user.click(await screen.findByRole('option', { name: '2026' }));
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith('/api/v1/admin/receipts/search', {
        body: { year: 2026, offset: 0, limit: 50 },
        signal: expect.anything(),
      }),
    );
    await user.type(screen.getByRole('searchbox', { name: 'Search receipts' }), ' mia ');
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith('/api/v1/admin/receipts/search', {
        body: { year: 2026, query: 'mia', offset: 0, limit: 50 },
        signal: expect.anything(),
      }),
    );
  });

  it('pages through many receipts', async () => {
    const post = mockOffice((init) => ({
      body: {
        items: (init.body as { offset?: number } | undefined)?.offset
          ? [receiptCanceled]
          : [receiptCancellation, receiptSale],
        total: 51,
        years: [2026],
      },
    }));
    const user = userEvent.setup();

    renderPage();
    const pages = within(await screen.findByRole('navigation', { name: 'Pages of receipts' }));
    expect(pages.getByText('1–50 of 51')).toBeInTheDocument();
    expect(pages.getByRole('button', { name: 'Previous' })).toBeDisabled();
    await user.click(pages.getByRole('button', { name: 'Next' }));

    expect(await pages.findByText('51–51 of 51')).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/admin/receipts/search', {
      body: { offset: 50, limit: 50 },
      signal: expect.anything(),
    });
    expect(pages.getByRole('button', { name: 'Next' })).toBeDisabled();
  });

  it('shows a receipt with its issuer, recipient, lines and totals, and its PDF', async () => {
    mockOffice();
    const user = userEvent.setup();

    const { router } = renderPage();
    await user.click(
      (await row('SD-2026-0002')).getByRole('button', { name: 'Open receipt SD-2026-0002' }),
    );

    const sheet = within(await screen.findByRole('dialog', { name: 'Receipt SD-2026-0002' }));
    expect(router.state.location.search).toEqual({ receipt: receiptSale.id });
    expect(await sheet.findByText(/^Example Dance Studio/)).toHaveTextContent(
      'Example Dance Studio 1 Example Street Sampletown Tax number: 00/000/00000 office@example.org',
    );
    expect(sheet.getByText(/^Mia Sample/)).toHaveTextContent('Mia Sample 2 Sample Road Sampletown');
    expect(sheet.getByText('Standard VAT')).toBeInTheDocument();
    const lines = within(sheet.getByRole('list', { name: 'Lines' }));
    expect(lines.getByText('10-visit pass for Noah Sample')).toBeInTheDocument();
    expect(lines.getByText('Net €117.65 · VAT 19% €22.35 · Gross €140.00')).toBeInTheDocument();
    expect(sheet.getByText('Total').nextSibling).toHaveTextContent('€140.00');

    await user.click(sheet.getByRole('button', { name: 'Download receipt SD-2026-0002 as PDF' }));
    expect(openDownload).toHaveBeenCalledWith(`/api/v1/admin/receipts/${receiptSale.id}/pdf`);
  });

  it('links a canceled receipt and its cancellation', async () => {
    mockOffice();
    const user = userEvent.setup();

    renderPage(receiptCanceled.id);
    const sheet = within(await screen.findByRole('dialog', { name: 'Receipt SD-2026-0001' }));
    expect(sheet.queryByRole('button', { name: 'Cancel receipt' })).not.toBeInTheDocument();
    await user.click(
      await sheet.findByRole('button', { name: 'SD-2026-0003 of September 3, 2026' }),
    );

    const storno = within(await screen.findByRole('dialog', { name: 'Cancellation SD-2026-0003' }));
    expect(await storno.findByText('Sold to the wrong person')).toBeInTheDocument();
    expect(
      storno.getByRole('button', { name: 'SD-2026-0001 of September 2, 2026' }),
    ).toBeInTheDocument();
  });

  it('cancels a receipt with a reason and shows the cancellation', async () => {
    mockOffice();
    const post = mockApiPost({
      '/api/v1/admin/receipts/{receiptId}/cancel': {
        status: 201,
        body: { ...receiptCancellation, number: 'SD-2026-0004' },
      },
    });
    const user = userEvent.setup();

    const { router } = renderPage(receiptSale.id);
    const sheet = within(await screen.findByRole('dialog', { name: 'Receipt SD-2026-0002' }));
    await user.click(await sheet.findByRole('button', { name: 'Cancel receipt' }));
    const dialog = within(
      await screen.findByRole('alertdialog', { name: 'Cancel receipt SD-2026-0002?' }),
    );
    await user.type(dialog.getByLabelText('Reason (optional, printed)'), 'Wrong pass');
    await user.click(dialog.getByRole('button', { name: 'Cancel receipt' }));

    expect(
      await screen.findByText('Cancellation SD-2026-0004 has been issued; the pass is voided.'),
    ).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/admin/receipts/{receiptId}/cancel', {
      params: { path: { receiptId: receiptSale.id } },
      body: { reason: 'Wrong pass' },
    });
    await waitFor(() =>
      expect(router.state.location.search).toEqual({ receipt: receiptCancellation.id }),
    );
  });

  it.each([
    [
      'pass-used',
      "The pass has paid for visits, so its receipt cannot be canceled. Void the pass on the person's page instead.",
    ],
    ['already-canceled', 'This receipt has been canceled already.'],
    ['receipt-settings-incomplete', 'Ask an administrator to complete them.'],
  ] as const)('says why a cancellation is refused (%s)', async (reason, message) => {
    mockOffice();
    mockApiPost({ '/api/v1/admin/receipts/{receiptId}/cancel': conflict(reason) });
    const user = userEvent.setup();

    renderPage(receiptSale.id);
    const sheet = within(await screen.findByRole('dialog', { name: 'Receipt SD-2026-0002' }));
    await user.click(await sheet.findByRole('button', { name: 'Cancel receipt' }));
    const dialog = within(
      await screen.findByRole('alertdialog', { name: 'Cancel receipt SD-2026-0002?' }),
    );
    await user.click(dialog.getByRole('button', { name: 'Cancel receipt' }));

    expect(await dialog.findByRole('alert')).toHaveTextContent(message);
    expect(dialog.getByRole('button', { name: 'Cancel receipt' })).toBeDisabled();
  });

  it('says when a linked receipt does not exist', async () => {
    mockOffice();

    renderPage('0190a000-0000-7000-8000-000000000999');

    expect(await screen.findByText('This receipt does not exist.')).toBeInTheDocument();
  });

  it('renders in German', async () => {
    mockOffice();
    await i18n.changeLanguage('de');

    renderPage();

    expect(await screen.findByRole('heading', { level: 1, name: 'Belege' })).toBeInTheDocument();
    const table = within(await screen.findByRole('table', { name: 'Belege' }));
    const tr = (await table.findByText('SD-2026-0002')).closest('tr')!;
    expect(within(tr).getByText(/^140,00\s€$/)).toBeInTheDocument();
    expect(within(tr).getByText('Bar')).toBeInTheDocument();
  });
});
