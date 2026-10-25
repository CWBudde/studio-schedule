import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Announcement } from '@/api/queries';
import { Toaster } from '@/components/ui/sonner';
import i18n from '@/i18n';
import {
  conflict,
  courseTypes,
  ids,
  meOffice,
  mockApiGet,
  mockApiPost,
  mockApiPut,
  publicSettings,
} from '@/test/mockApi';
import { renderWithQuery } from '@/test/utils';
import { AnnouncementsPage } from './AnnouncementsPage';

afterEach(() => {
  vi.restoreAllMocks();
});

const id = (n: number) => `0190a000-0000-7000-8000-${String(n).padStart(12, '0')}`;
const ballet = { id: ids.ballet, name: 'Ballet' };
const written = { createdAt: '2020-01-01T08:00:00Z', updatedAt: '2020-01-01T08:00:00Z' };

/** Fictional announcements as GET /admin/announcements lists them. */
const announcements: Announcement[] = [
  {
    id: id(911),
    audience: 'course-type',
    courseTypeId: ids.ballet,
    courseType: ballet,
    title: 'Ballet show',
    body: 'Tickets at the desk.',
    publishAt: '2099-01-01T08:00:00Z',
    email: true,
    emailRecipients: 0,
    author: 'Otto Office',
    ...written,
  },
  {
    id: id(912),
    audience: 'everyone',
    title: 'Welcome',
    body: 'Book here.',
    publishAt: '2020-01-01T08:00:00Z',
    emailRecipients: 0,
    ...written,
  },
  {
    id: id(913),
    audience: 'class',
    classId: ids.classBallet,
    class: {
      id: ids.classBallet,
      courseType: ballet,
      weekday: 1,
      startTime: '18:00',
      termId: ids.termAutumn,
      termName: 'Autumn 2026',
    },
    title: 'Class photo',
    body: 'Smile.',
    publishAt: '2019-12-01T08:00:00Z',
    emailRecipients: 0,
    ...written,
  },
  {
    id: id(914),
    audience: 'lesson',
    lessonId: ids.lessonBallet,
    lesson: {
      id: ids.lessonBallet,
      startsAt: '2026-10-26T17:00:00Z',
      endsAt: '2026-10-26T18:30:00Z',
      courseType: ballet,
    },
    title: 'Bring shoes',
    body: 'Pointe shoes, please.',
    publishAt: '2019-11-01T08:00:00Z',
    emailRecipients: 0,
    ...written,
  },
  {
    id: id(915),
    audience: 'role',
    role: 'office',
    title: 'Office meeting',
    body: 'Monday at nine.',
    publishAt: '2019-10-01T08:00:00Z',
    expiresAt: '2019-10-08T08:00:00Z',
    email: true,
    emailedAt: '2019-10-01T08:00:00Z',
    emailRecipients: 3,
    ...written,
  },
  {
    // Published with email; the announcements job has sent part of it.
    id: id(916),
    audience: 'everyone',
    title: 'Rehearsal',
    body: 'Saturday at ten.',
    publishAt: '2019-09-01T08:00:00Z',
    email: true,
    emailRecipients: 2,
    ...written,
  },
];

const asOffice = {
  '/api/v1/auth/me': { body: meOffice },
  '/api/v1/settings/public': { body: publicSettings },
  '/api/v1/course-types': { body: { items: courseTypes } },
  '/api/v1/admin/announcements': { body: { items: announcements } },
};

function renderPage() {
  render(<Toaster />);
  return renderWithQuery(<AnnouncementsPage />);
}

/** The body row of the table whose first cell starts with `title`. */
async function row(title: string, tableName = 'Announcements') {
  const table = within(await screen.findByRole('table', { name: tableName }));
  const cell = await table.findByText(title);
  const tr = cell.closest('tr');
  if (!tr) throw new Error(`no row ${title}`);
  return within(tr);
}

async function openAdd(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole('button', { name: 'New announcement' }));
  return within(await screen.findByRole('dialog', { name: 'New announcement' }));
}

describe('AnnouncementsPage', () => {
  it('lists the announcements with audience, window, status and email', async () => {
    mockApiGet(asOffice);

    renderPage();

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Announcements' }),
    ).toBeInTheDocument();

    const show = await row('Ballet show');
    expect(show.getByText('by Otto Office')).toBeInTheDocument();
    expect(show.getByText('Course type: Ballet')).toBeInTheDocument();
    expect(show.getByText('Scheduled')).toBeInTheDocument();
    expect(show.getByText('Emailed at publication')).toBeInTheDocument();

    const welcome = await row('Welcome');
    expect(welcome.getByText('Everyone')).toBeInTheDocument();
    expect(welcome.getByText('Never')).toBeInTheDocument();
    expect(welcome.getByText('Shown')).toBeInTheDocument();
    expect(welcome.getByText('In the app only')).toBeInTheDocument();

    expect(
      (await row('Class photo')).getByText('Class: Ballet, Monday 18:00 (Autumn 2026)'),
    ).toBeInTheDocument();
    // The lesson in Berlin time, though the tests run in New York.
    expect(
      (await row('Bring shoes')).getByText('Lesson: Ballet, Mon, Oct 26, 18:00'),
    ).toBeInTheDocument();

    const meeting = await row('Office meeting');
    expect(meeting.getByText('Role: Office')).toBeInTheDocument();
    expect(meeting.getByText('Expired')).toBeInTheDocument();
    expect(meeting.getByText('Emailed to 3 addresses')).toBeInTheDocument();
    // An expired one is no longer edited, but can be deleted.
    expect(meeting.getByRole('button', { name: 'Edit “Office meeting”' })).toBeDisabled();
    expect(meeting.getByRole('button', { name: 'Delete “Office meeting”' })).toBeEnabled();

    expect((await row('Rehearsal')).getByText('Being sent')).toBeInTheDocument();
  });

  it('says that an announcement published now with email is being sent', async () => {
    mockApiGet(asOffice);
    mockApiPost({
      '/api/v1/admin/announcements/recipients': { body: { recipients: 5 } },
      '/api/v1/admin/announcements': ({ body }) => ({
        status: 201,
        body: { id: id(922), emailRecipients: 0, ...written, ...(body as object) },
      }),
    });
    const user = userEvent.setup();

    renderPage();
    const sheet = await openAdd(user);
    await user.type(sheet.getByLabelText('Title'), 'Rain');
    await user.type(sheet.getByLabelText('Text'), 'No class outside.');
    await user.click(sheet.getByRole('switch', { name: 'Also send by email' }));
    await user.click(sheet.getByRole('button', { name: 'Publish' }));
    await user.click(
      await within(await screen.findByRole('alertdialog')).findByRole('button', {
        name: 'Publish and send',
      }),
    );

    expect(
      await screen.findByText('“Rain” is published and is being sent by email.'),
    ).toBeInTheDocument();
  });

  it('keeps the email of an announcement being sent', async () => {
    mockApiGet(asOffice);
    const user = userEvent.setup();

    renderPage();
    await user.click((await row('Rehearsal')).getByRole('button', { name: 'Edit “Rehearsal”' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Edit “Rehearsal”' }));
    const email = sheet.getByRole('switch', { name: 'Also send by email' });
    expect(email).toBeChecked();
    expect(email).toBeDisabled();
    expect(
      sheet.getByText(
        'Being sent by email: edits reach only the addresses that do not have it yet.',
      ),
    ).toBeInTheDocument();
  });

  it('publishes an announcement for everyone in the app', async () => {
    mockApiGet(asOffice);
    const post = mockApiPost({
      '/api/v1/admin/announcements': ({ body }) => ({
        status: 201,
        body: { id: id(920), emailRecipients: 0, ...written, ...(body as object) },
      }),
    });
    const user = userEvent.setup();

    renderPage();
    const sheet = await openAdd(user);
    await user.type(sheet.getByLabelText('Title'), '  Holiday  ');
    await user.type(sheet.getByLabelText('Text'), 'Closed on Monday.{Enter}Open on Tuesday.');
    await user.click(sheet.getByRole('button', { name: 'Publish' }));

    expect(await screen.findByText('“Holiday” is published.')).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/admin/announcements', {
      body: { audience: 'everyone', title: 'Holiday', body: 'Closed on Monday.\nOpen on Tuesday.' },
    });
  });

  it('asks before emailing, with the number of recipients, and schedules it in school time', async () => {
    mockApiGet(asOffice);
    const post = mockApiPost({
      '/api/v1/admin/announcements/recipients': { body: { recipients: 5 } },
      '/api/v1/admin/announcements': ({ body }) => ({
        status: 201,
        body: { id: id(921), emailRecipients: 0, ...written, ...(body as object) },
      }),
    });
    const user = userEvent.setup();

    renderPage();
    const sheet = await openAdd(user);
    await user.type(sheet.getByLabelText('Title'), 'Admins only');
    await user.type(sheet.getByLabelText('Text'), 'Hello.');
    await user.click(sheet.getByRole('combobox', { name: 'Audience' }));
    await user.click(await screen.findByRole('option', { name: 'Role' }));
    await user.click(sheet.getByRole('combobox', { name: 'Role' }));
    await user.click(await screen.findByRole('option', { name: 'Administrators' }));
    await user.type(sheet.getByLabelText('Publish on'), '2099-03-01');
    await user.type(sheet.getByLabelText('Publication time'), '09:00');
    await user.click(sheet.getByRole('switch', { name: 'Also send by email' }));
    await user.click(sheet.getByRole('button', { name: 'Publish' }));

    const confirm = within(await screen.findByRole('alertdialog'));
    expect(
      await confirm.findByText('It goes to 5 addresses now, or at its publication.'),
    ).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/admin/announcements/recipients', {
      body: { audience: 'role', role: 'admin' },
    });
    expect(post).not.toHaveBeenCalledWith('/api/v1/admin/announcements', expect.anything());
    await user.click(confirm.getByRole('button', { name: 'Publish and send' }));

    expect(await screen.findByText('“Admins only” is scheduled.')).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/admin/announcements', {
      body: {
        audience: 'role',
        role: 'admin',
        title: 'Admins only',
        body: 'Hello.',
        // 09:00 in Berlin.
        publishAt: '2099-03-01T08:00:00.000Z',
        email: true,
      },
    });
  });

  it('says when the day has as many emailed announcements as allowed', async () => {
    mockApiGet(asOffice);
    mockApiPost({
      '/api/v1/admin/announcements/recipients': { body: { recipients: 2 } },
      '/api/v1/admin/announcements': conflict('mail-limit'),
    });
    const user = userEvent.setup();

    renderPage();
    const sheet = await openAdd(user);
    await user.type(sheet.getByLabelText('Title'), 'Fourth');
    await user.type(sheet.getByLabelText('Text'), 'Hello.');
    await user.click(sheet.getByRole('switch', { name: 'Also send by email' }));
    await user.click(sheet.getByRole('button', { name: 'Publish' }));
    await user.click(
      await within(await screen.findByRole('alertdialog')).findByRole('button', {
        name: 'Publish and send',
      }),
    );

    expect(await sheet.findByText(/has 3 emailed announcements already/)).toBeInTheDocument();
  });

  it('checks the entries', async () => {
    mockApiGet(asOffice);
    const post = mockApiPost({});
    const user = userEvent.setup();

    renderPage();
    const sheet = await openAdd(user);
    await user.type(sheet.getByLabelText('Expires on'), '2099-03-01');
    await user.click(sheet.getByRole('button', { name: 'Publish' }));

    expect(await sheet.findAllByText('Please fill in this field.')).toHaveLength(2);
    expect(sheet.getByText('Enter a date and a time.')).toBeInTheDocument();
    expect(post).not.toHaveBeenCalled();
  });

  it('edits an announcement emailed already without emailing it again', async () => {
    mockApiGet({
      ...asOffice,
      '/api/v1/admin/announcements': {
        body: {
          items: [
            {
              ...announcements[4],
              expiresAt: undefined,
              publishAt: '2026-10-21T07:00:00Z',
            } as Announcement,
          ],
        },
      },
    });
    const put = mockApiPut({
      '/api/v1/admin/announcements/{announcementId}': ({ body }) => ({
        body: { ...announcements[4], ...(body as object) },
      }),
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(
      (await row('Office meeting')).getByRole('button', { name: 'Edit “Office meeting”' }),
    );
    const sheet = within(await screen.findByRole('dialog', { name: 'Edit “Office meeting”' }));
    const email = sheet.getByRole('switch', { name: 'Also send by email' });
    expect(email).toBeChecked();
    expect(email).toBeDisabled();
    expect(
      sheet.getByText('Emailed on Oct 1, 2019, 10:00: edits are not sent again.'),
    ).toBeInTheDocument();
    await user.clear(sheet.getByLabelText('Text'));
    await user.type(sheet.getByLabelText('Text'), 'Tuesday at nine.');
    await user.click(sheet.getByRole('button', { name: 'Save changes' }));

    expect(await screen.findByText('“Office meeting” is saved.')).toBeInTheDocument();
    expect(screen.queryByRole('alertdialog')).toBeNull();
    expect(put).toHaveBeenCalledWith('/api/v1/admin/announcements/{announcementId}', {
      params: { path: { announcementId: id(915) } },
      body: {
        audience: 'role',
        role: 'office',
        title: 'Office meeting',
        body: 'Tuesday at nine.',
        publishAt: '2026-10-21T07:00:00.000Z',
        email: true,
      },
    });
  });

  it('renders in German', async () => {
    mockApiGet(asOffice);
    await i18n.changeLanguage('de');

    renderPage();

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Mitteilungen' }),
    ).toBeInTheDocument();
    expect((await row('Welcome', 'Mitteilungen')).getByText('Alle')).toBeInTheDocument();
    expect(
      (await row('Office meeting', 'Mitteilungen')).getByText('An 3 Adressen verschickt'),
    ).toBeInTheDocument();
  });
});
