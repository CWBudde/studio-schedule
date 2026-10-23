import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Toaster } from '@/components/ui/sonner';
import i18n from '@/i18n';
import {
  conflict,
  ids,
  locations,
  meOffice,
  mockApiDelete,
  mockApiGet,
  mockApiPost,
  mockApiPut,
  problem,
  rooms,
} from '@/test/mockApi';
import { renderInRouter } from '@/test/utils';
import { LocationsPage } from './LocationsPage';

afterEach(() => {
  vi.restoreAllMocks();
});

const asOffice = {
  '/api/v1/auth/me': { body: meOffice },
  '/api/v1/locations': { body: { items: locations } },
  '/api/v1/rooms': { body: { items: rooms } },
};

function renderPage() {
  // The toaster lives outside the page, as in the app's layout.
  render(<Toaster />);
  return renderInRouter(<LocationsPage />);
}

/** The body row of the table `table` whose first cell is `name`. */
async function row(table: string, name: string) {
  const t = within(await screen.findByRole('table', { name: table }));
  const cell = await t.findByRole('cell', { name: new RegExp(`^${name}`) });
  const tr = cell.closest('tr');
  if (!tr) throw new Error(`no row ${name}`);
  return within(tr);
}

describe('LocationsPage', () => {
  it('lists the locations and the rooms of each location with capacity', async () => {
    mockApiGet(asOffice);

    renderPage();

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Locations and rooms' }),
    ).toBeInTheDocument();
    const north = await row('Locations', 'North Studio');
    expect(north.getByText('1 Example Street, Sampletown')).toBeInTheDocument();
    expect(north.getByText('1 room')).toBeInTheDocument();
    const roomA = await row('Rooms', 'Room A');
    expect(roomA.getByText('North Studio')).toBeInTheDocument();
    expect(roomA.getByText('16')).toBeInTheDocument();
    const roomB = await row('Rooms', 'Room B');
    expect(roomB.getByText('South Studio')).toBeInTheDocument();
    expect(roomB.getByText('12')).toBeInTheDocument();
  });

  it('does not count the rooms of a location while the rooms cannot be loaded', async () => {
    let fail = true;
    mockApiGet({
      ...asOffice,
      '/api/v1/rooms': () => (fail ? problem(500, 'boom') : { body: { items: rooms } }),
    });
    const user = userEvent.setup();

    renderPage();

    const north = await row('Locations', 'North Studio');
    expect(await north.findByText('Unknown')).toBeInTheDocument();
    expect(north.queryByText('No rooms')).not.toBeInTheDocument();
    const roomsSection = within(screen.getByRole('region', { name: 'Rooms' }));
    expect(roomsSection.getByText('Could not load the data.')).toBeInTheDocument();

    fail = false;
    await user.click(roomsSection.getByRole('button', { name: 'Retry' }));

    expect(await row('Rooms', 'Room A')).toBeTruthy();
    expect((await row('Locations', 'North Studio')).getByText('1 room')).toBeInTheDocument();
  });

  it('adds a location', async () => {
    let created = false;
    const added = { id: 'loc-new', name: 'West Studio', address: '3 Sample Road' };
    mockApiGet({
      ...asOffice,
      '/api/v1/locations': () => ({
        body: { items: created ? [...locations, added] : locations },
      }),
    });
    const post = mockApiPost({
      '/api/v1/locations': () => {
        created = true;
        return { status: 201, body: added };
      },
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(await screen.findByRole('button', { name: 'Add location' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Add a location' }));
    await user.type(sheet.getByLabelText('Name'), '  West Studio ');
    await user.type(sheet.getByLabelText('Address (optional)'), '3 Sample Road');
    await user.click(sheet.getByRole('button', { name: 'Add location' }));

    expect(await screen.findByText('West Studio has been added.')).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/locations', {
      body: { name: 'West Studio', address: '3 Sample Road' },
    });
    expect(await row('Locations', 'West Studio')).toBeTruthy();
    await waitFor(() =>
      expect(screen.queryByRole('dialog', { name: 'Add a location' })).not.toBeInTheDocument(),
    );
  });

  it('checks the entries before sending them', async () => {
    mockApiGet(asOffice);
    const post = mockApiPost({});
    const user = userEvent.setup();

    renderPage();
    await user.click(await screen.findByRole('button', { name: 'Add location' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Add a location' }));
    await user.click(sheet.getByRole('button', { name: 'Add location' }));

    expect(sheet.getByLabelText('Name')).toHaveAccessibleDescription('Please fill in this field.');
    expect(sheet.getByLabelText('Name')).toHaveAttribute('aria-invalid', 'true');
    expect(post).not.toHaveBeenCalled();
  });

  it('marks the fields a 422 answer names', async () => {
    mockApiGet(asOffice);
    mockApiPost({
      '/api/v1/locations': problem(422, 'validation failed', [{ location: 'body.name' }]),
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(await screen.findByRole('button', { name: 'Add location' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Add a location' }));
    await user.type(sheet.getByLabelText('Name'), 'West Studio');
    await user.click(sheet.getByRole('button', { name: 'Add location' }));

    expect(await sheet.findByRole('alert')).toHaveTextContent('Please check your entries.');
    expect(sheet.getByLabelText('Name')).toHaveAttribute('aria-invalid', 'true');
    expect(sheet.getByLabelText('Name')).toHaveAccessibleDescription('Please check this entry.');
    expect(sheet.getByLabelText('Address (optional)')).not.toHaveAttribute('aria-invalid', 'true');
  });

  it('says when the name of a location is taken', async () => {
    mockApiGet(asOffice);
    mockApiPut({
      '/api/v1/locations/{locationId}': conflict('duplicate', 'location name taken'),
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(
      (await row('Locations', 'South Studio')).getByRole('button', { name: 'Edit South Studio' }),
    );
    const sheet = within(await screen.findByRole('dialog', { name: 'Edit South Studio' }));
    expect(sheet.getByLabelText('Name')).toHaveValue('South Studio');
    await user.clear(sheet.getByLabelText('Name'));
    await user.type(sheet.getByLabelText('Name'), 'north studio');
    await user.click(sheet.getByRole('button', { name: 'Save changes' }));

    expect(await sheet.findByRole('alert')).toHaveTextContent(
      'Another location is already called “north studio”.',
    );
    expect(sheet.getByLabelText('Name')).toHaveAttribute('aria-invalid', 'true');
    expect(screen.queryByText('location name taken')).not.toBeInTheDocument();
  });

  it('renames a location', async () => {
    mockApiGet(asOffice);
    const put = mockApiPut({
      '/api/v1/locations/{locationId}': ({ body }) => ({
        body: { id: ids.locationSouth, ...(body as object) },
      }),
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(
      (await row('Locations', 'South Studio')).getByRole('button', { name: 'Edit South Studio' }),
    );
    const sheet = within(await screen.findByRole('dialog', { name: 'Edit South Studio' }));
    await user.clear(sheet.getByLabelText('Name'));
    await user.type(sheet.getByLabelText('Name'), 'Garden Studio');
    await user.click(sheet.getByRole('button', { name: 'Save changes' }));

    expect(await screen.findByText('Garden Studio has been saved.')).toBeInTheDocument();
    expect(put).toHaveBeenCalledWith('/api/v1/locations/{locationId}', {
      params: { path: { locationId: ids.locationSouth } },
      body: { name: 'Garden Studio' },
    });
  });

  it('says why a location with rooms cannot be deleted', async () => {
    mockApiGet(asOffice);
    const del = mockApiDelete({
      '/api/v1/locations/{locationId}': conflict('in-use', 'location has rooms'),
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(
      (await row('Locations', 'North Studio')).getByRole('button', {
        name: 'Delete North Studio',
      }),
    );
    const dialog = within(await screen.findByRole('alertdialog', { name: 'Delete North Studio?' }));
    await user.click(dialog.getByRole('button', { name: 'Delete' }));

    expect(await dialog.findByRole('alert')).toHaveTextContent(
      'North Studio still has rooms. Delete its rooms first.',
    );
    expect(del).toHaveBeenCalledWith('/api/v1/locations/{locationId}', {
      params: { path: { locationId: ids.locationNorth } },
    });
    expect(screen.getByRole('alertdialog')).toBeInTheDocument();
  });

  it('adds a room to a location', async () => {
    mockApiGet(asOffice);
    const post = mockApiPost({
      '/api/v1/rooms': ({ body }) => ({
        status: 201,
        body: { id: 'room-new', ...(body as object) },
      }),
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(await screen.findByRole('button', { name: 'Add room' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Add a room' }));
    await user.type(sheet.getByLabelText('Name'), 'Room C');
    await user.click(sheet.getByRole('combobox', { name: 'Location' }));
    await user.click(await screen.findByRole('option', { name: 'South Studio' }));
    await user.type(sheet.getByLabelText('Capacity'), '20');
    await user.click(sheet.getByRole('button', { name: 'Add room' }));

    expect(await screen.findByText('Room C has been added.')).toBeInTheDocument();
    expect(post).toHaveBeenCalledWith('/api/v1/rooms', {
      body: { name: 'Room C', locationId: ids.locationSouth, capacity: 20 },
    });
  });

  it('refuses a capacity that is no whole number from 1 to 1000', async () => {
    mockApiGet(asOffice);
    const post = mockApiPost({});
    const user = userEvent.setup();

    renderPage();
    await user.click(await screen.findByRole('button', { name: 'Add room' }));
    const sheet = within(await screen.findByRole('dialog', { name: 'Add a room' }));
    await user.type(sheet.getByLabelText('Capacity'), '0');
    await user.click(sheet.getByRole('button', { name: 'Add room' }));

    expect(sheet.getByLabelText('Capacity')).toHaveAccessibleDescription(
      /Enter a whole number from 1 to 1000\.$/,
    );
    expect(sheet.getByRole('combobox', { name: 'Location' })).toHaveAccessibleDescription(
      'Please choose a location.',
    );
    expect(post).not.toHaveBeenCalled();
  });

  it('says when a location has a room of that name already', async () => {
    mockApiGet(asOffice);
    mockApiPut({ '/api/v1/rooms/{roomId}': conflict('duplicate') });
    const user = userEvent.setup();

    renderPage();
    await user.click(
      (await row('Rooms', 'Room A')).getByRole('button', { name: 'Edit Room A (North Studio)' }),
    );
    const sheet = within(await screen.findByRole('dialog', { name: 'Edit Room A' }));
    expect(sheet.getByLabelText('Capacity')).toHaveValue(16);
    await user.clear(sheet.getByLabelText('Name'));
    await user.type(sheet.getByLabelText('Name'), 'Room B');
    await user.click(sheet.getByRole('button', { name: 'Save changes' }));

    expect(await sheet.findByRole('alert')).toHaveTextContent(
      'North Studio already has a room called “Room B”.',
    );
  });

  it('deletes a room that nothing uses', async () => {
    let deleted = false;
    mockApiGet({
      ...asOffice,
      '/api/v1/rooms': () => ({ body: { items: deleted ? [rooms[0]] : rooms } }),
    });
    mockApiDelete({
      '/api/v1/rooms/{roomId}': () => {
        deleted = true;
        return { status: 204 };
      },
    });
    const user = userEvent.setup();

    renderPage();
    await user.click(
      (await row('Rooms', 'Room B')).getByRole('button', { name: 'Delete Room B (South Studio)' }),
    );
    const dialog = within(await screen.findByRole('alertdialog', { name: 'Delete Room B?' }));
    await user.click(dialog.getByRole('button', { name: 'Delete' }));

    expect(await screen.findByText('Room B has been deleted.')).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
    await waitFor(() =>
      expect(
        within(screen.getByRole('table', { name: 'Rooms' })).queryByText('Room B'),
      ).not.toBeInTheDocument(),
    );
  });

  it('says why a room used by classes cannot be deleted', async () => {
    mockApiGet(asOffice);
    mockApiDelete({ '/api/v1/rooms/{roomId}': conflict('in-use') });
    const user = userEvent.setup();

    renderPage();
    await user.click(
      (await row('Rooms', 'Room A')).getByRole('button', { name: 'Delete Room A (North Studio)' }),
    );
    const dialog = within(await screen.findByRole('alertdialog', { name: 'Delete Room A?' }));
    await user.click(dialog.getByRole('button', { name: 'Delete' }));

    expect(await dialog.findByRole('alert')).toHaveTextContent(
      'Room A is used by classes or lessons, so it cannot be deleted.',
    );
  });

  it('renders in German', async () => {
    mockApiGet(asOffice);
    await i18n.changeLanguage('de');

    renderPage();

    expect(
      await screen.findByRole('heading', { level: 1, name: 'Standorte und Räume' }),
    ).toBeInTheDocument();
    const north = await row('Standorte', 'North Studio');
    expect(north.getByText('1 Raum')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Raum hinzufügen' })).toBeInTheDocument();
  });
});
