import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Theme } from '@/themes/slots';
import { ThemeContext } from '@/themes/themeContext';
import { Home as WerkstattHome } from '@/themes/werkstatt/Home';
import {
  family,
  healthOk,
  ids,
  lessonBallet,
  lessonContemporary,
  lessonWorkshop,
  meMember,
  meOffice,
  mockApiGet,
  publicSettings,
  schedule,
  signedOut,
} from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { HomePage } from './HomePage';

afterEach(() => {
  vi.restoreAllMocks();
});

describe('HomePage', () => {
  it('shows an OK badge and public settings when the backend is healthy', async () => {
    mockApiGet({
      '/api/v1/health': { body: healthOk },
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/schedule': { body: schedule },
    });

    renderInRouter(<HomePage />);

    const status = within(await screen.findByRole('region', { name: 'System status' }));

    expect(await status.findByText('OK')).toBeInTheDocument();
    expect(status.getByText('Europe/Berlin')).toBeInTheDocument();
    // The version is no public information (ASVS 14.3.3).
    expect(status.queryByText('Version')).not.toBeInTheDocument();
    expect(status.getByText('Example Dance Studio')).toBeInTheDocument();
    expect(
      screen.getByRole('heading', { level: 1, name: 'Example Dance Studio' }),
    ).toBeInTheDocument();
  });

  it.each([
    ['the office', { ...meOffice, version: '0.1.0-test' }, true],
    ['a member', meMember, false],
  ])('shows the version to %s only if the API names it', async (_, me, shown) => {
    mockApiGet({
      '/api/v1/health': { body: healthOk },
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/schedule': { body: schedule },
      '/api/v1/auth/me': { body: me },
    });

    renderInRouter(<HomePage />);

    const status = within(await screen.findByRole('region', { name: 'System status' }));
    expect(await status.findByText('Europe/Berlin')).toBeInTheDocument();
    if (shown) expect(await status.findByText('0.1.0-test')).toBeInTheDocument();
    else expect(status.queryByText('Version')).not.toBeInTheDocument();
  });

  it('teases the next lessons and links them into the schedule', async () => {
    mockApiGet({
      '/api/v1/health': { body: healthOk },
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/schedule': { body: schedule },
    });

    renderInRouter(<HomePage />);

    const teaser = within(await screen.findByRole('region', { name: 'Next lessons' }));
    const ballet = await teaser.findByRole('link', { name: /Ballet/ });
    // Times are in the school time zone (Berlin), not the browser's.
    expect(ballet).toHaveTextContent('Mon, Oct 26, 18:00');
    expect(ballet).toHaveAttribute('href', `/schedule?from=2026-10-26&lesson=${ids.lessonBallet}`);
    expect(teaser.getByRole('link', { name: /Jazz/ })).toBeInTheDocument();
    // Canceled lessons are left out of the teaser.
    expect(teaser.queryByText('Contemporary')).not.toBeInTheDocument();
    expect(teaser.getByRole('link', { name: /Full schedule/ })).toHaveAttribute(
      'href',
      '/schedule',
    );
  });

  it('says so when no lessons are coming up', async () => {
    mockApiGet({
      '/api/v1/health': { body: healthOk },
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/schedule': { body: { ...schedule, lessons: [] } },
    });

    renderInRouter(<HomePage />);

    expect(await screen.findByText('No lessons in the next 7 days.')).toBeInTheDocument();
  });

  it('shows an error badge when the health check fails', async () => {
    mockApiGet({
      '/api/v1/health': { status: 503, body: { message: 'down' } },
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/schedule': { body: schedule },
    });

    renderInRouter(<HomePage />);

    expect(await screen.findByText('Error')).toBeInTheDocument();
    expect(screen.getByText('Could not load data from the backend.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument();
  });
});

/** A theme whose home shows the member's own lessons (Werkstatt's, without its CSS). */
const withMyLessons: Theme = { id: 'werkstatt', slots: { Home: WerkstattHome } };

const nora = { personId: meMember.personId, name: 'Nora Example' };

describe('HomePage with the member’s own lessons', () => {
  it('shows the lessons the member’s family is booked for, by day, and opens one in the schedule', async () => {
    mockApiGet({
      '/api/v1/health': { body: healthOk },
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/auth/me': { body: meMember },
      '/api/v1/family': { body: family },
      '/api/v1/schedule': {
        body: {
          ...schedule,
          lessons: [
            { ...lessonBallet, bookedFor: [{ ...nora, bookingId: 'b-1' }] },
            // A canceled lesson they are booked for stays, so they learn it is off.
            { ...lessonContemporary, bookedFor: [{ ...nora, bookingId: 'b-2' }] },
            // Not booked: only the full schedule shows it.
            lessonWorkshop,
          ],
        },
      },
    });

    const { router } = renderInRouter(
      <ThemeContext.Provider value={withMyLessons}>
        <HomePage />
      </ThemeContext.Provider>,
    );

    const mine = within(await screen.findByRole('region', { name: 'My schedule' }));
    const monday = within(await mine.findByRole('region', { name: /Monday, October 26/ }));
    const ballet = monday.getByRole('button', { name: /Ballet/ });
    expect(
      within(mine.getByRole('region', { name: /Tuesday, October 27/ })).getByRole('button', {
        name: /Contemporary.*Canceled/,
      }),
    ).toBeInTheDocument();
    expect(mine.queryByRole('button', { name: /Jazz/ })).not.toBeInTheDocument();
    // Signed in, the teaser gives way to the member's own lessons.
    expect(screen.queryByRole('region', { name: 'Next lessons' })).not.toBeInTheDocument();

    await userEvent.click(ballet);
    expect(router.state.location.pathname).toBe('/schedule');
    expect(router.state.location.search).toEqual({ from: '2026-10-26', lesson: ids.lessonBallet });
  });

  it('leaves out the lessons that have ended', async () => {
    mockApiGet({
      '/api/v1/health': { body: healthOk },
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/auth/me': { body: meMember },
      '/api/v1/family': { body: family },
      '/api/v1/schedule': {
        body: {
          ...schedule,
          // Monday evening after the ballet class (17:00–18:30 UTC).
          now: '2026-10-26T19:00:00Z',
          lessons: [
            { ...lessonBallet, bookedFor: [{ ...nora, bookingId: 'b-1' }] },
            { ...lessonWorkshop, bookedFor: [{ ...nora, bookingId: 'b-3' }] },
          ],
        },
      },
    });

    renderInRouter(
      <ThemeContext.Provider value={withMyLessons}>
        <HomePage />
      </ThemeContext.Provider>,
    );

    const mine = within(await screen.findByRole('region', { name: 'My schedule' }));
    expect(await mine.findByRole('button', { name: /Jazz/ })).toBeInTheDocument();
    expect(mine.queryByRole('button', { name: /Ballet/ })).not.toBeInTheDocument();
    expect(mine.queryByRole('region', { name: /October 26/ })).not.toBeInTheDocument();
  });

  it('shows the next lessons to a visitor who is signed out, and does not ask for theirs', async () => {
    const get = mockApiGet({
      '/api/v1/health': { body: healthOk },
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/schedule': { body: schedule },
      ...signedOut,
    });

    renderInRouter(
      <ThemeContext.Provider value={withMyLessons}>
        <HomePage />
      </ThemeContext.Provider>,
    );

    const teaser = within(await screen.findByRole('region', { name: 'Next lessons' }));
    expect(await teaser.findByRole('link', { name: /Ballet/ })).toBeInTheDocument();
    expect(screen.queryByRole('heading', { name: 'My schedule' })).not.toBeInTheDocument();
    // Only the teaser asks for the schedule; the card's family is never needed.
    const paths = (get.mock.calls as unknown[][]).map((call) => call[0]);
    expect(paths.filter((path) => path === '/api/v1/schedule')).toHaveLength(1);
    expect(paths).not.toContain('/api/v1/family');
  });
});
