import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { AuditEventEntry } from '@/api/client';
import i18n from '@/i18n';
import {
  auditEvents,
  meAdmin,
  meOffice,
  mockApiGet,
  problem,
  publicSettings,
  signedOut,
} from '@/test/mockApi';
import { renderWithQuery } from '@/test/utils';
import { AuditPage } from './AuditPage';

afterEach(() => {
  vi.restoreAllMocks();
});

const asAdmin = {
  '/api/v1/auth/me': { body: meAdmin },
  '/api/v1/settings/public': { body: publicSettings },
  '/api/v1/admin/audit': { body: { items: auditEvents } },
};

/** An account that can no longer be found has only its ID. */
const lost = '0190a000-0000-7000-8000-000000000399';

/** An event whose target can no longer be found. */
const lostEvent: AuditEventEntry = {
  id: '0190a000-0000-7000-8000-000000000398',
  at: '2026-10-20T08:00:00Z',
  action: 'impersonation-started',
  actor: { id: meAdmin.accountId, email: meAdmin.email, givenName: 'Ada', familyName: 'Admin' },
  target: { id: lost },
};

/** The cells of the audit table's body rows, as text, in display order. */
async function rows() {
  const table = within(await screen.findByRole('table', { name: 'Audit log' }));
  return table
    .getAllByRole('row')
    .slice(1)
    .map((tr) =>
      within(tr)
        .getAllByRole('cell')
        .map((td) => td.textContent),
    );
}

describe('AuditPage', () => {
  it('lists the events newest first, in the school time zone, with who did what to whom', async () => {
    mockApiGet(asAdmin);

    renderWithQuery(<AuditPage />);

    expect(await screen.findByRole('heading', { level: 1, name: 'Audit log' })).toBeInTheDocument();
    const table = within(await screen.findByRole('table', { name: 'Audit log' }));
    expect(table.getAllByRole('columnheader').map((th) => th.textContent)).toEqual([
      'Time',
      'Action',
      'By',
      'Concerns',
    ]);
    // Berlin time, though the tests run in New York; an unknown action shows as it is.
    expect(await rows()).toEqual([
      [
        'Oct 26, 2026, 00:30',
        'account-merged',
        'Ada Adminada.admin@example.com',
        'Olaf Oldolaf.old@example.org',
      ],
      [
        'Oct 23, 2026, 16:45',
        'Sign-in as ended',
        'Ada Adminada.admin@example.com',
        'Nora Examplenora.example@example.org',
      ],
      [
        'Oct 23, 2026, 16:30',
        'Sign-in as started',
        'Ada Adminada.admin@example.com',
        'Nora Examplenora.example@example.org',
      ],
    ]);
    expect(screen.getByText('Oct 26, 2026, 00:30')).toHaveAttribute(
      'dateTime',
      '2026-10-25T23:30:00Z',
    );
  });

  it('searches the events', async () => {
    mockApiGet(asAdmin);
    const user = userEvent.setup();

    renderWithQuery(<AuditPage />);
    await user.type(await screen.findByRole('searchbox', { name: 'Search the audit log' }), 'olaf');

    expect((await rows()).map((cells) => cells[3])).toEqual(['Olaf Oldolaf.old@example.org']);

    await user.clear(screen.getByRole('searchbox', { name: 'Search the audit log' }));
    await user.type(screen.getByRole('searchbox', { name: 'Search the audit log' }), 'started');
    expect((await rows()).map((cells) => cells[1])).toEqual(['Sign-in as started']);
  });

  it('shows an account that can no longer be found by its ID, and finds it by the ID', async () => {
    mockApiGet({
      ...asAdmin,
      '/api/v1/admin/audit': { body: { items: [...auditEvents, lostEvent] } },
    });
    const user = userEvent.setup();

    renderWithQuery(<AuditPage />);
    await user.type(await screen.findByRole('searchbox', { name: 'Search the audit log' }), lost);

    expect((await rows()).map((cells) => cells[3])).toEqual([`Unknown account${lost}`]);
  });

  it('shows an account whose person can no longer be found by its email', async () => {
    const nameless: AuditEventEntry = {
      ...lostEvent,
      target: { id: lost, email: 'nameless@example.org' },
    };
    mockApiGet({ ...asAdmin, '/api/v1/admin/audit': { body: { items: [nameless] } } });

    renderWithQuery(<AuditPage />);

    expect((await rows()).map((cells) => cells[3])).toEqual(['nameless@example.org']);
  });

  it('shows a mail queued again, and a person, as what an event concerns', async () => {
    const requeued: AuditEventEntry = {
      ...lostEvent,
      id: '0190a000-0000-7000-8000-000000000397',
      action: 'mail-requeued',
      target: undefined,
      details: {
        mailId: '0190a000-0000-7000-8000-000000000396',
        recipient: 'nora.example@example.org',
        template: 'receipt',
      },
    };
    const aboutPerson: AuditEventEntry = {
      ...lostEvent,
      id: '0190a000-0000-7000-8000-000000000395',
      at: '2026-10-19T08:00:00Z',
      action: 'person-merged',
      target: undefined,
      targetPerson: {
        id: '0190a000-0000-7000-8000-000000000394',
        givenName: 'Mia',
        familyName: 'Example',
      },
      details: {
        mergedPersonId: '0190a000-0000-7000-8000-000000000393',
        mergedName: 'Mia Exampel',
      },
    };
    mockApiGet({
      ...asAdmin,
      '/api/v1/admin/audit': { body: { items: [requeued, aboutPerson] } },
    });
    const user = userEvent.setup();

    renderWithQuery(<AuditPage />);

    expect((await rows()).map((cells) => [cells[1], cells[3]])).toEqual([
      ['Mail queued again', 'Mail to nora.example@example.orgreceipt'],
      ['Duplicate merged', 'Mia Examplemerged: Mia Exampel'],
    ]);

    await user.type(screen.getByRole('searchbox', { name: 'Search the audit log' }), 'receipt');
    expect((await rows()).map((cells) => cells[1])).toEqual(['Mail queued again']);
  });

  it('names an anonymized person in the current language', async () => {
    const anonymized: AuditEventEntry = {
      ...lostEvent,
      action: 'person-anonymized',
      target: undefined,
      targetPerson: {
        id: '0190a000-0000-7000-8000-000000000392',
        givenName: 'Deleted',
        familyName: 'person',
        anonymized: true,
      },
    };
    mockApiGet({ ...asAdmin, '/api/v1/admin/audit': { body: { items: [anonymized] } } });
    await i18n.changeLanguage('de');

    renderWithQuery(<AuditPage />);

    const table = within(await screen.findByRole('table', { name: 'Protokoll' }));
    expect(table.getByText('Gelöschte Person')).toBeInTheDocument();
    expect(table.queryByText('Deleted person')).toBeNull();
  });

  it('says how many of the latest events it lists', async () => {
    mockApiGet(asAdmin);

    renderWithQuery(<AuditPage />);

    expect(
      await screen.findByText(/^The latest 200 recorded actions, newest first/),
    ).toBeInTheDocument();
  });

  it('shows only the actions it knows by name, any other one as the API names it', async () => {
    const odd = ['toString', 'actions', '__proto__'].map((action, i): AuditEventEntry => ({
      ...lostEvent,
      id: `0190a000-0000-7000-8000-00000000039${i}`,
      at: `2026-10-2${i}T08:00:00Z`,
      action,
    }));
    mockApiGet({ ...asAdmin, '/api/v1/admin/audit': { body: { items: odd } } });

    renderWithQuery(<AuditPage />);

    expect((await rows()).map((cells) => cells[1])).toEqual(['__proto__', 'actions', 'toString']);
  });

  it('says when nothing has been recorded yet', async () => {
    mockApiGet({ ...asAdmin, '/api/v1/admin/audit': { body: { items: [] } } });

    renderWithQuery(<AuditPage />);

    expect(await screen.findByText('Nothing has been recorded yet.')).toBeInTheDocument();
  });

  it('says when the events cannot be loaded, and loads them again', async () => {
    let fail = true;
    mockApiGet({
      ...asAdmin,
      '/api/v1/admin/audit': () =>
        fail ? problem(503, 'unavailable') : { body: { items: auditEvents } },
    });
    const user = userEvent.setup();

    renderWithQuery(<AuditPage />);

    expect(await screen.findByText('Could not load the data.')).toBeInTheDocument();
    fail = false;
    await user.click(screen.getByRole('button', { name: 'Retry' }));

    expect(await rows()).toHaveLength(auditEvents.length);
  });

  it.each([
    ['the office', { '/api/v1/auth/me': { body: meOffice } }],
    ['a visitor who is not signed in', signedOut],
  ])('shows no access to %s and does not ask for the events', async (_, fixtures) => {
    const get = mockApiGet(fixtures);

    renderWithQuery(<AuditPage />);

    expect(await screen.findByText('Only administrators can see this page.')).toBeInTheDocument();
    expect(screen.queryByRole('table')).not.toBeInTheDocument();
    expect(get).not.toHaveBeenCalledWith('/api/v1/admin/audit', expect.anything());
  });

  it('renders in German', async () => {
    mockApiGet(asAdmin);
    await i18n.changeLanguage('de');

    renderWithQuery(<AuditPage />);

    expect(await screen.findByRole('heading', { level: 1, name: 'Protokoll' })).toBeInTheDocument();
    expect(
      await screen.findByText(/^Die letzten 200 protokollierten Aktionen/),
    ).toBeInTheDocument();
    const table = within(await screen.findByRole('table', { name: 'Protokoll' }));
    expect(table.getAllByRole('columnheader').map((th) => th.textContent)).toEqual([
      'Zeitpunkt',
      'Aktion',
      'Von',
      'Betrifft',
    ]);
    expect(table.getByText('26. Okt. 2026, 00:30')).toBeInTheDocument();
    expect(table.getByText('Anmelden als begonnen')).toBeInTheDocument();
    expect(table.getByText('Anmelden als beendet')).toBeInTheDocument();
  });

  it('names an account that can no longer be found in German', async () => {
    mockApiGet({ ...asAdmin, '/api/v1/admin/audit': { body: { items: [lostEvent] } } });
    await i18n.changeLanguage('de');

    renderWithQuery(<AuditPage />);

    expect(await screen.findByText('Unbekanntes Konto')).toBeInTheDocument();
  });
});
