import { useMutation } from '@tanstack/react-query';
import type { PersonExport } from '@/api/queries';
import { exportMyDataMutation, exportPersonMutation } from '@/api/queries';

/**
 * The file name of a data export: the person's name, ASCII only, and the
 * day it was made, e.g. `data-export-clara-amberly-2026-10-25.json`.
 */
export function exportFileName(data: PersonExport): string {
  const name = `${data.person.givenName} ${data.person.familyName}`
    .normalize('NFKD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
  const day = data.exportedAt.slice(0, 10);
  return `data-export-${name || 'person'}-${day}.json`;
}

/**
 * Hands a JSON document to the browser as a file to save. It is made from
 * the API's answer in the page, so it works alike over HTTP and in the
 * WebAssembly demo, which has no server to download from.
 */
export function saveJson(data: unknown, filename: string): void {
  const blob = new Blob([`${JSON.stringify(data, null, 2)}\n`], { type: 'application/json' });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename;
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  setTimeout(() => URL.revokeObjectURL(url), 60_000);
}

type ExportMutation = typeof exportPersonMutation | typeof exportMyDataMutation;

/**
 * Downloads the data export of a person with `mutation`: the office's
 * export of anyone, or a member's of their family. `download` resolves
 * after the file is handed over; a failure shows through `error`.
 */
export function useExportDownload(mutation: ExportMutation) {
  const exportData = useMutation(mutation);
  return {
    download: (personId: string) =>
      exportData.mutate(personId, {
        onSuccess: (data) => saveJson(data, exportFileName(data)),
      }),
    /** The person whose export is under way, if one is. */
    pendingFor: exportData.isPending ? exportData.variables : undefined,
    error: exportData.error,
    /** The person whose export failed, if the last one did. */
    failedFor: exportData.isError ? exportData.variables : undefined,
  };
}
