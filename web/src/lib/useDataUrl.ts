import { useEffect, useState } from 'react';

/** A file as a data URL ('data:image/png;base64,…'). */
export function fileDataUrl(file: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(reader.error ?? new Error('could not read the file'));
    reader.readAsDataURL(file);
  });
}

/**
 * `blob` as a data URL, once it is read: the content security policy allows
 * data: images, not blob: ones. Nothing to revoke, so it is safe however
 * often effects run.
 */
export function useDataUrl(blob: Blob | null | undefined): string | undefined {
  const [read, setRead] = useState<{ blob: Blob; url: string }>();
  useEffect(() => {
    if (!blob) return;
    let current = true;
    fileDataUrl(blob).then(
      (url) => current && setRead({ blob, url }),
      () => undefined,
    );
    return () => {
      current = false;
    };
  }, [blob]);
  return blob && read?.blob === blob ? read.url : undefined;
}
