import { transport } from '@/lib/env';
import { transportFetch } from './transport';

/**
 * Opens a backend-generated file (iCal, PDF, ...).
 *
 * HTTP mode: let the browser load the URL directly (cookies are sent).
 * WASM mode: there is no server, so fetch through the worker bridge and hand
 * the bytes to the browser as a Blob URL.
 */
export async function openDownload(path: string): Promise<void> {
  if (transport !== 'wasm') {
    window.open(path, '_blank', 'noopener');
    return;
  }

  const response = await transportFetch(path);
  if (!response.ok) throw new Error(`Download failed with status ${response.status}`);

  const blob = await response.blob();
  const url = URL.createObjectURL(blob);
  const filename = filenameFromDisposition(response.headers.get('content-disposition'));

  // An anchor click is not subject to popup blocking after the await above.
  const anchor = document.createElement('a');
  anchor.href = url;
  if (filename) {
    anchor.download = filename;
  } else {
    anchor.target = '_blank';
    anchor.rel = 'noopener';
  }
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  setTimeout(() => URL.revokeObjectURL(url), 60_000);
}

export function filenameFromDisposition(header: string | null): string | null {
  if (!header || !/^\s*attachment/i.test(header)) return null;
  const extended = /filename\*\s*=\s*(?:UTF-8'')?([^;]+)/i.exec(header);
  if (extended?.[1]) {
    try {
      return decodeURIComponent(extended[1].trim().replace(/^"|"$/g, ''));
    } catch {
      // fall through to the plain filename parameter
    }
  }
  const plain = /filename\s*=\s*("?)([^";]+)\1/i.exec(header);
  return plain?.[2]?.trim() ?? null;
}
