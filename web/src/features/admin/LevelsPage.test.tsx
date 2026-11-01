import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '@/api/client';
import { Toaster } from '@/components/ui/sonner';
import i18n from '@/i18n';
import {
  conflict,
  ids,
  levels,
  meOffice,
  mockApiDelete,
  mockApiGet,
  mockApiPost,
  mockApiPut,
  problem,
} from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { LevelsPage } from './LevelsPage';

afterEach(() => {
  vi.restoreAllMocks();
});

const asOffice = {
  '/api/v1/auth/me': { body: meOffice },
  '/api/v1/levels': { body: { items: levels } },
};

function renderPage() {
  // The toaster lives outside the page, as in the app's layout.
  render(<Toaster />);
  return renderInRouter(<LevelsPage />);
}

type LevelFixture = (typeof levels)[number];
const [beginner, intermediate, advanced, professional] = levels as [
  LevelFixture,
  LevelFixture,
  LevelFixture,
  LevelFixture,
];

/** The names in the first column of the levels table, top to bottom. */
function levelNames() {
  const table = screen.getByRole('table', { name: 'Levels' });
  return within(table)
    .getAllByRole('row')
    .slice(1)
    .map((tr) => within(tr).getAllByRole('cell')[0]?.textContent);
}

/** The body row of the levels table whose first cell is `name`. */
async function row(name: string) {
  const table = within(await screen.findByRole('table', { name: 'Levels' }));
  const cell = await table.findByRole('cell', { name: new RegExp(`^${name}$`) });
  const tr = cell.closest('tr');
  if (!tr) throw new Error(`no row ${name}`);
  return within(tr);
}

async function openAddSheet(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole('button', { name: 'Add level' }));
  return within(await screen.findByRole('dialog', { name: 'Add a level' }));
}

describe('LevelsPage', () => {
  it('lists the levels in the API order with their codes and move buttons', async () => {
    mockApiGet(asOffice);

    renderPage();

    expect(await screen.findByRole('heading', { level: 1, name: 'Levels' })).toBeInTheDocument();
    await row('Beginner');
    expect(levelNames()).toEqual(['Beginner', 'Intermediate', 'Advanced', 'Professional']);
    const first = await row('Beginner');
    expect(first.getByText('B')).toBeInTheDocument();
    expect(first.queryByRole('button', { name: 'Move Beginner up' })).not.toBeInTheDocument();
    expect(first.getByRole('button', { name: 'Move Beginner down' })).toBeEnabled();
    const last = await row('Professional');
    expect(last.getByRole('button', { name: 'Move Professional up' })).toBeEnabled();
    expect(last.queryByRole('button', { name: 'Move Professional down' })).not.toBeInTheDocument();
    // The order is the point of the list: the columns do not sort it otherwise.
    expect(
      within(screen.getByRole('table', { name: 'Levels' })).queryByRole('button', {
        name: 'Name',
      }),
    ).not.toBeInTheDocument();
  });

  it('invites to add levels while there are none', async () => {
    mockApiGet({ ...asOffice, '/api/v1/levels': { body: { items: [] } } });

    renderPage();

    expect(
      await screen.findByText('No levels yet. Add the levels your classes are for.'),
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Add level' })).toBeEnabled();
  });

  it('adds a level, which comes last', async () => {
    let created = false;
    const added = { id: 'level-new', name: 'Masterclass', code: 'MC', position: 5 };
    mockApiGet({
      ...asOffice,
      '/api/v1/levels': () => ({ body: { items: created ? [...levels, added] : levels } }),
    });
    const post = mockApiPost({
      '/api/v1/levels': () => {
        created = true;
        return { status: 201, body: added };
      },
    });
    const user = userEvent.setup();

    renderPage();
    const sheet = await openAddSheet(user);
    await user.type(sheet.getByLabelText('Name'), ' Masterclass ');
    await user.type(sheet.getByLabelText('Code (optional)'), ' MC ');
    await user.click(sheet.getByRole('button', { name: 'Add level' }));

    expect(await screen.findByText('Masterclass has been added.')).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/levels', {
      body: { name: 'Masterclass', code: 'MC' },
    });
    await waitFor(() =>
      expect(levelNames()).toEqual([
        'Beginner',
        'Intermediate',
        'Advanced',
        'Professional',
        'Masterclass',
      ]),
    );
    await waitFor(() =>
      expect(screen.queryByRole('dialog', { name: 'Add a level' })).not.toBeInTheDocument(),
    );
  });

  it('leaves out an empty code and checks the entries before sending them', async () => {
    mockApiGet(asOffice);
    const post = mockApiPost({
      '/api/v1/levels': ({ body }) => ({
        status: 201,
        body: { id: 'level-new', position: 5, ...(body as object) },
      }),
    });
    const user = userEvent.setup();

    renderPage();
    const sheet = await openAddSheet(user);
    await user.type(sheet.getByLabelText('Code (optional)'), 'TOOLONGCODE');
    await user.click(sheet.getByRole('button', { name: 'Add level' }));

    expect(sheet.getByLabelText('Name')).toHaveAccessibleDescription('Please fill in this field.');
    expect(sheet.getByLabelText('Code (optional)')).toHaveAttribute('aria-invalid', 'true');
    expect(post).not.toHaveBeenCalled();

    await user.type(sheet.getByLabelText('Name'), 'Open');
    await user.clear(sheet.getByLabelText('Code (optional)'));
    await user.type(sheet.getByLabelText('Code (optional)'), '   ');
    await user.click(sheet.getByRole('button', { name: 'Add level' }));

    expect(await screen.findByText('Open has been added.')).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/levels', { body: { name: 'Open' } });
  });

  it('says on the name field when the name or code is taken', async () => {
    mockApiGet(asOffice);
    mockApiPost({ '/api/v1/levels': conflict('duplicate', 'level name taken') });
    const user = userEvent.setup();

    renderPage();
    const sheet = await openAddSheet(user);
    await user.type(sheet.getByLabelText('Name'), 'Masterclass');
    await user.type(sheet.getByLabelText('Code (optional)'), 'b');
    await user.click(sheet.getByRole('button', { name: 'Add level' }));

    expect(await sheet.findByRole('alert')).toHaveTextContent(
      'Another level already has the name “Masterclass” or the code “b”.',
    );
    expect(sheet.getByLabelText('Name')).toHaveAttribute('aria-invalid', 'true');
    expect(sheet.getByLabelText('Name')).toHaveAccessibleDescription(
      'This name or code is already taken.',
    );
    expect(screen.queryByText('level name taken')).not.toBeInTheDocument();
  });

  it('marks the fields a 422 answer names', async () => {
    mockApiGet(asOffice);
    mockApiPut({
      '/api/v1/levels/{levelId}': problem(422, 'validation failed', [{ location: 'body.code' }]),
    });
    const user = userEvent.setup();

    renderPage();
    await user.click((await row('Advanced')).getByRole('button', { name: 'Edit Advanced' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Edit Advanced' }));
    await user.click(sheet.getByRole('button', { name: 'Save changes' }));

    expect(await sheet.findByRole('alert')).toHaveTextContent('Please check your entries.');
    expect(sheet.getByLabelText('Code (optional)')).toHaveAttribute('aria-invalid', 'true');
    expect(sheet.getByLabelText('Name')).not.toHaveAttribute('aria-invalid', 'true');
  });

  it('renames a level and removes its code', async () => {
    mockApiGet(asOffice);
    const put = mockApiPut({
      '/api/v1/levels/{levelId}': ({ body }) => ({
        body: { id: ids.levelAdvanced, position: 3, ...(body as object) },
      }),
    });
    const user = userEvent.setup();

    renderPage();
    await user.click((await row('Advanced')).getByRole('button', { name: 'Edit Advanced' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Edit Advanced' }));
    expect(sheet.getByLabelText('Code (optional)')).toHaveValue('A');
    await user.clear(sheet.getByLabelText('Name'));
    await user.type(sheet.getByLabelText('Name'), 'Experienced');
    await user.clear(sheet.getByLabelText('Code (optional)'));
    await user.click(sheet.getByRole('button', { name: 'Save changes' }));

    expect(await screen.findByText('Experienced has been saved.')).toBeInTheDocument();
    expect(put).toHaveBeenCalledWith('/api/v1/levels/{levelId}', {
      params: { path: { levelId: ids.levelAdvanced } },
      body: { name: 'Experienced' },
    });
  });

  it('moves a level down, sending the whole new order', async () => {
    let saved = false;
    mockApiGet({
      ...asOffice,
      '/api/v1/levels': () => ({
        body: { items: saved ? [intermediate, beginner, advanced, professional] : levels },
      }),
    });
    let answer: (value: { error?: unknown; response: Response }) => void = () => undefined;
    const put = vi.spyOn(api, 'PUT').mockImplementation(
      (() =>
        new Promise((resolve) => {
          answer = resolve;
        })) as unknown as typeof api.PUT,
    );
    const user = userEvent.setup();

    renderPage();
    await user.click((await row('Beginner')).getByRole('button', { name: 'Move Beginner down' }));

    expect(put).toHaveBeenCalledWith('/api/v1/level-order', {
      body: {
        levelIds: [intermediate.id, beginner.id, advanced.id, professional.id],
      },
    });
    // While the order is saved, no other move can start.
    for (const button of within(screen.getByRole('table', { name: 'Levels' })).getAllByRole(
      'button',
      { name: /^Move / },
    ))
      expect(button).toBeDisabled();

    saved = true;
    answer({ response: new Response(null, { status: 204 }) });

    await waitFor(() =>
      expect(levelNames()).toEqual(['Intermediate', 'Beginner', 'Advanced', 'Professional']),
    );
    expect((await row('Beginner')).getByRole('button', { name: 'Move Beginner up' })).toBeEnabled();
  });

  it('says when the levels have changed meanwhile and shows them as they are', async () => {
    // Someone added a level after the page loaded.
    const open = { id: 'level-open', name: 'Open', position: 5 };
    let loads = 0;
    mockApiGet({
      ...asOffice,
      '/api/v1/levels': () => ({ body: { items: loads++ === 0 ? levels : [...levels, open] } }),
    });
    const put = mockApiPut({
      '/api/v1/level-order': problem(422, 'validation failed', [{ location: 'body.levelIds' }]),
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(
      (await row('Professional')).getByRole('button', { name: 'Move Professional up' }),
    );

    expect(
      await screen.findByText(
        'The levels have changed meanwhile. The list now shows them as they are; please move the level again.',
      ),
    ).toBeInTheDocument();
    expect(put).toHaveBeenCalledWith('/api/v1/level-order', {
      body: { levelIds: [beginner.id, intermediate.id, professional.id, advanced.id] },
    });
    await waitFor(() =>
      expect(levelNames()).toEqual([
        'Beginner',
        'Intermediate',
        'Advanced',
        'Professional',
        'Open',
      ]),
    );
    expect(screen.queryByText('validation failed')).not.toBeInTheDocument();
  });

  it('says when the order could not be saved', async () => {
    mockApiGet(asOffice);
    mockApiPut({ '/api/v1/level-order': problem(500, 'boom') });
    const user = userEvent.setup();

    renderPage();
    await user.click((await row('Advanced')).getByRole('button', { name: 'Move Advanced down' }));

    expect(
      await screen.findByText('The new order of the levels could not be saved.'),
    ).toBeInTheDocument();
  });

  it('says why a level that classes use cannot be deleted', async () => {
    mockApiGet(asOffice);
    const del = mockApiDelete({ '/api/v1/levels/{levelId}': conflict('in-use') });
    const user = userEvent.setup();

    renderPage();
    await user.click((await row('Beginner')).getByRole('button', { name: 'Delete Beginner' }));
    const dialog = within(await screen.findByRole('alertdialog', { name: 'Delete Beginner?' }));
    await user.click(dialog.getByRole('button', { name: 'Delete' }));

    expect(await dialog.findByRole('alert')).toHaveTextContent(
      'Beginner is used by classes, so it cannot be deleted.',
    );
    expect(del).toHaveBeenCalledWith('/api/v1/levels/{levelId}', {
      params: { path: { levelId: ids.levelBeginner } },
    });
  });

  it('deletes a level that nothing uses', async () => {
    let deleted = false;
    mockApiGet({
      ...asOffice,
      '/api/v1/levels': () => ({
        body: { items: deleted ? levels.filter((l) => l !== professional) : levels },
      }),
    });
    mockApiDelete({
      '/api/v1/levels/{levelId}': () => {
        deleted = true;
        return { status: 204 };
      },
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(
      (await row('Professional')).getByRole('button', { name: 'Delete Professional' }),
    );
    const dialog = within(await screen.findByRole('alertdialog', { name: 'Delete Professional?' }));
    await user.click(dialog.getByRole('button', { name: 'Delete' }));

    expect(await screen.findByText('Professional has been deleted.')).toBeInTheDocument();
    await waitFor(() => expect(levelNames()).toEqual(['Beginner', 'Intermediate', 'Advanced']));
  });

  it('renders in German', async () => {
    mockApiGet(asOffice);
    await i18n.changeLanguage('de');

    renderPage();

    expect(await screen.findByRole('heading', { level: 1, name: 'Level' })).toBeInTheDocument();
    const middle = within(
      (await screen.findByRole('cell', { name: 'Intermediate' })).closest('tr')!,
    );
    expect(
      middle.getByRole('button', { name: 'Intermediate nach oben verschieben' }),
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Level hinzufügen' })).toBeInTheDocument();
  });
});
