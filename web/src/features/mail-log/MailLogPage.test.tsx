import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { MailLogDetail, MailLogItem } from '@/api/queries';
import { Toaster } from '@/components/ui/sonner';
import i18n from '@/i18n';
import {
  conflict,
  meAdmin,
  meOffice,
  mockApiGet,
  mockApiPost,
  problem,
  publicSettings,
  type MockRequest,
} from '@/test/mockApi';
import { renderWithQuery } from '@/test/utils';
import { MailLogPage } from './MailLogPage';
import { mailTemplates } from './mailTemplates';

afterEach(() => {
  vi.restoreAllMocks();
});

const id = (n: number) => `0190a000-0000-7000-8000-${String(n).padStart(12, '0')}`;

/** Fictional mails as GET /admin/mails lists them (newest first). */
const mails: MailLogItem[] = [
  {
    id: id(931),
    to: 'nora.example@example.org',
    subject: 'Studio Example: Costume fitting',
    template: 'announcement',
    createdAt: '2026-10-23T14:30:00Z',
    status: 'pending',
    attempts: 0,
    nextAttemptAt: '2026-10-23T14:30:00Z',
    body: 'shown',
  },
  {
    id: id(932),
    to: 'ada@example.org',
    subject: 'Confirm your email address',
    template: 'confirm-email',
    createdAt: '2026-10-22T10:00:00Z',
    status: 'sent',
    attempts: 1,
    sentAt: '2026-10-22T10:01:00Z',
    body: 'withheld',
  },
  {
    id: id(933),
    to: 'olaf.old@example.org',
    subject: 'Your receipt SD-2026-0007',
    template: 'receipt',
    createdAt: '2026-10-21T09:00:00Z',
    status: 'failed',
    attempts: 5,
    lastError: '550 no such user',
    attachment: 'SD-2026-0007.pdf',
    body: 'shown',
  },
];

const details: Record<string, MailLogDetail> = {
  [id(931)]: { ...(mails[0] as MailLogItem), text: 'Hello Nora,\n\nplease come on Saturday.' },
  [id(932)]: mails[1] as MailLogItem,
  [id(933)]: { ...(mails[2] as MailLogItem), text: 'Attached is your receipt.' },
};

const asAdmin = {
  '/api/v1/auth/me': { body: meAdmin },
  '/api/v1/settings/public': { body: publicSettings },
  '/api/v1/admin/mails': ({ params }: MockRequest) => {
    const status = params?.query?.status;
    const items = status ? mails.filter((m) => m.status === status) : mails;
    return { body: { items } };
  },
  '/api/v1/admin/mails/{mailId}': ({ params }: MockRequest) => ({
    body: details[params?.path?.mailId ?? ''],
  }),
};

function renderPage() {
  render(<Toaster />);
  return renderWithQuery(<MailLogPage />);
}

/** The cells of the table's body rows, as text. */
async function rows() {
  const table = within(await screen.findByRole('table', { name: 'Mails' }));
  return table
    .getAllByRole('row')
    .slice(1)
    .map((tr) =>
      within(tr)
        .getAllByRole('cell')
        .slice(0, 4)
        .map((td) => td.textContent),
    );
}

describe('MailLogPage', () => {
  it('lists the mails, newest first, in the school time zone', async () => {
    mockApiGet(asAdmin);

    renderPage();

    expect(await screen.findByRole('heading', { level: 1, name: 'Mail log' })).toBeInTheDocument();
    expect(await rows()).toEqual([
      [
        'Oct 23, 2026, 16:30',
        'nora.example@example.org',
        'Studio Example: Costume fittingAnnouncement',
        'Pending',
      ],
      [
        'Oct 22, 2026, 12:00',
        'ada@example.org',
        'Confirm your email addressEmail confirmation',
        'Sent',
      ],
      ['Oct 21, 2026, 11:00', 'olaf.old@example.org', 'Your receipt SD-2026-0007Receipt', 'Failed'],
    ]);
  });

  it('filters by status', async () => {
    const get = mockApiGet(asAdmin);
    const user = userEvent.setup();

    renderPage();
    await screen.findByRole('table', { name: 'Mails' });
    await user.click(screen.getByRole('combobox', { name: 'Status' }));
    await user.click(await screen.findByRole('option', { name: 'Failed' }));

    await vi.waitFor(async () => expect(await rows()).toHaveLength(1));
    expect(get).toHaveBeenCalledWith('/api/v1/admin/mails', {
      params: { query: { status: 'failed' } },
      signal: expect.anything(),
    });
  });

  it('shows a mail with its text', async () => {
    mockApiGet(asAdmin);
    const user = userEvent.setup();

    renderPage();
    await user.click(
      await screen.findByRole('button', { name: 'Show the mail to nora.example@example.org' }),
    );

    const sheet = within(
      await screen.findByRole('dialog', { name: 'Mail to nora.example@example.org' }),
    );
    expect(await sheet.findByText(/please come on Saturday/)).toBeInTheDocument();
    expect(sheet.queryByRole('button', { name: 'Queue again' })).toBeNull();
  });

  it('never shows the text of a mail with a secret link', async () => {
    mockApiGet(asAdmin);
    const user = userEvent.setup();

    renderPage();
    await user.click(
      await screen.findByRole('button', { name: 'Show the mail to ada@example.org' }),
    );

    const sheet = within(await screen.findByRole('dialog', { name: 'Mail to ada@example.org' }));
    expect(sheet.getByText(/contains a secret link/)).toBeInTheDocument();
  });

  it('queues a failed mail again', async () => {
    mockApiGet(asAdmin);
    const post = mockApiPost({
      '/api/v1/admin/mails/{mailId}/requeue': {
        body: { ...mails[2], status: 'pending', attempts: 0 },
      },
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(
      await screen.findByRole('button', { name: 'Show the mail to olaf.old@example.org' }),
    );
    const sheet = within(
      await screen.findByRole('dialog', { name: 'Mail to olaf.old@example.org' }),
    );
    expect(sheet.getByText('550 no such user')).toBeInTheDocument();
    expect(sheet.getByText('SD-2026-0007.pdf')).toBeInTheDocument();
    await user.click(sheet.getByRole('button', { name: 'Queue again' }));

    expect(
      await screen.findByText('The mail to olaf.old@example.org is queued again.'),
    ).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/admin/mails/{mailId}/requeue', {
      params: { path: { mailId: id(933) } },
    });
  });

  it('says why a mail is not queued again', async () => {
    mockApiGet(asAdmin);
    mockApiPost({ '/api/v1/admin/mails/{mailId}/requeue': conflict('not-failed') });
    const user = userEvent.setup();

    renderPage();
    await user.click(
      await screen.findByRole('button', { name: 'Show the mail to olaf.old@example.org' }),
    );
    const sheet = within(
      await screen.findByRole('dialog', { name: 'Mail to olaf.old@example.org' }),
    );
    await user.click(sheet.getByRole('button', { name: 'Queue again' }));

    expect(await sheet.findByText('Only a failed mail can be queued again.')).toBeInTheDocument();
  });

  it('says when the mails cannot be loaded', async () => {
    mockApiGet({ ...asAdmin, '/api/v1/admin/mails': problem(503, 'unavailable') });

    renderPage();

    expect(await screen.findByText('The mails could not be loaded.')).toBeInTheDocument();
  });

  it('is for admins only', async () => {
    const get = mockApiGet({ '/api/v1/auth/me': { body: meOffice } });

    renderPage();

    expect(await screen.findByText('Only administrators can see this page.')).toBeInTheDocument();
    expect(get).not.toHaveBeenCalledWith('/api/v1/admin/mails', expect.anything());
  });

  it('renders in German', async () => {
    mockApiGet(asAdmin);
    await i18n.changeLanguage('de');

    renderPage();

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Mail-Protokoll' }),
    ).toBeInTheDocument();
    expect(await screen.findByText('Fehlgeschlagen')).toBeInTheDocument();
    expect(screen.getByText('E-Mail-Bestätigung')).toBeInTheDocument();
  });
});

describe('mailTemplates', () => {
  it('has a label for every template of the API in every language', () => {
    expect(mailTemplates).toHaveLength(16);
    for (const lng of ['en', 'de']) {
      for (const name of mailTemplates) {
        expect(i18n.exists(`mailLog:templates.${name}`, { lng }), `${lng}: ${name}`).toBe(true);
      }
    }
  });
});
