import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Toaster } from '@/components/ui/sonner';
import i18n from '@/i18n';
import {
  conflict,
  courseTypes,
  ids,
  meOffice,
  mockApiDelete,
  mockApiGet,
  mockApiPost,
  mockApiPut,
  problem,
} from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { CourseTypesPage } from './CourseTypesPage';

afterEach(() => {
  vi.restoreAllMocks();
});

const asOffice = {
  '/api/v1/auth/me': { body: meOffice },
  '/api/v1/course-types': { body: { items: courseTypes } },
};

function renderPage() {
  render(<Toaster />);
  return renderInRouter(<CourseTypesPage />);
}

/** The body row of the course types table whose first cell is `name`. */
async function row(name: string) {
  const table = within(await screen.findByRole('table', { name: 'Course types' }));
  const cell = await table.findByRole('cell', { name: new RegExp(`^${name}`) });
  const tr = cell.closest('tr');
  if (!tr) throw new Error(`no row ${name}`);
  return within(tr);
}

describe('CourseTypesPage', () => {
  it('lists the course types with their descriptions', async () => {
    mockApiGet(asOffice);

    renderPage();

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Course types' }),
    ).toBeInTheDocument();
    expect((await row('Ballet')).getByText('Classical ballet technique.')).toBeInTheDocument();
    expect(await row('Jazz')).toBeTruthy();
  });

  it('adds a course type', async () => {
    let created = false;
    const added = { id: 'ct-new', name: 'Tap', description: 'Rhythm with metal plates.' };
    mockApiGet({
      ...asOffice,
      '/api/v1/course-types': () => ({
        body: { items: created ? [...courseTypes, added] : courseTypes },
      }),
    });
    const post = mockApiPost({
      '/api/v1/course-types': () => {
        created = true;
        return { status: 201, body: added };
      },
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(await screen.findByRole('button', { name: 'Add course type' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Add a course type' }));
    await user.type(sheet.getByLabelText('Name'), ' Tap ');
    await user.type(sheet.getByLabelText('Description (optional)'), 'Rhythm with metal plates.');
    await user.click(sheet.getByRole('button', { name: 'Add course type' }));

    expect(await screen.findByText('Tap has been added.')).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/course-types', {
      body: { name: 'Tap', description: 'Rhythm with metal plates.' },
    });
    expect(await row('Tap')).toBeTruthy();
    await waitFor(() =>
      expect(screen.queryByRole('dialog', { name: 'Add a course type' })).not.toBeInTheDocument(),
    );
  });

  it('checks the entries before sending them', async () => {
    mockApiGet(asOffice);
    const post = mockApiPost({});
    const user = userEvent.setup();

    renderPage();
    await user.click(await screen.findByRole('button', { name: 'Add course type' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Add a course type' }));
    await user.click(sheet.getByRole('button', { name: 'Add course type' }));

    expect(sheet.getByLabelText('Name')).toHaveAccessibleDescription('Please fill in this field.');
    expect(post).not.toHaveBeenCalled();
  });

  it('marks the fields a 422 answer names', async () => {
    mockApiGet(asOffice);
    mockApiPost({
      '/api/v1/course-types': problem(422, 'validation failed', [{ location: 'body.description' }]),
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(await screen.findByRole('button', { name: 'Add course type' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Add a course type' }));
    await user.type(sheet.getByLabelText('Name'), 'Tap');
    await user.type(sheet.getByLabelText('Description (optional)'), 'Rhythm.');
    await user.click(sheet.getByRole('button', { name: 'Add course type' }));

    expect(await sheet.findByRole('alert')).toHaveTextContent('Please check your entries.');
    expect(sheet.getByLabelText('Description (optional)')).toHaveAttribute('aria-invalid', 'true');
    expect(sheet.getByLabelText('Name')).not.toHaveAttribute('aria-invalid', 'true');
  });

  it('renames a course type and removes its description', async () => {
    mockApiGet(asOffice);
    const put = mockApiPut({
      '/api/v1/course-types/{courseTypeId}': ({ body }) => ({
        body: { id: ids.ballet, ...(body as object) },
      }),
    });
    const user = userEvent.setup();

    renderPage();
    await user.click((await row('Ballet')).getByRole('button', { name: 'Edit Ballet' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Edit Ballet' }));
    expect(sheet.getByLabelText('Description (optional)')).toHaveValue(
      'Classical ballet technique.',
    );
    await user.clear(sheet.getByLabelText('Name'));
    await user.type(sheet.getByLabelText('Name'), 'Classical Ballet');
    await user.clear(sheet.getByLabelText('Description (optional)'));
    await user.click(sheet.getByRole('button', { name: 'Save changes' }));

    expect(await screen.findByText('Classical Ballet has been saved.')).toBeInTheDocument();
    expect(put).toHaveBeenCalledWith('/api/v1/course-types/{courseTypeId}', {
      params: { path: { courseTypeId: ids.ballet } },
      body: { name: 'Classical Ballet' },
    });
  });

  it('says when the name is taken', async () => {
    mockApiGet(asOffice);
    mockApiPut({
      '/api/v1/course-types/{courseTypeId}': conflict('duplicate', 'course type name taken'),
    });
    const user = userEvent.setup();

    renderPage();
    await user.click((await row('Jazz')).getByRole('button', { name: 'Edit Jazz' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Edit Jazz' }));
    await user.clear(sheet.getByLabelText('Name'));
    await user.type(sheet.getByLabelText('Name'), 'BALLET');
    await user.click(sheet.getByRole('button', { name: 'Save changes' }));

    expect(await sheet.findByRole('alert')).toHaveTextContent(
      'Another course type is already called “BALLET”.',
    );
    expect(sheet.getByLabelText('Name')).toHaveAttribute('aria-invalid', 'true');
    expect(screen.queryByText('course type name taken')).not.toBeInTheDocument();
  });

  it('deletes a course type that nothing uses', async () => {
    let deleted = false;
    mockApiGet({
      ...asOffice,
      '/api/v1/course-types': () => ({
        body: { items: deleted ? courseTypes.slice(0, 2) : courseTypes },
      }),
    });
    const del = mockApiDelete({
      '/api/v1/course-types/{courseTypeId}': () => {
        deleted = true;
        return { status: 204 };
      },
    });
    const user = userEvent.setup();

    renderPage();
    await user.click((await row('Jazz')).getByRole('button', { name: 'Delete Jazz' }));
    const dialog = within(await screen.findByRole('alertdialog', { name: 'Delete Jazz?' }));
    await user.click(dialog.getByRole('button', { name: 'Delete' }));

    expect(await screen.findByText('Jazz has been deleted.')).toBeInTheDocument();
    expect(del).toHaveBeenCalledWith('/api/v1/course-types/{courseTypeId}', {
      params: { path: { courseTypeId: ids.jazz } },
    });
    await waitFor(() =>
      expect(
        within(screen.getByRole('table', { name: 'Course types' })).queryByText('Jazz'),
      ).not.toBeInTheDocument(),
    );
  });

  it('says why a course type that classes use cannot be deleted', async () => {
    mockApiGet(asOffice);
    mockApiDelete({ '/api/v1/course-types/{courseTypeId}': conflict('in-use') });
    const user = userEvent.setup();

    renderPage();
    await user.click((await row('Ballet')).getByRole('button', { name: 'Delete Ballet' }));
    const dialog = within(await screen.findByRole('alertdialog', { name: 'Delete Ballet?' }));
    await user.click(dialog.getByRole('button', { name: 'Delete' }));

    expect(await dialog.findByRole('alert')).toHaveTextContent(
      'Ballet is used by classes or lessons, so it cannot be deleted.',
    );
    expect(screen.getByRole('alertdialog')).toBeInTheDocument();
  });

  it('renders in German', async () => {
    mockApiGet(asOffice);
    await i18n.changeLanguage('de');

    renderPage();

    expect(await screen.findByRole('heading', { level: 1, name: 'Kursarten' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Kursart hinzufügen' })).toBeInTheDocument();
  });
});
