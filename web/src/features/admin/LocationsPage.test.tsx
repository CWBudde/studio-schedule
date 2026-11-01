import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { api } from '@/api/client';
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

/**
 * North Studio with three rooms, ordered by position and not by name, and
 * South Studio with one; in the API's order (by location, then position).
 */
const studio = '0190a000-0000-7000-8000-0000000000';
type RoomFixture = (typeof rooms)[number];
const [roomA, roomB] = rooms as [RoomFixture, RoomFixture];
const roomD = {
  id: `${studio}13`,
  locationId: ids.locationNorth,
  name: 'Room D',
  capacity: 8,
  position: 2,
};
const roomC = {
  id: `${studio}14`,
  locationId: ids.locationNorth,
  name: 'Room C',
  capacity: 10,
  position: 3,
};
const ordered = [roomA, roomD, roomC, roomB];

/** The names in the first column of the rooms table, top to bottom. */
function roomNames() {
  const table = screen.getByRole('table', { name: 'Rooms' });
  return within(table)
    .getAllByRole('row')
    .slice(1)
    .map((tr) => within(tr).getAllByRole('cell')[0]?.textContent);
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

  describe('room order', () => {
    it('lists the rooms of each location by position, each with its move buttons', async () => {
      mockApiGet({ ...asOffice, '/api/v1/rooms': { body: { items: ordered } } });

      renderPage();

      await row('Rooms', 'Room A');
      expect(roomNames()).toEqual(['Room A', 'Room D', 'Room C', 'Room B']);
      const first = await row('Rooms', 'Room A');
      expect(first.queryByRole('button', { name: /^Move Room A .* up$/ })).not.toBeInTheDocument();
      expect(
        first.getByRole('button', { name: 'Move Room A (North Studio) down' }),
      ).toBeInTheDocument();
      const middle = await row('Rooms', 'Room D');
      expect(middle.getByRole('button', { name: 'Move Room D (North Studio) up' })).toBeEnabled();
      expect(middle.getByRole('button', { name: 'Move Room D (North Studio) down' })).toBeEnabled();
      const last = await row('Rooms', 'Room C');
      expect(last.getByRole('button', { name: 'Move Room C (North Studio) up' })).toBeEnabled();
      expect(last.queryByRole('button', { name: /^Move Room C .* down$/ })).not.toBeInTheDocument();
      // The only room of a location has nowhere to go.
      const only = await row('Rooms', 'Room B');
      expect(only.queryByRole('button', { name: /^Move / })).not.toBeInTheDocument();
    });

    it("sends the location's whole new order and shows it", async () => {
      let saved = false;
      mockApiGet({
        ...asOffice,
        '/api/v1/rooms': () => ({
          body: {
            items: saved
              ? [{ ...roomD, position: 1 }, { ...roomA, position: 2 }, roomC, roomB]
              : ordered,
          },
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
      await user.click(
        (await row('Rooms', 'Room D')).getByRole('button', {
          name: 'Move Room D (North Studio) up',
        }),
      );

      expect(put).toHaveBeenCalledWith('/api/v1/locations/{locationId}/room-order', {
        params: { path: { locationId: ids.locationNorth } },
        body: { roomIds: [roomD.id, roomA.id, roomC.id] },
      });
      // While the order is saved, no other move can start.
      for (const button of within(screen.getByRole('table', { name: 'Rooms' })).getAllByRole(
        'button',
        { name: /^Move / },
      ))
        expect(button).toBeDisabled();

      saved = true;
      answer({ response: new Response(null, { status: 204 }) });

      await waitFor(() => expect(roomNames()).toEqual(['Room D', 'Room A', 'Room C', 'Room B']));
      expect(
        (await row('Rooms', 'Room A')).getByRole('button', {
          name: 'Move Room A (North Studio) up',
        }),
      ).toBeEnabled();
    });

    it('says when the rooms have changed meanwhile and shows them as they are', async () => {
      // Someone added Room E to North Studio after the page loaded.
      const roomE = {
        id: `${studio}15`,
        locationId: ids.locationNorth,
        name: 'Room E',
        capacity: 6,
        position: 4,
      };
      let loads = 0;
      mockApiGet({
        ...asOffice,
        '/api/v1/rooms': () => ({
          body: { items: loads++ === 0 ? ordered : [roomA, roomD, roomC, roomE, roomB] },
        }),
      });
      const put = mockApiPut({
        '/api/v1/locations/{locationId}/room-order': problem(422, 'validation failed', [
          { location: 'body.roomIds' },
        ]),
      });
      const user = userEvent.setup();

      renderPage();
      await user.click(
        (await row('Rooms', 'Room C')).getByRole('button', {
          name: 'Move Room C (North Studio) up',
        }),
      );

      expect(
        await screen.findByText(
          'The rooms of North Studio have changed meanwhile. The list now shows them as they are; please move the room again.',
        ),
      ).toBeInTheDocument();
      expect(put).toHaveBeenCalledWith('/api/v1/locations/{locationId}/room-order', {
        params: { path: { locationId: ids.locationNorth } },
        body: { roomIds: [roomA.id, roomC.id, roomD.id] },
      });
      await waitFor(() =>
        expect(roomNames()).toEqual(['Room A', 'Room D', 'Room C', 'Room E', 'Room B']),
      );
      expect(screen.queryByText('validation failed')).not.toBeInTheDocument();
    });

    it('says when the location is gone or the order could not be saved', async () => {
      mockApiGet({ ...asOffice, '/api/v1/rooms': { body: { items: ordered } } });
      let status = 404;
      mockApiPut({
        '/api/v1/locations/{locationId}/room-order': () => problem(status, 'nope'),
      });
      const user = userEvent.setup();

      renderPage();
      await user.click(
        (await row('Rooms', 'Room A')).getByRole('button', {
          name: 'Move Room A (North Studio) down',
        }),
      );
      expect(await screen.findByText('North Studio no longer exists.')).toBeInTheDocument();

      status = 500;
      await user.click(
        (await row('Rooms', 'Room A')).getByRole('button', {
          name: 'Move Room A (North Studio) down',
        }),
      );
      expect(
        await screen.findByText('The new order of the rooms of North Studio could not be saved.'),
      ).toBeInTheDocument();
    });

    it('offers the moves only while the table shows the rooms in their order', async () => {
      mockApiGet({ ...asOffice, '/api/v1/rooms': { body: { items: ordered } } });
      const user = userEvent.setup();
      const moves = () =>
        within(screen.getByRole('table', { name: 'Rooms' })).queryAllByRole('button', {
          name: /^Move /,
        });

      renderPage();
      await row('Rooms', 'Room A');
      expect(moves()).toHaveLength(4);

      // By name, a move would swap rooms that are not next to each other.
      const rooms = within(screen.getByRole('region', { name: 'Rooms' }));
      await user.click(rooms.getByRole('button', { name: 'Name' }));
      expect(roomNames()).toEqual(['Room A', 'Room B', 'Room C', 'Room D']);
      expect(moves()).toHaveLength(0);
      expect((await row('Rooms', 'Room C')).getByText('3')).toBeInTheDocument();

      await user.click(rooms.getByRole('button', { name: 'Order' }));
      expect(roomNames()).toEqual(['Room A', 'Room D', 'Room C', 'Room B']);
      expect(moves()).toHaveLength(4);

      // A search hides neighbours, so it hides the moves too.
      await user.type(rooms.getByRole('searchbox', { name: 'Search rooms' }), 'Room C');
      expect(roomNames()).toEqual(['Room C']);
      expect(moves()).toHaveLength(0);
    });

    it('names the moves in German', async () => {
      mockApiGet({ ...asOffice, '/api/v1/rooms': { body: { items: ordered } } });
      await i18n.changeLanguage('de');

      renderPage();

      const middle = await row('Räume', 'Room D');
      expect(
        middle.getByRole('button', { name: 'Room D (North Studio) nach oben verschieben' }),
      ).toBeInTheDocument();
      expect(
        middle.getByRole('button', { name: 'Room D (North Studio) nach unten verschieben' }),
      ).toBeInTheDocument();
      expect(screen.getByRole('button', { name: 'Reihenfolge' })).toBeInTheDocument();
    });
  });
});
