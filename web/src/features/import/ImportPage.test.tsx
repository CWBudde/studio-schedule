import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ImportInspection, ImportResult, ImportRunEntry } from '@/api/queries';
import { type MockRequest, mockApiGet, mockApiPost, problem, publicSettings } from '@/test/mockApi';
import { renderWithQuery } from '@/test/utils';
import { mappingStorageKey } from './importMapping';
import { ImportPage } from './ImportPage';

const inspection: ImportInspection = {
  encoding: 'windows-1252',
  delimiter: 'semicolon',
  headers: [
    'Mitgliedsnummer',
    'Vorname',
    'Nachname',
    'E-Mail Mutter',
    'Vorname Mutter',
    'Nachname Mutter',
    'Kurs',
  ],
  rows: 2,
  sample: [
    ['M-1', 'Jürgen', 'Müßig', 'kaethe@example.org', 'Käthe', 'Müßig', 'Ballett'],
    ['M-2', 'Zoë', 'Müßig', 'kaethe@example.org', 'Käthe', 'Müßig', 'Jazz'],
  ],
  suggested: {
    columns: {
      memberNo: 'Mitgliedsnummer',
      givenName: 'Vorname',
      familyName: 'Nachname',
      guardian1Email: 'E-Mail Mutter',
      guardian1GivenName: 'Vorname Mutter',
      guardian1FamilyName: 'Nachname Mutter',
    },
  },
};

const counts = {
  rows: 2,
  created: 1,
  updated: 0,
  unchanged: 0,
  failed: 1,
  guardiansCreated: 1,
  guardiansUpdated: 0,
  linksAdded: 1,
};

const result = (applied: boolean): ImportResult => ({
  applied,
  ...(applied ? { runId: '0190a000-0000-7000-8000-000000000501' } : {}),
  counts,
  rows: [
    {
      line: 2,
      memberNo: 'M-1',
      givenName: 'Jürgen',
      familyName: 'Müßig',
      action: 'create',
      changes: ['memberNo', 'givenName', 'familyName'],
      issues: [],
      duplicates: [
        { id: '0190a000-0000-7000-8000-000000000502', givenName: 'Jurgen', familyName: 'Mussig' },
      ],
      guardians: [
        { slot: 1, givenName: 'Käthe', familyName: 'Müßig', action: 'create', linked: true },
      ],
    },
    {
      line: 3,
      memberNo: 'M-2',
      givenName: 'Zoë',
      familyName: 'Müßig',
      action: 'error',
      changes: [],
      issues: [{ code: 'invalid', field: 'guardian1Email' }],
      duplicates: [],
      guardians: [],
    },
  ],
});

const run: ImportRunEntry = {
  id: '0190a000-0000-7000-8000-000000000501',
  fileName: 'Mitglieder.csv',
  by: {
    id: '0190a000-0000-7000-8000-000000000503',
    email: 'olga@example.net',
    givenName: 'Olga',
    familyName: 'Office',
  },
  startedAt: '2026-10-21T10:00:00Z',
  finishedAt: '2026-10-21T10:00:01Z',
  counts,
  errors: [{ line: 3, code: 'invalid', field: 'guardian1Email' }],
};

beforeEach(() => {
  localStorage.clear();
});

afterEach(() => {
  vi.restoreAllMocks();
});

function file(name = 'Mitglieder.csv') {
  return new File([new Uint8Array([0x4d, 0xfc, 0x3b])], name, { type: 'text/csv' });
}

describe('ImportPage', () => {
  it('reads a file, maps its columns, tries it and imports it', async () => {
    let runs: ImportRunEntry[] = [];
    mockApiGet({
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/admin/imports': () => ({ body: { items: runs } }),
    });
    const posts: MockRequest[] = [];
    mockApiPost({
      '/api/v1/admin/imports/inspect': (init) => {
        posts.push(init);
        return { body: inspection };
      },
      '/api/v1/admin/imports': (init) => {
        posts.push(init);
        const body = init.body as { apply?: boolean };
        if (body.apply) runs = [run];
        return { body: result(body.apply === true) };
      },
    });
    const user = userEvent.setup();

    renderWithQuery(<ImportPage />);

    expect(await screen.findByText('Nothing has been imported yet.')).toBeInTheDocument();
    await user.upload(screen.getByLabelText('CSV file'), file());

    // The bytes go to the server base64 encoded.
    expect(posts[0]?.body).toEqual({ fileName: 'Mitglieder.csv', content: 'Tfw7' });
    expect(
      await screen.findByText('Mitglieder.csv: 2 rows, Windows-1252, separated by semicolons.'),
    ).toBeInTheDocument();
    expect(screen.getByRole('combobox', { name: 'Member number' })).toHaveTextContent(
      'Mitgliedsnummer',
    );
    expect(screen.getByRole('combobox', { name: 'Parent 1: Email' })).toHaveTextContent(
      'E-Mail Mutter',
    );
    expect(screen.getByRole('combobox', { name: 'Phone' })).toHaveTextContent('Not imported');
    expect(screen.getByRole('table', { name: 'First rows of the file' })).toHaveTextContent(
      'Jürgen',
    );

    // "Kurs" goes into the notes, which this browser remembers.
    await user.click(screen.getByRole('checkbox', { name: 'Kurs' }));
    expect(JSON.parse(localStorage.getItem(mappingStorageKey) ?? '{}')).toMatchObject({
      notes: ['Kurs'],
    });

    await user.click(screen.getByRole('button', { name: 'Try it' }));

    expect(posts[1]?.body).toEqual({
      fileName: 'Mitglieder.csv',
      content: 'Tfw7',
      mapping: { columns: inspection.suggested.columns, notes: ['Kurs'] },
      apply: false,
    });
    const trial = within(await screen.findByRole('table', { name: 'Trial' }));
    const rows = trial.getAllByRole('row').slice(1);
    expect(rows[0]).toHaveTextContent('2M-1Jürgen MüßigNew');
    expect(rows[0]).toHaveTextContent('Sets Member number, First name, Last name');
    expect(rows[0]).toHaveTextContent('Käthe Müßig: new, linked as parent');
    expect(rows[0]).toHaveTextContent('May be the same person as Jurgen Mussig');
    expect(rows[1]).toHaveTextContent('Not imported');
    expect(rows[1]).toHaveTextContent('Parent 1: Email is not valid.');
    expect(screen.getByRole('list', { name: 'Summary' })).toHaveTextContent('1 new');

    await user.click(screen.getByRole('button', { name: 'Import 1 row' }));

    expect(posts[2]?.body).toMatchObject({ apply: true });
    expect(await screen.findByRole('table', { name: 'Imported' })).toBeInTheDocument();
    const history = within(await screen.findByRole('table', { name: 'Import runs' }));
    expect(history.getByText('Mitglieder.csv')).toBeInTheDocument();
    expect(history.getByText('Olga Office')).toBeInTheDocument();
    expect(
      history.getByText('1 added, 0 updated, 0 unchanged, 1 not imported'),
    ).toBeInTheDocument();
    expect(history.getByText('Line 3: Parent 1: Email is not valid.')).toBeInTheDocument();
  });

  it('starts from the mapping this browser remembers', async () => {
    localStorage.setItem(
      mappingStorageKey,
      JSON.stringify({ columns: { notes: 'kurs', phone: 'Telefon' }, notes: [] }),
    );
    mockApiGet({
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/admin/imports': { body: { items: [] } },
    });
    mockApiPost({ '/api/v1/admin/imports/inspect': { body: inspection } });
    const user = userEvent.setup();

    renderWithQuery(<ImportPage />);
    await user.upload(screen.getByLabelText('CSV file'), file());

    expect(await screen.findByRole('combobox', { name: 'Notes' })).toHaveTextContent('Kurs');
    expect(screen.getByRole('combobox', { name: 'Phone' })).toHaveTextContent('Not imported');
  });

  it('says when it cannot read a file, and refuses a large one', async () => {
    mockApiGet({
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/admin/imports': { body: { items: [] } },
    });
    mockApiPost({
      '/api/v1/admin/imports/inspect': problem(422, 'validation failed', [
        { location: 'body.content', message: 'the file is empty' },
      ]),
    });
    const user = userEvent.setup();

    renderWithQuery(<ImportPage />);
    await user.upload(screen.getByLabelText('CSV file'), file());

    expect(
      await screen.findByText(
        'This file cannot be read as a table. Check that it is a CSV file with a header row.',
      ),
    ).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Try it' })).not.toBeInTheDocument();

    const large = new File([new Uint8Array((1 << 20) + 1)], 'big.csv', { type: 'text/csv' });
    await user.upload(screen.getByLabelText('CSV file'), large);
    expect(
      await screen.findByText('This file is larger than 1 MiB. Split it into smaller files.'),
    ).toBeInTheDocument();
  });

  it('offers the sample file as a download, without the network', async () => {
    mockApiGet({
      '/api/v1/settings/public': { body: publicSettings },
      '/api/v1/admin/imports': { body: { items: [] } },
    });
    const created: Blob[] = [];
    URL.createObjectURL = vi.fn((blob: Blob) => {
      created.push(blob);
      return 'blob:sample';
    });
    URL.revokeObjectURL = vi.fn();
    const click = vi
      .spyOn(HTMLAnchorElement.prototype, 'click')
      .mockImplementation(() => undefined);
    const user = userEvent.setup();

    renderWithQuery(<ImportPage />);
    await user.click(screen.getByRole('button', { name: 'Download the sample file' }));

    expect(click).toHaveBeenCalledOnce();
    expect(await created[0]?.text()).toMatch(/^Mitgliedsnummer;Vorname;Nachname;/);
  });
});
