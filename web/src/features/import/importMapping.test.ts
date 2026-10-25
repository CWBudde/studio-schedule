import { describe, expect, it } from 'vitest';
import type { ImportInspection } from '@/api/queries';
import {
  assign,
  base64Of,
  initialMapping,
  mappingComplete,
  mappingStorageKey,
  rememberedMapping,
  rememberMapping,
  splitField,
  toggleNote,
} from './importMapping';

const inspection: ImportInspection = {
  encoding: 'utf-8',
  delimiter: 'semicolon',
  headers: ['Nr', 'Vorname', 'Nachname', 'Mail', 'Kurs'],
  rows: 1,
  sample: [['1', 'Ada', 'Example', 'ada@example.org', 'Ballet']],
  suggested: { columns: { givenName: 'Vorname', familyName: 'Nachname' } },
};

describe('initialMapping', () => {
  it('takes the suggestion, with the remembered columns the file has on top', () => {
    const mapping = initialMapping(inspection, {
      columns: { memberNo: 'NR', email: 'Mail', phone: 'Telefon', familyName: 'Vorname' },
      notes: ['kurs', 'Bemerkung'],
    });
    // The remembered family name takes "Vorname" from the given name.
    expect(mapping).toEqual({
      columns: { memberNo: 'Nr', email: 'Mail', familyName: 'Vorname' },
      notes: ['Kurs'],
    });
  });

  it('is the suggestion without anything remembered', () => {
    expect(initialMapping(inspection, undefined)).toEqual({
      columns: inspection.suggested.columns,
      notes: [],
    });
  });
});

describe('assign and toggleNote', () => {
  it('fills a field with a column, which leaves the field and the notes it was in', () => {
    let m = toggleNote({ columns: { givenName: 'Vorname' } }, 'Kurs', true);
    m = assign(m, 'notes', 'Kurs');
    expect(m).toEqual({ columns: { givenName: 'Vorname', notes: 'Kurs' }, notes: [] });
    m = assign(m, 'familyName', 'Vorname');
    expect(m).toEqual({ columns: { familyName: 'Vorname', notes: 'Kurs' }, notes: [] });
    expect(assign(m, 'notes', undefined)).toEqual({
      columns: { familyName: 'Vorname' },
      notes: [],
    });
    expect(toggleNote(toggleNote(m, 'Nr', true), 'Nr', false).notes).toEqual([]);
  });
});

describe('mappingComplete', () => {
  it('needs the member number and names, and a guardian’s names with its other columns', () => {
    const base = { columns: { memberNo: 'Nr', givenName: 'Vorname', familyName: 'Nachname' } };
    expect(mappingComplete(base)).toBe(true);
    expect(mappingComplete({ columns: { givenName: 'Vorname', familyName: 'Nachname' } })).toBe(
      false,
    );
    expect(mappingComplete({ columns: { ...base.columns, guardian2Email: 'Mail' } })).toBe(false);
    expect(
      mappingComplete({
        columns: {
          ...base.columns,
          guardian2Email: 'Mail',
          guardian2GivenName: 'a',
          guardian2FamilyName: 'b',
        },
      }),
    ).toBe(true);
  });
});

describe('splitField', () => {
  it('tells a guardian’s field from the member’s', () => {
    expect(splitField('birthDate')).toEqual({ slot: 0, base: 'birthDate' });
    expect(splitField('guardian2FamilyName')).toEqual({ slot: 2, base: 'familyName' });
  });
});

describe('the remembered mapping', () => {
  it('round-trips, and junk or a blocked storage is none', () => {
    const storage = new Map<string, string>();
    const fake = {
      getItem: (k: string) => storage.get(k) ?? null,
      setItem: (k: string, v: string) => void storage.set(k, v),
    } as unknown as Storage;
    rememberMapping({ columns: { memberNo: 'Nr' }, notes: ['Kurs'] }, fake);
    expect(rememberedMapping(fake)).toEqual({ columns: { memberNo: 'Nr' }, notes: ['Kurs'] });
    storage.set(mappingStorageKey, '{not json');
    expect(rememberedMapping(fake)).toBeUndefined();
    const blocked = {
      getItem: () => {
        throw new Error('blocked');
      },
      setItem: () => {
        throw new Error('blocked');
      },
    } as unknown as Storage;
    expect(rememberedMapping(blocked)).toBeUndefined();
    expect(() => rememberMapping({ columns: {} }, blocked)).not.toThrow();
  });
});

describe('base64Of', () => {
  it('encodes the bytes, Windows-1252 ones included', async () => {
    expect(await base64Of(new Blob([new Uint8Array([0x4d, 0xfc, 0x00, 0xff])]))).toBe('TfwA/w==');
  });
});
