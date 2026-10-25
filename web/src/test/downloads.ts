import { vi } from 'vitest';

/** A file the page handed to the browser to save. */
export type SavedFile = { name: string; blob: Blob };

/**
 * Catches the files a page saves through a Blob URL and an anchor click
 * (jsdom has no URL.createObjectURL and does not navigate), in order.
 */
export function captureDownloads(): SavedFile[] {
  const saved: SavedFile[] = [];
  let last: Blob | undefined;
  URL.createObjectURL = vi.fn((blob: Blob) => {
    last = blob;
    return 'blob:download';
  });
  URL.revokeObjectURL = vi.fn();
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
    this: HTMLAnchorElement,
  ) {
    if (last) saved.push({ name: this.download, blob: last });
  });
  return saved;
}
