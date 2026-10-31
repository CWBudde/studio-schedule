import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { toast } from 'sonner';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Toaster } from '@/components/ui/sonner';
import i18n from '@/i18n';
import {
  meAdmin,
  meOffice,
  type MockRequest,
  mockApiGet,
  mockApiPut,
  problem,
  publicSettings,
  receiptSettings,
} from '@/test/mockApi';
import { renderWithQuery } from '@/test/utils';
import { SettingsPage } from './SettingsPage';

afterEach(() => {
  vi.restoreAllMocks();
  // Every card says the same when it saves: no toast may outlive its test.
  toast.dismiss();
});

const school = {
  schoolName: 'Example Dance Studio',
  legalNotice: 'Example Dance Studio · 1 Example Street · Sampletown',
};
const branding = { minContrast: 4.5, hasLogo: false, hasBackground: false, theme: 'programme' };
const regional = {
  timezone: 'Europe/Berlin',
  defaultLocale: 'en',
  currency: 'EUR',
  timezoneLocked: false,
  currencyLocked: false,
};
// Sunday 18:00, two weeks ahead; Wednesday, October 21st, 2026 at noon in Berlin.
const window = {
  now: '2026-10-21T10:00:00Z',
  lastReleaseAt: '2026-10-18T16:00:00Z',
  nextReleaseAt: '2026-10-25T17:00:00Z',
  releasedUntil: '2026-11-01T23:00:00Z',
  ruleReleasedUntil: '2026-11-01T23:00:00Z',
};
const rules = {
  releaseWeekday: 0,
  releaseTime: '18:00',
  releaseWeeksAhead: 2,
  bookingCutoffMinutes: 0,
  cancellationCutoffMinutes: 120,
  waitlistInviteExpiryMinutes: 1440,
  defaultVisibleDays: 14,
  window,
  generatedLessons: 0,
};

const fixtures = (me = meAdmin, overrides: Record<string, unknown> = {}) => ({
  '/api/v1/auth/me': { body: me },
  '/api/v1/settings/public': { body: { ...publicSettings, hasLogo: false } },
  '/api/v1/admin/settings/school': { body: school },
  '/api/v1/admin/settings/branding': { body: branding },
  '/api/v1/admin/settings/regional': { body: regional },
  '/api/v1/admin/settings/booking-rules': { body: rules },
  '/api/v1/admin/settings/booking-rules/preview': { body: window },
  '/api/v1/admin/settings/passes': { body: { redemptionMode: 'attendance' } },
  '/api/v1/admin/settings/receipts': { body: receiptSettings },
  ...overrides,
});

function renderPage() {
  render(<Toaster />);
  return renderWithQuery(<SettingsPage />);
}

const card = async (name: string) => within(await screen.findByRole('region', { name }));
const form = async (name: string) => within(await (await card(name)).findByRole('form', { name }));

describe('School settings', () => {
  it('lets an admin rename the school and change the legal notice', async () => {
    mockApiGet(fixtures());
    const put = mockApiPut({ '/api/v1/admin/settings/school': ({ body }) => ({ body }) });
    const user = userEvent.setup();

    renderPage();
    const f = await form('School');
    expect(f.getByLabelText('School name')).toHaveValue('Example Dance Studio');
    await user.clear(f.getByLabelText('School name'));
    await user.type(f.getByLabelText('School name'), '  Sample Ballet Academy ');
    await user.clear(f.getByLabelText('Legal notice (optional)'));
    await user.click(f.getByRole('button', { name: 'Save changes' }));

    expect(await screen.findByText('The settings have been saved.')).toBeInTheDocument();
    expect(put).toHaveBeenCalledWith('/api/v1/admin/settings/school', {
      body: { schoolName: 'Sample Ballet Academy' },
    });
  });

  it('needs a name', async () => {
    mockApiGet(fixtures());
    const put = mockApiPut({});
    const user = userEvent.setup();

    renderPage();
    const f = await form('School');
    await user.clear(f.getByLabelText('School name'));
    await user.click(f.getByRole('button', { name: 'Save changes' }));

    expect(f.getByLabelText('School name')).toHaveAccessibleDescription(
      'Please fill in this field.',
    );
    expect(put).not.toHaveBeenCalled();
  });

  it('shows the office the profile without a way to change it', async () => {
    mockApiGet(fixtures(meOffice));

    renderPage();

    const c = await card('School');
    expect(await c.findByText(school.legalNotice)).toBeInTheDocument();
    expect(c.getByText('Only administrators can change this.')).toBeInTheDocument();
    expect(c.queryByRole('form')).not.toBeInTheDocument();
  });

  it('says when the settings cannot be loaded', async () => {
    mockApiGet(fixtures(meAdmin, { '/api/v1/admin/settings/school': problem(500, 'boom') }));

    renderPage();

    expect(
      await (await card('School')).findByText('The settings could not be loaded.'),
    ).toBeInTheDocument();
  });
});

describe('Branding', () => {
  it('checks the contrast while the accent is typed and saves it', async () => {
    mockApiGet(fixtures());
    const put = mockApiPut({
      '/api/v1/admin/settings/branding': ({ body }) => ({
        body: { ...branding, ...(body as object), accentColorDark: '#97b7f8' },
      }),
    });
    const user = userEvent.setup();

    renderPage();
    const f = await form('Branding');
    const accent = f.getByLabelText('Accent color');
    expect(accent).toHaveAccessibleDescription(
      "As #rrggbb. Leave it empty for the app's own color.",
    );

    await user.type(accent, '#ff0000');
    expect(accent).toHaveAccessibleDescription(
      expect.stringContaining('Contrast 3.75:1 with the text on it – below WCAG AA (4.5:1)'),
    );
    await user.click(f.getByRole('button', { name: 'Save changes' }));
    expect(accent).toHaveAccessibleDescription(
      expect.stringContaining('This color is too light for the text on it; choose a darker one.'),
    );
    expect(put).not.toHaveBeenCalled();

    await user.clear(accent);
    await user.type(accent, '#1D4ED8');
    expect(accent).toHaveAccessibleDescription(
      'Contrast 6.28:1 with the text on it – meets WCAG AA',
    );
    expect(f.getByText('Light')).toBeInTheDocument();
    expect(f.getByText('Dark')).toBeInTheDocument();
    await user.click(f.getByRole('button', { name: 'Save changes' }));

    expect(await screen.findByText('The settings have been saved.')).toBeInTheDocument();
    expect(put).toHaveBeenCalledWith('/api/v1/admin/settings/branding', {
      body: { accentColor: '#1d4ed8' },
    });

    await user.click(f.getByRole('button', { name: "Use the app's own color" }));
    expect(accent).toHaveValue('');
    await user.click(f.getByRole('button', { name: 'Save changes' }));
    await waitFor(() =>
      expect(put).toHaveBeenLastCalledWith('/api/v1/admin/settings/branding', { body: {} }),
    );
  });

  it('marks the accent the server refuses', async () => {
    mockApiGet(fixtures());
    mockApiPut({
      '/api/v1/admin/settings/branding': problem(422, 'validation failed', [
        { location: 'body.accentColor', message: 'too light' },
      ]),
    });
    const user = userEvent.setup();

    renderPage();
    const f = await form('Branding');
    await user.type(f.getByLabelText('Accent color'), '#0f766e');
    await user.click(f.getByRole('button', { name: 'Save changes' }));

    expect(await f.findByRole('alert')).toHaveTextContent('Please check your entries.');
    expect(f.getByLabelText('Accent color')).toHaveAccessibleDescription(
      expect.stringContaining('Please check this entry.'),
    );
  });

  it('shows the office the accent and the logo', async () => {
    mockApiGet(
      fixtures(meOffice, {
        '/api/v1/admin/settings/branding': {
          body: { ...branding, accentColor: '#0f766e', accentColorDark: '#72cac0', hasLogo: true },
        },
        '/api/v1/settings/logo': { body: new Blob(['png'], { type: 'image/png' }) },
      }),
    );

    renderPage();

    const c = await card('Branding');
    expect(await c.findByText('#0f766e')).toBeInTheDocument();
    expect(await c.findByRole('img', { name: "The school's logo" })).toHaveAttribute(
      'src',
      `data:image/png;base64,${btoa('png')}`,
    );
    expect(c.queryByRole('form')).not.toBeInTheDocument();
  });
});

describe('Time and language', () => {
  it('lets an admin change the language, zone and currency before the first lessons', async () => {
    mockApiGet(fixtures());
    const put = mockApiPut({
      '/api/v1/admin/settings/regional': ({ body }) => ({
        body: { ...regional, ...(body as object) },
      }),
    });
    const user = userEvent.setup();

    renderPage();
    const f = await form('Time and language');
    await user.clear(f.getByLabelText('Time zone'));
    await user.type(f.getByLabelText('Time zone'), 'Mars/Olympus');
    await user.click(f.getByRole('button', { name: 'Save changes' }));
    expect(f.getByLabelText('Time zone')).toHaveAccessibleDescription(
      expect.stringContaining('Enter a time zone such as Europe/Berlin.'),
    );

    await user.clear(f.getByLabelText('Time zone'));
    await user.type(f.getByLabelText('Time zone'), 'Europe/Vienna');
    await user.click(f.getByRole('combobox', { name: 'Default language' }));
    await user.click(await screen.findByRole('option', { name: 'Deutsch' }));
    await user.clear(f.getByLabelText('Currency'));
    await user.type(f.getByLabelText('Currency'), 'chf');
    await user.click(f.getByRole('button', { name: 'Save changes' }));

    expect(await screen.findByText('The settings have been saved.')).toBeInTheDocument();
    expect(put).toHaveBeenCalledWith('/api/v1/admin/settings/regional', {
      body: { timezone: 'Europe/Vienna', defaultLocale: 'de', currency: 'CHF' },
    });
  });

  it('says why the time zone and the currency are fixed', async () => {
    mockApiGet(
      fixtures(meAdmin, {
        '/api/v1/admin/settings/regional': {
          body: { ...regional, timezoneLocked: true, currencyLocked: true },
        },
      }),
    );

    renderPage();
    const f = await form('Time and language');

    expect(f.getByLabelText('Time zone')).toHaveAttribute('readonly');
    expect(f.getByLabelText('Time zone')).toHaveAccessibleDescription(
      'Fixed: lessons exist, whose times are in this time zone. It is chosen when the school is set up.',
    );
    expect(f.getByLabelText('Currency')).toHaveAttribute('readonly');
    expect(f.getByLabelText('Currency')).toHaveAccessibleDescription(
      'Fixed: prices or receipts exist in this currency. It is chosen when the school is set up.',
    );
  });

  it('explains a time zone that got fixed meanwhile', async () => {
    mockApiGet(fixtures());
    mockApiPut({
      '/api/v1/admin/settings/regional': {
        status: 409,
        body: { title: 'Conflict', status: 409, reason: 'timezone-locked' },
      },
    });
    const user = userEvent.setup();

    renderPage();
    const f = await form('Time and language');
    await user.clear(f.getByLabelText('Time zone'));
    await user.type(f.getByLabelText('Time zone'), 'Europe/Vienna');
    await user.click(f.getByRole('button', { name: 'Save changes' }));

    expect(await f.findByRole('alert')).toHaveTextContent(
      'Lessons exist meanwhile, so the time zone can no longer change.',
    );
  });
});

describe('Booking rules', () => {
  it('shows the durations in their units and the release window', async () => {
    mockApiGet(fixtures());

    renderPage();
    const f = await form('Booking rules');

    expect(f.getByRole('combobox', { name: 'Release day' })).toHaveTextContent('Sunday');
    expect(f.getByLabelText('Release time')).toHaveValue('18:00');
    expect(f.getByLabelText('Canceling closes before the start')).toHaveValue('2');
    expect(
      f.getByRole('combobox', { name: 'Unit of Canceling closes before the start' }),
    ).toHaveTextContent('hours');
    expect(
      f.getByRole('combobox', { name: 'Unit of A waiting-list invitation holds the place for' }),
    ).toHaveTextContent('days');
    // In the school's zone (Berlin), whatever the browser's.
    expect(await f.findByText('Sun, Oct 25, 18:00')).toBeInTheDocument();
    expect(f.getByText('Sunday, November 1')).toBeInTheDocument();
  });

  it('previews the window of the rule being typed and saves the rules', async () => {
    const get = mockApiGet(
      fixtures(meAdmin, {
        '/api/v1/admin/settings/booking-rules/preview': ({ params }: MockRequest) => ({
          body:
            params?.query?.releaseWeeksAhead === 4
              ? { ...window, releasedUntil: '2026-11-15T23:00:00Z' }
              : window,
        }),
      }),
    );
    const put = mockApiPut({
      '/api/v1/admin/settings/booking-rules': ({ body }) => ({
        body: { ...rules, ...(body as object), generatedLessons: 12 },
      }),
    });
    const user = userEvent.setup();

    renderPage();
    const f = await form('Booking rules');
    await user.clear(f.getByLabelText('Weeks released ahead'));
    await user.type(f.getByLabelText('Weeks released ahead'), '4');

    expect(await f.findByText('Sunday, November 15')).toBeInTheDocument();
    expect(get).toHaveBeenCalledWith('/api/v1/admin/settings/booking-rules/preview', {
      params: { query: { releaseWeekday: 0, releaseTime: '18:00', releaseWeeksAhead: 4 } },
      signal: expect.any(AbortSignal),
    });

    await user.clear(f.getByLabelText('Booking closes before the start'));
    await user.type(f.getByLabelText('Booking closes before the start'), '30');
    await user.click(f.getByRole('button', { name: 'Save changes' }));

    expect(
      await screen.findByText(
        'The settings have been saved. 12 lessons of the wider window were created.',
      ),
    ).toBeInTheDocument();
    expect(put).toHaveBeenCalledWith('/api/v1/admin/settings/booking-rules', {
      body: {
        releaseWeekday: 0,
        releaseTime: '18:00',
        releaseWeeksAhead: 4,
        bookingCutoffMinutes: 30,
        cancellationCutoffMinutes: 120,
        waitlistInviteExpiryMinutes: 1440,
        defaultVisibleDays: 14,
      },
    });
  });

  it('says when the rule being typed is incomplete instead of loading forever', async () => {
    mockApiGet(fixtures());
    const user = userEvent.setup();

    renderPage();
    const f = await form('Booking rules');
    expect(await f.findByText('Sun, Oct 25, 18:00')).toBeInTheDocument();

    await user.clear(f.getByLabelText('Weeks released ahead'));
    await user.type(f.getByLabelText('Weeks released ahead'), '60');

    expect(await f.findByText(/^Rule incomplete/)).toBeInTheDocument();
    expect(f.queryByRole('status')).not.toBeInTheDocument();
    expect(f.queryByText('Sun, Oct 25, 18:00')).not.toBeInTheDocument();

    await user.clear(f.getByLabelText('Weeks released ahead'));
    await user.type(f.getByLabelText('Weeks released ahead'), '3');
    expect(await f.findByText('Sun, Oct 25, 18:00')).toBeInTheDocument();
  });

  it('says when the window of a rule cannot be previewed', async () => {
    mockApiGet(
      fixtures(meAdmin, {
        '/api/v1/admin/settings/booking-rules/preview': problem(500, 'internal server error'),
      }),
    );
    const user = userEvent.setup();

    renderPage();
    const f = await form('Booking rules');
    await user.clear(f.getByLabelText('Weeks released ahead'));
    await user.type(f.getByLabelText('Weeks released ahead'), '3');

    expect(await f.findByText('The release window could not be computed.')).toBeInTheDocument();
  });

  it('checks the ranges, and marks what the server refuses', async () => {
    mockApiGet(fixtures());
    const put = mockApiPut({
      '/api/v1/admin/settings/booking-rules': problem(422, 'validation failed', [
        { location: 'body.waitlistInviteExpiryMinutes', message: 'out of range' },
      ]),
    });
    const user = userEvent.setup();

    renderPage();
    const f = await form('Booking rules');
    const invite = f.getByLabelText('A waiting-list invitation holds the place for');
    await user.clear(invite);
    await user.type(invite, '31');
    await user.click(f.getByRole('button', { name: 'Save changes' }));
    expect(invite).toHaveAccessibleDescription(
      expect.stringContaining('at least 1 minute and at most 30 days'),
    );
    expect(put).not.toHaveBeenCalled();

    await user.clear(invite);
    await user.type(invite, '2');
    await user.click(f.getByRole('button', { name: 'Save changes' }));
    expect(await f.findByRole('alert')).toHaveTextContent('Please check your entries.');
    expect(invite).toHaveAccessibleDescription(expect.stringContaining('Please check this entry.'));
  });

  it('previews and saves a released-through date beyond the weekly release', async () => {
    const get = mockApiGet(
      fixtures(meAdmin, {
        '/api/v1/admin/settings/booking-rules/preview': ({ params }: MockRequest) => ({
          body:
            params?.query?.releasedThrough === '2026-11-20'
              ? { ...window, releasedUntil: '2026-11-20T23:00:00Z' }
              : window,
        }),
      }),
    );
    const put = mockApiPut({
      '/api/v1/admin/settings/booking-rules': ({ body }) => ({
        body: { ...rules, ...(body as object), generatedLessons: 3 },
      }),
    });
    const user = userEvent.setup();

    renderPage();
    const f = await form('Booking rules');
    const date = f.getByLabelText('Also release through');
    // Without a date, the hint names where the weekly release alone reaches.
    expect(
      await f.findByText(/The weekly release alone reaches through Sunday, November 1\./),
    ).toBeInTheDocument();
    // 52 weeks after the server's today in the school's zone.
    expect(date).toHaveAttribute('max', '2027-10-20');

    await user.type(date, '2026-11-20');

    expect(await f.findByText('Friday, November 20')).toBeInTheDocument();
    expect(date).toHaveAccessibleDescription(
      expect.stringContaining(
        'This date extends the release: the weekly release alone reaches through Sunday, November 1.',
      ),
    );
    expect(f.getByText('Weekly release alone through')).toBeInTheDocument();
    expect(get).toHaveBeenCalledWith('/api/v1/admin/settings/booking-rules/preview', {
      params: {
        query: {
          releaseWeekday: 0,
          releaseTime: '18:00',
          releaseWeeksAhead: 2,
          releasedThrough: '2026-11-20',
        },
      },
      signal: expect.any(AbortSignal),
    });

    await user.click(f.getByRole('button', { name: 'Save changes' }));
    expect(
      await screen.findByText(
        'The settings have been saved. 3 lessons of the wider window were created.',
      ),
    ).toBeInTheDocument();
    expect(put).toHaveBeenCalledWith('/api/v1/admin/settings/booking-rules', {
      body: expect.objectContaining({ releasedThrough: '2026-11-20' }),
    });
  });

  it('says when the released-through date has no effect, and clears it', async () => {
    mockApiGet(
      fixtures(meAdmin, {
        '/api/v1/admin/settings/booking-rules': {
          body: { ...rules, releasedThrough: '2026-10-28' },
        },
      }),
    );
    const put = mockApiPut({
      '/api/v1/admin/settings/booking-rules': ({ body }) => ({
        body: { ...rules, ...(body as object) },
      }),
    });
    const user = userEvent.setup();

    renderPage();
    const f = await form('Booking rules');
    const date = f.getByLabelText('Also release through');
    expect(date).toHaveValue('2026-10-28');
    expect(
      await f.findByText(
        /This date has no effect now: the weekly release already reaches through Sunday, November 1\./,
      ),
    ).toBeInTheDocument();

    await user.click(f.getByRole('button', { name: 'Clear the date' }));
    expect(date).toHaveValue('');
    await user.click(f.getByRole('button', { name: 'Save changes' }));

    await waitFor(() => expect(put).toHaveBeenCalled());
    expect(put.mock.calls[0]?.[1]).toEqual({
      body: expect.not.objectContaining({ releasedThrough: expect.anything() }),
    });
  });

  it('refuses a released-through date more than 52 weeks ahead', async () => {
    mockApiGet(fixtures());
    const put = mockApiPut({ '/api/v1/admin/settings/booking-rules': { body: rules } });
    const user = userEvent.setup();

    renderPage();
    const f = await form('Booking rules');
    const date = f.getByLabelText('Also release through');
    await waitFor(() => expect(date).toHaveAttribute('max', '2027-10-20'));
    await user.type(date, '2027-10-21');
    await user.click(f.getByRole('button', { name: 'Save changes' }));

    expect(date).toHaveAccessibleDescription(
      expect.stringContaining('Choose a day at most 52 weeks from today.'),
    );
    expect(put).not.toHaveBeenCalled();
  });

  it('shows the office the rules and the window', async () => {
    mockApiGet(fixtures(meOffice));

    renderPage();

    const c = await card('Booking rules');
    expect(await c.findByText('2 hours')).toBeInTheDocument();
    expect(c.getByText('1 day')).toBeInTheDocument();
    expect(await c.findByText('Sunday, November 1')).toBeInTheDocument();
    expect(c.getByText('Also release through').nextElementSibling).toHaveTextContent('—');
    expect(c.queryByRole('form')).not.toBeInTheDocument();
  });

  it('shows the office the released-through date and where the weekly release alone reaches', async () => {
    mockApiGet(
      fixtures(meOffice, {
        '/api/v1/admin/settings/booking-rules': {
          body: {
            ...rules,
            releasedThrough: '2026-11-20',
            window: { ...window, releasedUntil: '2026-11-20T23:00:00Z' },
          },
        },
      }),
    );

    renderPage();

    const c = await card('Booking rules');
    expect(
      (await c.findByText('Weekly release alone through')).nextElementSibling,
    ).toHaveTextContent('Sunday, November 1');
    expect(c.getByText('Also release through').nextElementSibling).toHaveTextContent(
      'Friday, November 20',
    );
    expect(c.getByText('Released through').nextElementSibling).toHaveTextContent(
      'Friday, November 20',
    );
  });

  it('renders in German', async () => {
    mockApiGet(fixtures(meOffice));
    await i18n.changeLanguage('de');

    renderPage();

    const c = await card('Buchungsregeln');
    expect(await c.findByText('2 Stunden')).toBeInTheDocument();
    expect(await c.findByText('Sonntag, 1. November')).toBeInTheDocument();
    expect(await screen.findByRole('region', { name: 'Zeit und Sprache' })).toBeInTheDocument();
  });
});
