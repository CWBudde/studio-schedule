import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Toaster } from '@/components/ui/sonner';
import {
  conflict,
  ids,
  meOffice,
  mockApiGet,
  mockApiPost,
  type MockRequest,
  personDetails,
  personExport,
  problem,
  publicSettings,
} from '@/test/mockApi';
import { captureDownloads } from '@/test/downloads';
import { renderInRouter } from '@/test/utils';
import { PersonPage } from './PersonPage';

afterEach(() => {
  vi.restoreAllMocks();
});

const mia = personDetails[ids.personMia]!;

const fixtures = (detail = mia) => ({
  '/api/v1/settings/public': { body: publicSettings },
  '/api/v1/auth/me': { body: meOffice },
  '/api/v1/admin/persons/{personId}': (init: MockRequest) =>
    init.params?.path?.personId === detail.id
      ? { body: detail }
      : problem(404, 'resource not found'),
  '/api/v1/admin/persons/{personId}/export': (init: MockRequest) => ({
    body: personExport({
      id: init.params?.path?.personId ?? '',
      givenName: detail.givenName,
      familyName: detail.familyName,
    }),
  }),
});

function renderPage(personId: string) {
  render(<Toaster />);
  return renderInRouter(<PersonPage personId={personId} />, { at: '/admin/persons/$personId' });
}

describe('PersonPage privacy', () => {
  it('exports the data of a person as a file', async () => {
    mockApiGet(fixtures());
    const saved = captureDownloads();
    const user = userEvent.setup();

    renderPage(mia.id);
    await user.click(await screen.findByRole('button', { name: 'Export the data of Mia Sample' }));

    await vi.waitFor(() => expect(saved).toHaveLength(1));
    expect(saved[0]?.name).toBe('data-export-mia-sample-2026-10-25.json');
    const json = JSON.parse((await saved[0]?.blob.text()) ?? '') as { format: string };
    expect(json.format).toBe('studio-schedule.person-export');
  });

  it('anonymizes a person after asking, and shows it so', async () => {
    let anonymized = false;
    const anonymizedMia = {
      ...mia,
      givenName: 'Deleted',
      familyName: 'person',
      email: undefined,
      phone: undefined,
      notes: undefined,
      dependents: [],
      account: undefined,
      anonymizedAt: '2026-10-25T10:00:00Z',
    };
    mockApiGet({
      ...fixtures(),
      '/api/v1/admin/persons/{personId}': () => ({ body: anonymized ? anonymizedMia : mia }),
    });
    const post = mockApiPost({
      '/api/v1/admin/persons/{personId}/anonymize': () => {
        anonymized = true;
        return {
          body: {
            person: { id: mia.id, givenName: 'Deleted', familyName: 'person' },
            bookingsCanceled: 1,
            waitlistWithdrawn: 0,
            membershipsEnded: 0,
            linksRemoved: 1,
            staffUnlinked: 0,
            accountDeleted: true,
          },
        };
      },
    });
    const user = userEvent.setup();

    renderPage(mia.id);
    await user.click(await screen.findByRole('button', { name: 'Anonymize Mia Sample' }));
    const dialog = within(
      await screen.findByRole('alertdialog', { name: 'Anonymize Mia Sample?' }),
    );
    expect(dialog.getByText(/Past bookings, passes and receipts stay/)).toBeInTheDocument();
    await user.click(dialog.getByRole('button', { name: 'Anonymize' }));

    expect(await screen.findByText('Mia Sample has been anonymized.')).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/admin/persons/{personId}/anonymize', {
      params: { path: { personId: mia.id } },
    });
    expect(
      await screen.findByRole('heading', { level: 1, name: 'Deleted person' }),
    ).toBeInTheDocument();
    expect(screen.getByText(/This person’s data was erased on/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /^Anonymize/ })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Export the data of Deleted person' })).toBeEnabled();
  });

  it('explains that only an admin anonymizes an admin', async () => {
    mockApiGet(fixtures());
    mockApiPost({ '/api/v1/admin/persons/{personId}/anonymize': problem(403, 'forbidden') });
    const user = userEvent.setup();

    renderPage(mia.id);
    await user.click(await screen.findByRole('button', { name: 'Anonymize Mia Sample' }));
    const dialog = within(await screen.findByRole('alertdialog'));
    await user.click(dialog.getByRole('button', { name: 'Anonymize' }));

    expect(
      await dialog.findByText(
        'Mia Sample has an administrator account; only an administrator can anonymize them.',
      ),
    ).toBeInTheDocument();
  });

  it.each([
    [
      'last-admin',
      'Mia Sample has the school’s last administrator account that can sign in. Make another account an administrator first; it counts once it is confirmed.',
    ],
    [
      'sole-guardian',
      'Mia Sample is the only guardian of a child. Link another guardian to the child, or remove the link, first.',
    ],
    [
      'in-use',
      'Receipts, the audit log, announcements or duplicate decisions name the account of Mia Sample, so it cannot be deleted. Disable the account instead.',
    ],
  ] as const)('explains a refusal (%s)', async (reason, text) => {
    mockApiGet(fixtures());
    mockApiPost({ '/api/v1/admin/persons/{personId}/anonymize': conflict(reason) });
    const user = userEvent.setup();

    renderPage(mia.id);
    await user.click(await screen.findByRole('button', { name: 'Anonymize Mia Sample' }));
    const dialog = within(await screen.findByRole('alertdialog'));
    await user.click(dialog.getByRole('button', { name: 'Anonymize' }));

    expect(await dialog.findByText(text)).toBeInTheDocument();
  });
});
