import { z } from 'zod';
import type { ReceiptSettings, ReceiptSettingsData } from '@/api/queries';

// Limits as in the API (ReceiptSettingsData). Messages are keys of
// `auth:form.errors` or `admin:form.errors` (see useAdminErrorText).

const text = (max: number) => z.string().trim().max(max, 'tooLong');

/**
 * The receipt settings form: every field may stay empty (the API says what
 * receipts still need), but the prefix of the receipt numbers (1–16 letters
 * A–Z and digits, upper-cased here). The output is the request body
 * without the empty fields.
 */
export const receiptSettingsSchema = z
  .object({
    issuerName: text(200),
    issuerAddress: text(500),
    taxNumber: text(50),
    email: text(254).refine((v) => v === '' || z.email().safeParse(v).success, 'email'),
    phone: text(50),
    taxMode: z.enum(['', 'small_business', 'exempt', 'standard']),
    taxNote: text(500),
    footer: text(500),
    receiptPrefix: z
      .string()
      .trim()
      .toUpperCase()
      .regex(/^[A-Z0-9]{1,16}$/, 'receiptPrefix'),
  })
  .transform(({ taxMode, receiptPrefix, ...rest }): ReceiptSettingsData => ({
    ...Object.fromEntries(Object.entries(rest).filter(([, v]) => v !== '')),
    ...(taxMode ? { taxMode } : {}),
    receiptPrefix,
  }));

export type ReceiptSettingsValues = z.input<typeof receiptSettingsSchema>;

export const receiptSettingsFields = [
  'issuerName',
  'issuerAddress',
  'taxNumber',
  'email',
  'phone',
  'taxMode',
  'taxNote',
  'footer',
  'receiptPrefix',
] as const satisfies (keyof ReceiptSettingsValues)[];

/** The form values of the settings as the API has them. */
export const receiptSettingsValues = (s: ReceiptSettings): ReceiptSettingsValues => ({
  issuerName: s.issuerName ?? '',
  issuerAddress: s.issuerAddress ?? '',
  taxNumber: s.taxNumber ?? '',
  email: s.email ?? '',
  phone: s.phone ?? '',
  taxMode: s.taxMode ?? '',
  taxNote: s.taxNote ?? '',
  footer: s.footer ?? '',
  receiptPrefix: s.receiptPrefix,
});

/** A file as a data URL ('data:image/png;base64,…'). */
export function fileDataUrl(file: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(reader.error ?? new Error('could not read the file'));
    reader.readAsDataURL(file);
  });
}

/** The base64 data of a file, without the data URL's prefix. */
export async function fileBase64(file: Blob): Promise<string> {
  return (await fileDataUrl(file)).replace(/^data:[^,]*,/, '');
}
