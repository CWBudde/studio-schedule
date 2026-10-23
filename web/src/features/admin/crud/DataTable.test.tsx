import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { Button } from '@/components/ui/button';
import { DataTable } from './DataTable';
import { columnHelper } from './table';

afterEach(() => {
  vi.restoreAllMocks();
});

type Studio = { id: string; name: string; capacity: number };

const studios: Studio[] = [
  { id: '1', name: 'Blue Room', capacity: 12 },
  { id: '2', name: 'Attic', capacity: 30 },
  { id: '3', name: 'Cellar', capacity: 8 },
];

const helper = columnHelper<Studio>();
const columns = helper.columns([
  helper.accessor('name', { header: 'Name' }),
  helper.accessor('capacity', { header: 'Capacity' }),
]);

function renderTable(props: Partial<Parameters<typeof DataTable<Studio>>[0]> = {}) {
  return render(
    <DataTable
      label="Studios"
      searchLabel="Search studios"
      empty="No studios yet."
      columns={columns}
      data={studios}
      getRowId={(s) => s.id}
      initialSorting={[{ id: 'name', desc: false }]}
      actions={(s) => <Button size="sm">Edit {s.name}</Button>}
      {...props}
    />,
  );
}

/** The first cell of every body row, in display order. */
const names = () =>
  within(screen.getByRole('table', { name: 'Studios' }))
    .getAllByRole('row')
    .slice(1)
    .map((row) => within(row).getAllByRole('cell')[0]?.textContent);

/** Makes the phone layout's media query match. */
function onPhone() {
  vi.spyOn(window, 'matchMedia').mockImplementation(
    (query: string) =>
      ({
        matches: true,
        media: query,
        onchange: null,
        addEventListener: () => undefined,
        removeEventListener: () => undefined,
        addListener: () => undefined,
        removeListener: () => undefined,
        dispatchEvent: () => false,
      }) as MediaQueryList,
  );
}

describe('DataTable', () => {
  it('shows the rows in the initial order with their actions', () => {
    renderTable();

    const table = within(screen.getByRole('table', { name: 'Studios' }));
    expect(table.getAllByRole('columnheader').map((th) => th.textContent)).toEqual([
      'Name',
      'Capacity',
      'Actions',
    ]);
    expect(names()).toEqual(['Attic', 'Blue Room', 'Cellar']);
    expect(table.getByRole('button', { name: 'Edit Cellar' })).toBeInTheDocument();
  });

  it('sorts by a column when its header is clicked', async () => {
    const user = userEvent.setup();
    renderTable();

    const nameHeader = screen.getByRole('columnheader', { name: /Name/ });
    expect(nameHeader).toHaveAttribute('aria-sort', 'ascending');
    await user.click(within(nameHeader).getByRole('button', { name: /Name/ }));
    expect(nameHeader).toHaveAttribute('aria-sort', 'descending');
    expect(names()).toEqual(['Cellar', 'Blue Room', 'Attic']);

    const capacityHeader = screen.getByRole('columnheader', { name: /Capacity/ });
    await user.click(within(capacityHeader).getByRole('button', { name: /Capacity/ }));
    expect(capacityHeader).toHaveAttribute('aria-sort', 'ascending');
    expect(nameHeader).toHaveAttribute('aria-sort', 'none');
    expect(names()).toEqual(['Cellar', 'Blue Room', 'Attic']);
    await user.click(within(capacityHeader).getByRole('button', { name: /Capacity/ }));
    expect(names()).toEqual(['Attic', 'Blue Room', 'Cellar']);
  });

  it('filters the rows by the search text', async () => {
    const user = userEvent.setup();
    renderTable();

    await user.type(screen.getByRole('searchbox', { name: 'Search studios' }), 'room');
    expect(names()).toEqual(['Blue Room']);

    await user.clear(screen.getByRole('searchbox', { name: 'Search studios' }));
    await user.type(screen.getByRole('searchbox', { name: 'Search studios' }), 'ballroom');
    expect(screen.getByText('Nothing matches “ballroom”.')).toBeInTheDocument();
    expect(screen.queryByRole('table')).not.toBeInTheDocument();
  });

  it('says when there is nothing yet', () => {
    renderTable({ data: [] });

    expect(screen.getByText('No studios yet.')).toBeInTheDocument();
    expect(screen.queryByRole('table')).not.toBeInTheDocument();
  });

  it('shows a loading state while the data is not there', () => {
    renderTable({ data: undefined });

    expect(screen.getByRole('status')).toHaveTextContent('Loading…');
  });

  it('shows an error with retry when loading failed', async () => {
    const user = userEvent.setup();
    const onRetry = vi.fn();
    renderTable({ data: undefined, isError: true, onRetry });

    expect(screen.getByText('Could not load the data.')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Retry' }));
    expect(onRetry).toHaveBeenCalledOnce();
  });

  it('shows the rows as cards on phones', async () => {
    onPhone();
    const user = userEvent.setup();
    renderTable();

    expect(screen.queryByRole('table')).not.toBeInTheDocument();
    const list = within(screen.getByRole('list', { name: 'Studios' }));
    const cards = list.getAllByRole('listitem');
    expect(cards.map((c) => within(c).getByRole('heading').textContent)).toEqual([
      'Attic',
      'Blue Room',
      'Cellar',
    ]);
    const attic = within(cards[0]!);
    expect(attic.getByText('Capacity')).toBeInTheDocument();
    expect(attic.getByText('30')).toBeInTheDocument();
    expect(attic.getByRole('button', { name: 'Edit Attic' })).toBeInTheDocument();

    await user.type(screen.getByRole('searchbox', { name: 'Search studios' }), 'cel');
    expect(list.getAllByRole('listitem')).toHaveLength(1);
  });
});
