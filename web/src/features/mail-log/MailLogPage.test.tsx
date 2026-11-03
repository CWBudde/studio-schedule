import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { toast } from 'sonner';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '@/api/client';
import type { MailLogDetail, MailLogItem } from '@/api/queries';
import { Toaster } from '@/components/ui/sonner';
import i18n from '@/i18n';
import {
  conflict,
  deferred,
  meAdmin,
  meOffice,
  mockApiGet,
  mockApiPost,
  type MockRequest,
  problem,
  publicSettings,
} from '@/test/mockApi';
import { renderWithQuery } from '@/test/utils';
import { MailLogPage } from './MailLogPage';
import { mailTemplates } from './mailTemplates';

afterEach(() => {
  vi.restoreAllMocks();
  // Sonner keeps its toasts across tests; a later one must not find them.
  toast.dismiss();
});

const id = (n: number) => `0190a000-0000-7000-8000-${String(n).padStart(12, '0')}`;

/** Fictional mails as POST /admin/mails/search finds them (newest first). */
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
  '/api/v1/admin/mails/{mailId}': ({ params }: MockRequest) => ({
    body: details[params?.path?.mailId ?? ''],
  }),
};

/** The mail log search: the mails, filtered by the body's status. */
function searchMails({ body }: MockRequest) {
  const status = (body as { status?: string } | undefined)?.status;
  const items = status ? mails.filter((m) => m.status === status) : mails;
  return { body: { items } };
}

/** Signs in as an admin; `search` answers the mail log search. */
function mockAdmin(search: Parameters<typeof mockApiPost>[0][string] = searchMails) {
  const post = mockApiPost({ '/api/v1/admin/mails/search': search });
  return { get: mockApiGet(asAdmin), post };
}

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
    mockAdmin();

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
    const { post } = mockAdmin();
    const user = userEvent.setup();

    renderPage();
    await screen.findByRole('table', { name: 'Mails' });
    await user.click(screen.getByRole('combobox', { name: 'Status' }));
    await user.click(await screen.findByRole('option', { name: 'Failed' }));

    await vi.waitFor(async () => expect(await rows()).toHaveLength(1));
    expect(post).toHaveBeenCalledWith('/api/v1/admin/mails/search', {
      body: { status: 'failed' },
      signal: expect.anything(),
    });
  });

  it('shows a mail with its text', async () => {
    mockAdmin();
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
    mockAdmin();
    const user = userEvent.setup();

    renderPage();
    await user.click(
      await screen.findByRole('button', { name: 'Show the mail to ada@example.org' }),
    );

    const sheet = within(await screen.findByRole('dialog', { name: 'Mail to ada@example.org' }));
    expect(sheet.getByText(/contains a secret link/)).toBeInTheDocument();
  });

  it('queues a failed mail again', async () => {
    mockAdmin();
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

  it('shows that a mail is being queued until the API answers', async () => {
    mockApiGet(asAdmin);
    const answer = deferred<unknown>();
    // The requeue waits for the test; the page's search still answers.
    vi.spyOn(api, 'POST').mockImplementation(((path: string, init: MockRequest) =>
      path === '/api/v1/admin/mails/{mailId}/requeue'
        ? answer.promise
        : Promise.resolve({
            data: searchMails(init).body,
            error: undefined,
            response: new Response(null, { status: 200 }),
          })) as unknown as typeof api.POST);
    const user = userEvent.setup();

    renderPage();
    await user.click(
      await screen.findByRole('button', { name: 'Show the mail to olaf.old@example.org' }),
    );
    const sheet = within(
      await screen.findByRole('dialog', { name: 'Mail to olaf.old@example.org' }),
    );
    await user.click(sheet.getByRole('button', { name: 'Queue again' }));

    const pending = await sheet.findByRole('button', { name: 'Queuing…' });
    expect(pending).toBeDisabled();
    expect(pending.querySelector('[data-slot="spinner"]')).not.toBeNull();

    answer.resolve({
      data: { ...mails[2], status: 'pending', attempts: 0 },
      error: undefined,
      response: new Response(null, { status: 200 }),
    });

    expect(
      await screen.findByText('The mail to olaf.old@example.org is queued again.'),
    ).toBeInTheDocument();
  });

  it('says why a mail is not queued again', async () => {
    mockAdmin();
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

    expect(await sheet.findByRole('alert')).toHaveTextContent(
      'Only a failed mail can be queued again.',
    );
    const toast = (
      await screen.findByText('The mail to olaf.old@example.org was not queued again.')
    ).closest('[data-sonner-toast]');
    expect(toast).toHaveTextContent('Only a failed mail can be queued again.');
    const button = sheet.getByRole('button', { name: 'Queue again' });
    expect(button).toBeEnabled();
    expect(button.querySelector('[data-slot="spinner"]')).toBeNull();
  });

  it('says when queuing a mail again fails', async () => {
    mockAdmin();
    mockApiPost({ '/api/v1/admin/mails/{mailId}/requeue': problem(500, 'internal') });
    const user = userEvent.setup();

    renderPage();
    await user.click(
      await screen.findByRole('button', { name: 'Show the mail to olaf.old@example.org' }),
    );
    const sheet = within(
      await screen.findByRole('dialog', { name: 'Mail to olaf.old@example.org' }),
    );
    await user.click(sheet.getByRole('button', { name: 'Queue again' }));

    const toast = (
      await screen.findByText('The mail to olaf.old@example.org was not queued again.')
    ).closest('[data-sonner-toast]');
    expect(toast).toHaveTextContent('Something went wrong. Please try again.');
    expect(toast).not.toHaveTextContent('internal');
    expect(sheet.getByRole('button', { name: 'Queue again' })).toBeEnabled();
  });

  it('says when the mails cannot be loaded', async () => {
    mockAdmin(problem(503, 'unavailable'));

    renderPage();

    expect(await screen.findByText('The mails could not be loaded.')).toBeInTheDocument();
  });

  it('is for admins only', async () => {
    mockApiGet({ '/api/v1/auth/me': { body: meOffice } });
    const post = mockApiPost({});

    renderPage();

    expect(await screen.findByText('Only administrators can see this page.')).toBeInTheDocument();
    expect(post).not.toHaveBeenCalledWith('/api/v1/admin/mails/search', expect.anything());
  });

  it('renders in German', async () => {
    mockAdmin();
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
    expect(mailTemplates).toHaveLength(20);
    for (const lng of ['en', 'de']) {
      for (const name of mailTemplates) {
        expect(i18n.exists(`mailLog:templates.${name}`, { lng }), `${lng}: ${name}`).toBe(true);
      }
    }
  });
});
