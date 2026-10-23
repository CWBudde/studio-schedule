import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import i18n from '@/i18n';
import {
  autumnClasses,
  catalogFixtures,
  ids,
  mockApiGet,
  type MockRequest,
  problem,
  publicSettings,
  springClasses,
  terms,
} from '@/test/mockApi';
import { renderAtClasses } from './renderAtClasses';
import { TimetablePage } from './TimetablePage';

/** Friday 2026-10-23, 12:00 in Berlin: in the autumn term. */
const friday = new Date('2026-10-23T10:00:00Z');

beforeEach(() => {
  // Only Date: the router and Testing Library keep their real timers.
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(friday);
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

/** Mocks the API and records the term of every classes request. */
function mockTimetable(overrides: Record<string, unknown> = {}) {
  const requested: string[] = [];
  mockApiGet({
    ...catalogFixtures,
    '/api/v1/settings/public': { body: publicSettings },
    '/api/v1/terms': { body: { items: terms } },
    '/api/v1/terms/{termId}/classes': (init: MockRequest) => {
      const termId = init.params?.path?.termId ?? '';
      requested.push(termId);
      const items =
        termId === ids.termAutumn ? autumnClasses : termId === ids.termSpring ? springClasses : [];
      return { body: { items } };
    },
    ...overrides,
  });
  return requested;
}

const termSelect = () => screen.getByRole('combobox', { name: 'Term' });

describe('TimetablePage', () => {
  it('shows the classes of the current term by weekday, Monday first', async () => {
    const requested = mockTimetable();

    renderAtClasses(<TimetablePage />);

    const monday = await screen.findByRole('region', { name: 'Monday' });
    expect(screen.getByRole('heading', { level: 1, name: 'Classes' })).toBeInTheDocument();
    expect(requested).toEqual([ids.termAutumn]);
    expect(termSelect()).toHaveTextContent('Autumn 2026');
    expect(screen.getByText('Aug 31, 2026 – Jan 29, 2027')).toBeInTheDocument();

    // The API lists Sunday first; the page shows it last.
    expect(screen.getAllByRole('heading', { level: 2 }).map((h) => h.textContent)).toEqual([
      'Monday',
      'Tuesday',
      'Sunday',
    ]);

    const ballet = within(monday).getByRole('article', {
      name: 'Ballet, Monday, 18:00–19:30',
    });
    const card = within(ballet);
    expect(card.getByRole('heading', { level: 3, name: 'Ballet' })).toBeInTheDocument();
    expect(card.getByText('18:00–19:30')).toBeInTheDocument();
    expect(card.getByText('Beginner')).toBeInTheDocument();
    expect(card.getByText('Room A · North Studio')).toBeInTheDocument();
    expect(card.getByText('Alex Example')).toBeInTheDocument();
    expect(card.getByText('accompanied by Sam Pianist')).toBeInTheDocument();

    const tuesday = within(screen.getByRole('region', { name: 'Tuesday' }));
    const contemporary = within(
      tuesday.getByRole('article', { name: 'Contemporary, Tuesday, 17:00–18:00' }),
    );
    expect(contemporary.getByText('Intermediate')).toBeInTheDocument();
    expect(contemporary.getByText('Room B · South Studio')).toBeInTheDocument();
    expect(contemporary.getByText('Riley Sample')).toBeInTheDocument();
    expect(contemporary.queryByText(/accompanied by/)).not.toBeInTheDocument();

    expect(screen.getByRole('link', { name: 'Schedule' })).toHaveAttribute('href', '/schedule');
  });

  it('takes today in the school time zone, not the browser zone', async () => {
    // 00:30 on Saturday, January 30 in Berlin, the day after the autumn term;
    // still January 29 in the browser zone (America/New_York).
    vi.setSystemTime(new Date('2027-01-29T23:30:00Z'));
    const requested = mockTimetable();

    renderAtClasses(<TimetablePage />);

    expect(await screen.findByRole('region', { name: 'Thursday' })).toBeInTheDocument();
    expect(requested).toEqual([ids.termSpring]);
    expect(termSelect()).toHaveTextContent('Spring 2027');
  });

  it('shows the term of a deep link', async () => {
    mockTimetable();

    renderAtClasses(<TimetablePage />, { term: ids.termSpring });

    const thursday = await screen.findByRole('region', { name: 'Thursday' });
    expect(
      within(thursday).getByRole('article', { name: 'Ballet, Thursday, 19:15–20:30' }),
    ).toBeInTheDocument();
    expect(within(thursday).getByText('Advanced')).toBeInTheDocument();
    expect(termSelect()).toHaveTextContent('Spring 2027');
    expect(screen.queryByRole('region', { name: 'Monday' })).not.toBeInTheDocument();
  });

  it('falls back to the current term when the linked term does not exist', async () => {
    const requested = mockTimetable();

    renderAtClasses(<TimetablePage />, { term: ids.classBallet });

    expect(await screen.findByRole('region', { name: 'Monday' })).toBeInTheDocument();
    expect(requested).toEqual([ids.termAutumn]);
    expect(termSelect()).toHaveTextContent('Autumn 2026');
  });

  it('puts the chosen term into the URL', async () => {
    const requested = mockTimetable();
    const user = userEvent.setup();
    const { router } = renderAtClasses(<TimetablePage />);
    await screen.findByRole('region', { name: 'Monday' });

    await user.click(termSelect());
    await user.click(await screen.findByRole('option', { name: 'Spring 2027' }));

    expect(await screen.findByRole('region', { name: 'Thursday' })).toBeInTheDocument();
    expect(router.state.location.search).toEqual({ term: ids.termSpring });
    expect(requested).toEqual([ids.termAutumn, ids.termSpring]);
    expect(screen.getByText('Feb 1 – Jul 16, 2027')).toBeInTheDocument();
  });

  it('says so when no term is planned', async () => {
    const requested = mockTimetable({ '/api/v1/terms': { body: { items: [] } } });

    renderAtClasses(<TimetablePage />);

    expect(await screen.findByText('No terms yet')).toBeInTheDocument();
    expect(screen.queryByRole('combobox', { name: 'Term' })).not.toBeInTheDocument();
    expect(requested).toEqual([]);
  });

  it('says so when the term has no classes', async () => {
    mockTimetable();

    renderAtClasses(<TimetablePage />, { term: ids.termPast });

    expect(await screen.findByText('No classes in this term')).toBeInTheDocument();
    expect(termSelect()).toHaveTextContent('School Year 2025/26');
    expect(screen.queryByRole('heading', { level: 2 })).not.toBeInTheDocument();
  });

  it('offers a retry when the terms cannot be loaded', async () => {
    let fail = true;
    mockTimetable({
      '/api/v1/terms': () => (fail ? problem(500, 'boom') : { body: { items: terms } }),
    });
    const user = userEvent.setup();

    renderAtClasses(<TimetablePage />);

    expect(await screen.findByText('Could not load the classes.')).toBeInTheDocument();
    fail = false;
    await user.click(screen.getByRole('button', { name: 'Retry' }));

    expect(await screen.findByRole('region', { name: 'Monday' })).toBeInTheDocument();
  });

  it('offers a retry when the classes or the catalogue cannot be loaded', async () => {
    let fail = true;
    mockTimetable({
      '/api/v1/rooms': () => (fail ? problem(500, 'boom') : { body: { items: [] } }),
    });
    const user = userEvent.setup();

    renderAtClasses(<TimetablePage />);

    expect(await screen.findByText('Could not load the classes.')).toBeInTheDocument();
    expect(termSelect()).toHaveTextContent('Autumn 2026');
    fail = false;
    await user.click(screen.getByRole('button', { name: 'Retry' }));

    // Without its room, a class still shows, its place unknown.
    const monday = within(await screen.findByRole('region', { name: 'Monday' }));
    expect(monday.getByText('Unknown')).toBeInTheDocument();
  });

  it('speaks German', async () => {
    mockTimetable();
    await i18n.changeLanguage('de');

    renderAtClasses(<TimetablePage />);

    const monday = await screen.findByRole('region', { name: 'Montag' });
    expect(screen.getByRole('heading', { level: 1, name: 'Kurse' })).toBeInTheDocument();
    expect(screen.getByRole('combobox', { name: 'Kurszeitraum' })).toBeInTheDocument();
    expect(within(monday).getByText('Anfänger')).toBeInTheDocument();
    expect(within(monday).getByText('Korrepetition: Sam Pianist')).toBeInTheDocument();
  });
});
