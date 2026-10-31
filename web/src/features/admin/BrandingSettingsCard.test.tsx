import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { toast } from 'sonner';
import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  type BrandingSettings,
  brandingSettingsQuery,
  publicSettingsQuery,
  schoolBackgroundQuery,
} from '@/api/queries';
import { Toaster } from '@/components/ui/sonner';
import i18n from '@/i18n';
import {
  meAdmin,
  meOffice,
  mockApiDelete,
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
const branding: BrandingSettings = {
  minContrast: 4.5,
  hasLogo: false,
  hasBackground: false,
  theme: 'programme',
};
const regional = {
  timezone: 'Europe/Berlin',
  defaultLocale: 'en',
  currency: 'EUR',
  timezoneLocked: false,
  currencyLocked: false,
};
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

const backgroundPath = '/api/v1/admin/settings/branding/background';
const background = { body: new Blob(['jpg'], { type: 'image/jpeg' }) };

const fixtures = (
  me = meAdmin,
  settings: Partial<BrandingSettings> = {},
  overrides: Record<string, unknown> = {},
) => ({
  '/api/v1/auth/me': { body: me },
  '/api/v1/settings/public': { body: { ...publicSettings, hasLogo: false } },
  '/api/v1/admin/settings/school': { body: school },
  '/api/v1/admin/settings/branding': { body: { ...branding, ...settings } },
  '/api/v1/admin/settings/regional': { body: regional },
  '/api/v1/admin/settings/booking-rules': { body: rules },
  '/api/v1/admin/settings/booking-rules/preview': { body: window },
  '/api/v1/admin/settings/passes': { body: { redemptionMode: 'attendance' } },
  '/api/v1/admin/settings/receipts': { body: receiptSettings },
  '/api/v1/settings/background': background,
  ...overrides,
});

function renderPage() {
  render(<Toaster />);
  const rendered = renderWithQuery(<SettingsPage />);
  const invalidate = vi.spyOn(rendered.queryClient, 'invalidateQueries');
  return { ...rendered, invalidate };
}

const card = async (name = 'Branding') => within(await screen.findByRole('region', { name }));
const form = async (name = 'Branding') =>
  within(await (await card(name)).findByRole('form', { name }));

/** Expects the queries that show the background to be refetched. */
async function expectBackgroundRefreshed(invalidate: ReturnType<typeof renderPage>['invalidate']) {
  for (const queryKey of [
    publicSettingsQuery.queryKey,
    brandingSettingsQuery.queryKey,
    schoolBackgroundQuery.queryKey,
  ]) {
    await waitFor(() => expect(invalidate).toHaveBeenCalledWith({ queryKey }));
  }
}

const hint =
  'A JPEG, PNG or WebP of at most 1 MB. Werkstatt shows it behind the page; Programme does not.';
const invalid = 'Choose a JPEG, PNG or WebP image of at most 1 MB.';
const jpeg = () => new File(['jpg'], 'bg.jpg', { type: 'image/jpeg' });

describe('Theme', () => {
  it('lets an admin choose Werkstatt, describing each theme', async () => {
    mockApiGet(fixtures());
    const put = mockApiPut({
      '/api/v1/admin/settings/branding': ({ body }) => ({
        body: { ...branding, ...(body as object) },
      }),
    });
    const user = userEvent.setup();

    const { invalidate } = renderPage();
    const f = await form();
    const theme = f.getByRole('combobox', { name: 'Theme' });
    expect(theme).toHaveTextContent('Programme');
    expect(theme).toHaveAccessibleDescription('Clear cards on a calm page, in the accent color.');

    await user.click(theme);
    expect(await screen.findByRole('option', { name: 'Programme' })).toBeInTheDocument();
    await user.click(await screen.findByRole('option', { name: 'Werkstatt' }));
    expect(theme).toHaveTextContent('Werkstatt');
    expect(theme).toHaveAccessibleDescription(
      "Condensed type, colored day bands and the rooms side by side, on the school's background image.",
    );

    await user.click(f.getByRole('button', { name: 'Save changes' }));
    expect(await screen.findByText('The settings have been saved.')).toBeInTheDocument();
    expect(put).toHaveBeenCalledWith('/api/v1/admin/settings/branding', {
      body: { theme: 'werkstatt' },
    });
    await waitFor(() =>
      expect(invalidate).toHaveBeenCalledWith({ queryKey: publicSettingsQuery.queryKey }),
    );
  });

  it('keeps the stored theme when only the accent changes', async () => {
    mockApiGet(fixtures(meAdmin, { theme: 'werkstatt' }));
    const put = mockApiPut({
      '/api/v1/admin/settings/branding': ({ body }) => ({
        body: { ...branding, theme: 'werkstatt', ...(body as object) },
      }),
    });
    const user = userEvent.setup();

    renderPage();
    const f = await form();
    expect(f.getByRole('combobox', { name: 'Theme' })).toHaveTextContent('Werkstatt');
    await user.type(f.getByLabelText('Accent color'), '#1d4ed8');
    await user.click(f.getByRole('button', { name: 'Save changes' }));

    expect(await screen.findByText('The settings have been saved.')).toBeInTheDocument();
    expect(put).toHaveBeenCalledWith('/api/v1/admin/settings/branding', {
      body: { accentColor: '#1d4ed8' },
    });
  });

  it("keeps another admin's theme that arrives while the form is open", async () => {
    mockApiGet(fixtures());
    const put = mockApiPut({
      '/api/v1/admin/settings/branding': ({ body }) => ({
        body: { ...branding, theme: 'werkstatt', ...(body as object) },
      }),
    });
    const user = userEvent.setup();

    const { queryClient } = renderPage();
    const f = await form();
    expect(f.getByRole('combobox', { name: 'Theme' })).toHaveTextContent('Programme');
    // Another admin picks Werkstatt; a refetch (as after a background upload) brings it in.
    mockApiGet(fixtures(meAdmin, { theme: 'werkstatt' }));
    await queryClient.refetchQueries({ queryKey: brandingSettingsQuery.queryKey });
    await user.type(f.getByLabelText('Accent color'), '#1d4ed8');
    await user.click(f.getByRole('button', { name: 'Save changes' }));

    expect(await screen.findByText('The settings have been saved.')).toBeInTheDocument();
    expect(put).toHaveBeenCalledWith('/api/v1/admin/settings/branding', {
      body: { accentColor: '#1d4ed8' },
    });
  });

  it('marks the theme the server refuses', async () => {
    mockApiGet(fixtures());
    mockApiPut({
      '/api/v1/admin/settings/branding': problem(422, 'validation failed', [
        { location: 'body.theme', message: 'unknown theme' },
      ]),
    });
    const user = userEvent.setup();

    renderPage();
    const f = await form();
    await user.click(f.getByRole('combobox', { name: 'Theme' }));
    await user.click(await screen.findByRole('option', { name: 'Werkstatt' }));
    await user.click(f.getByRole('button', { name: 'Save changes' }));

    expect(await f.findByRole('alert')).toHaveTextContent('Please check your entries.');
    const theme = f.getByRole('combobox', { name: 'Theme' });
    expect(theme).toHaveAccessibleDescription(expect.stringContaining('Please check this entry.'));
    expect(theme).toHaveAttribute('aria-invalid', 'true');
  });

  it('shows the office the theme and the background without a way to change them', async () => {
    mockApiGet(fixtures(meOffice, { theme: 'werkstatt', hasBackground: true }));

    renderPage();

    const c = await card();
    expect(await c.findByRole('img', { name: "The school's background image" })).toHaveAttribute(
      'src',
      `data:image/jpeg;base64,${btoa('jpg')}`,
    );
    const terms = c.getAllByRole('term');
    const index = terms.findIndex((term) => term.textContent === 'Theme');
    expect(index).toBeGreaterThanOrEqual(0);
    expect(c.getAllByRole('definition')[index]).toHaveTextContent('Werkstatt');
    expect(c.getByText('Only administrators can change this.')).toBeInTheDocument();
    expect(c.queryByRole('form')).not.toBeInTheDocument();
    expect(c.queryByRole('combobox')).not.toBeInTheDocument();
    expect(c.queryByLabelText('Upload a background image')).not.toBeInTheDocument();
    expect(c.queryByLabelText('Replace the background image')).not.toBeInTheDocument();
    expect(
      c.queryByRole('button', { name: 'Remove the background image' }),
    ).not.toBeInTheDocument();
  });
});

describe('Background image', () => {
  it('shows the background image', async () => {
    mockApiGet(fixtures(meAdmin, { hasBackground: true }));

    renderPage();

    const c = await card();
    expect(await c.findByRole('heading', { name: 'Background image' })).toBeInTheDocument();
    expect(await c.findByRole('img', { name: "The school's background image" })).toHaveAttribute(
      'src',
      `data:image/jpeg;base64,${btoa('jpg')}`,
    );
    expect(c.getByLabelText('Replace the background image')).toHaveAccessibleDescription(hint);
  });

  it('says when there is none, without asking for it', async () => {
    const get = mockApiGet(fixtures());

    renderPage();

    const c = await card();
    expect(await c.findByText('No background image.')).toBeInTheDocument();
    expect(c.queryByRole('img', { name: "The school's background image" })).not.toBeInTheDocument();
    expect(c.getByLabelText('Upload a background image')).toHaveAccessibleDescription(hint);
    expect((get.mock.calls as unknown[][]).map(([path]) => path)).not.toContain(
      '/api/v1/settings/background',
    );
  });

  it('uploads a JPEG', async () => {
    mockApiGet(fixtures());
    const put = mockApiPut({ [backgroundPath]: { status: 204 } });
    const user = userEvent.setup();

    const { invalidate } = renderPage();
    const c = await card();
    await user.upload(await c.findByLabelText('Upload a background image'), jpeg());

    expect(await screen.findByText('The background image has been saved.')).toBeInTheDocument();
    expect(put).toHaveBeenCalledWith(backgroundPath, {
      body: { contentType: 'image/jpeg', data: btoa('jpg') },
    });
    await expectBackgroundRefreshed(invalidate);
  });

  it('accepts a WebP and an image of exactly 1 MB', async () => {
    mockApiGet(fixtures());
    const put = mockApiPut({ [backgroundPath]: { status: 204 } });
    const user = userEvent.setup();

    renderPage();
    const c = await card();
    await user.upload(
      await c.findByLabelText('Upload a background image'),
      new File(['webp'], 'bg.webp', { type: 'image/webp' }),
    );
    await waitFor(() =>
      expect(put).toHaveBeenCalledWith(backgroundPath, {
        body: { contentType: 'image/webp', data: btoa('webp') },
      }),
    );

    await user.upload(
      await c.findByLabelText('Upload a background image'),
      new File([new Uint8Array(1024 * 1024)], 'exact.jpg', { type: 'image/jpeg' }),
    );
    await waitFor(() => expect(put).toHaveBeenCalledTimes(2));
    expect(put).toHaveBeenLastCalledWith(backgroundPath, {
      body: { contentType: 'image/jpeg', data: expect.any(String) },
    });
    expect(c.queryByText(invalid)).not.toBeInTheDocument();
  });

  it('refuses a GIF before uploading it', async () => {
    mockApiGet(fixtures());
    const put = mockApiPut({});
    const user = userEvent.setup({ applyAccept: false });

    renderPage();
    const c = await card();
    const input = await c.findByLabelText('Upload a background image');
    await user.upload(input, new File(['gif'], 'bg.gif', { type: 'image/gif' }));

    expect(await c.findByRole('alert')).toHaveTextContent(invalid);
    expect(input).toHaveAccessibleDescription(expect.stringContaining(invalid));
    expect(input).toHaveAccessibleDescription(expect.stringContaining(hint));
    expect(put).not.toHaveBeenCalled();
  });

  it('refuses an image larger than 1 MB before uploading it', async () => {
    mockApiGet(fixtures());
    const put = mockApiPut({});
    const user = userEvent.setup();

    renderPage();
    const c = await card();
    const input = await c.findByLabelText('Upload a background image');
    await user.upload(
      input,
      new File([new Uint8Array(1024 * 1024 + 1)], 'big.jpg', { type: 'image/jpeg' }),
    );

    expect(await c.findByRole('alert')).toHaveTextContent(invalid);
    expect(input).toHaveAccessibleDescription(expect.stringContaining(invalid));
    expect(put).not.toHaveBeenCalled();
  });

  it('says when the chosen file cannot be read', async () => {
    mockApiGet(fixtures());
    const put = mockApiPut({});
    const user = userEvent.setup();

    renderPage();
    const c = await card();
    const input = await c.findByLabelText('Upload a background image');
    vi.spyOn(FileReader.prototype, 'readAsDataURL').mockImplementation(function (this: FileReader) {
      this.onerror?.(new ProgressEvent('error') as ProgressEvent<FileReader>);
    });
    await user.upload(input, jpeg());

    expect(await c.findByRole('alert')).toHaveTextContent(
      'The file could not be read; choose it again.',
    );
    expect(put).not.toHaveBeenCalled();
  });

  it('says so when the server refuses the image', async () => {
    mockApiGet(fixtures());
    const put = mockApiPut({
      [backgroundPath]: problem(422, 'validation failed', [
        { location: 'body.data', message: 'not a JPEG' },
      ]),
    });
    const user = userEvent.setup();

    renderPage();
    const c = await card();
    await user.upload(await c.findByLabelText('Upload a background image'), jpeg());

    expect(await c.findByText(invalid)).toBeInTheDocument();
    expect(put).toHaveBeenCalledTimes(1);
    expect(screen.queryByText('The background image has been saved.')).not.toBeInTheDocument();
  });

  it('removes the background image', async () => {
    mockApiGet(fixtures(meAdmin, { hasBackground: true }));
    const del = mockApiDelete({ [backgroundPath]: { status: 204 } });
    const user = userEvent.setup();

    const { invalidate } = renderPage();
    const c = await card();
    await c.findByRole('img', { name: "The school's background image" });
    await user.click(c.getByRole('button', { name: 'Remove the background image' }));

    expect(await screen.findByText('The background image has been removed.')).toBeInTheDocument();
    expect(del).toHaveBeenCalledWith(backgroundPath);
    await expectBackgroundRefreshed(invalidate);
  });

  it('offers no removal without a background image', async () => {
    mockApiGet(fixtures());

    renderPage();

    const c = await card();
    await c.findByText('No background image.');
    expect(
      c.queryByRole('button', { name: 'Remove the background image' }),
    ).not.toBeInTheDocument();
  });

  it('renders in German', async () => {
    mockApiGet(fixtures());
    await i18n.changeLanguage('de');

    renderPage();

    const f = await form('Erscheinungsbild');
    expect(f.getByRole('combobox', { name: 'Design' })).toHaveTextContent('Programm');
    const c = await card('Erscheinungsbild');
    expect(c.getByRole('heading', { name: 'Hintergrundbild' })).toBeInTheDocument();
  });
});
