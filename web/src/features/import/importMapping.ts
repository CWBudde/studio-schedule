import type { ImportField, ImportInspection, ImportMapping } from '@/api/queries';

/** The fields of the member, then of each guardian, in form order. */
export const memberFields = [
  'memberNo',
  'givenName',
  'familyName',
  'email',
  'phone',
  'birthDate',
  'notes',
] as const satisfies ImportField[];

export const guardianFields = (slot: 1 | 2) =>
  [
    `guardian${slot}GivenName`,
    `guardian${slot}FamilyName`,
    `guardian${slot}Email`,
    `guardian${slot}Phone`,
  ] as const satisfies ImportField[];

/** The fields a mapping must fill: the member number and the member's names. */
export const requiredFields = ['memberNo', 'givenName', 'familyName'] as const;

/** The member field a guardian's field corresponds to, for its label. */
export type BaseField = (typeof memberFields)[number];

/** The guardian slot of a field (0 for the member's) and its base field. */
export function splitField(field: ImportField): { slot: 0 | 1 | 2; base: BaseField } {
  const m = /^guardian([12])(\w+)$/.exec(field);
  if (!m) return { slot: 0, base: field as BaseField };
  const rest = m[2] ?? '';
  return {
    slot: m[1] === '1' ? 1 : 2,
    base: (rest.charAt(0).toLowerCase() + rest.slice(1)) as BaseField,
  };
}

/** Where the browser remembers the last mapping (ADR 0015: per browser). */
export const mappingStorageKey = 'studio-schedule.import-mapping';

/** The mapping remembered in this browser, if any and readable. */
export function rememberedMapping(storage: Storage | undefined = globalThis.localStorage) {
  try {
    const raw = storage?.getItem(mappingStorageKey);
    if (!raw) return undefined;
    const parsed = JSON.parse(raw) as Partial<ImportMapping>;
    return {
      columns: typeof parsed.columns === 'object' && parsed.columns ? parsed.columns : {},
      notes: Array.isArray(parsed.notes) ? parsed.notes.filter((n) => typeof n === 'string') : [],
    } satisfies ImportMapping;
  } catch {
    return undefined;
  }
}

/** Remembers a mapping in this browser; a full or blocked storage is no error. */
export function rememberMapping(
  mapping: ImportMapping,
  storage: Storage | undefined = globalThis.localStorage,
) {
  try {
    storage?.setItem(mappingStorageKey, JSON.stringify(mapping));
  } catch {
    // The mapping is only a convenience.
  }
}

/**
 * The mapping a file starts with: the headers the server recognized, with
 * what this browser remembers on top, for the headers the file has
 * (ignoring case). A header fills one field at most.
 */
export function initialMapping(
  inspection: ImportInspection,
  remembered: ImportMapping | undefined,
): ImportMapping {
  const header = (name: string) =>
    inspection.headers.find((h) => h.toLowerCase() === name.toLowerCase());
  let mapping: ImportMapping = { columns: { ...inspection.suggested.columns }, notes: [] };
  if (remembered) {
    for (const [field, name] of Object.entries(remembered.columns) as [ImportField, string][]) {
      const h = name ? header(name) : undefined;
      if (h) mapping = assign(mapping, field, h);
    }
    for (const name of remembered.notes ?? []) {
      const h = header(name);
      if (h && !Object.values(mapping.columns).includes(h)) {
        mapping = { ...mapping, notes: [...(mapping.notes ?? []), h] };
      }
    }
  }
  return mapping;
}

/**
 * The mapping with `field` filled by the column `header` (none for
 * undefined); a field that had the header before loses it, and so do the
 * notes.
 */
export function assign(
  mapping: ImportMapping,
  field: ImportField,
  header: string | undefined,
): ImportMapping {
  const columns = Object.fromEntries(
    Object.entries(mapping.columns).filter(([f, h]) => f !== field && h !== header),
  ) as ImportMapping['columns'];
  if (header) columns[field] = header;
  return { columns, notes: (mapping.notes ?? []).filter((h) => h !== header) };
}

/** The mapping with `header` going into the notes, or not. */
export function toggleNote(mapping: ImportMapping, header: string, on: boolean): ImportMapping {
  const notes = (mapping.notes ?? []).filter((h) => h !== header);
  return { ...mapping, notes: on ? [...notes, header] : notes };
}

/** Whether the mapping fills the required fields and a guardian's names with any of its columns. */
export function mappingComplete(mapping: ImportMapping): boolean {
  const has = (f: ImportField) => Boolean(mapping.columns[f]);
  if (!requiredFields.every(has)) return false;
  return ([1, 2] as const).every((slot) => {
    const fields = guardianFields(slot);
    return !fields.some(has) || (has(fields[0]) && has(fields[1]));
  });
}

/** The bytes of a file, base64 encoded, as the API takes them. */
export async function base64Of(file: Blob): Promise<string> {
  const bytes = new Uint8Array(await file.arrayBuffer());
  let binary = '';
  for (let i = 0; i < bytes.length; i += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  }
  return btoa(binary);
}

/** The largest file the API takes (csvimport.MaxBytes). */
export const maxImportBytes = 1 << 20;
