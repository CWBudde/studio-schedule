import { z } from 'zod';
import type { AssignPassData, PersonDetail, PersonMatch, SellPassData } from '@/api/queries';
import { moneyValue } from './passTypeForms';

// Limits as in the API (SellPassData, AssignPassData). Messages are keys of
// `auth:form.errors` or `admin:form.errors` (see useAdminErrorText).

/** The longest note of a pass. */
export const maxPassNote = 500;

/**
 * The fields of both forms: the type, the first day ('' for today), a note,
 * and for a sale who paid, the price paid ('' for the type's), how, and
 * the recipient printed on the receipt (name, email and address; '' for
 * none). Giving a pass away ignores the sale's fields.
 */
const fields = {
  passTypeId: z.string().min(1, 'choose'),
  validFrom: z
    .string()
    .trim()
    .regex(/^(\d{4}-\d{2}-\d{2})?$/, 'invalid'),
  note: z.string().trim().max(maxPassNote, 'tooLong'),
  payerId: z.string(),
  price: z.string().trim(),
  paymentMethod: z.enum(['cash', 'transfer', 'card']),
  recipientName: z.string().trim().max(200, 'tooLong'),
  recipientEmail: z
    .string()
    .trim()
    .max(254, 'tooLong')
    .refine((v) => v === '' || z.email().safeParse(v).success, 'email'),
  recipientAddress: z.string().trim().max(500, 'tooLong'),
};

/** What the office gives away: a type, a first day and a note. */
export const assignPassSchema = z
  .object(fields)
  .transform(({ passTypeId, validFrom, note }): AssignPassData => ({
    passTypeId,
    ...(validFrom ? { validFrom } : {}),
    ...(note ? { note } : {}),
  }));

/** The values of both forms. */
export type PassSaleValues = z.input<typeof assignPassSchema>;

export const assignPassFields = [
  'passTypeId',
  'validFrom',
  'note',
] as const satisfies (keyof PassSaleValues)[];

/**
 * What the office sells, for a currency with `digits` decimals: the common
 * fields, who paid (the owner unless someone else is picked) and the price
 * paid ('' for the type's). The output is the request body; a new field of
 * the sale goes into the object, the fields list and `passSaleValues`.
 */
export const sellPassSchema = (digits: number, ownerId: string) =>
  z.object(fields).transform((values, ctx): SellPassData => {
    const { passTypeId, validFrom, note, payerId, price, paymentMethod } = values;
    const { recipientName, recipientEmail, recipientAddress } = values;
    const minor = price ? moneyValue(price, digits) : undefined;
    if (price && minor === undefined) {
      ctx.addIssue({ code: 'custom', path: ['price'], message: 'money' });
    }
    return {
      passTypeId,
      ...(payerId && payerId !== ownerId ? { payerId } : {}),
      ...(validFrom ? { validFrom } : {}),
      ...(minor !== undefined ? { price: minor } : {}),
      ...(note ? { note } : {}),
      paymentMethod,
      ...(recipientName ? { recipientName } : {}),
      ...(recipientEmail ? { recipientEmail } : {}),
      ...(recipientAddress ? { recipientAddress } : {}),
    };
  });

export const sellPassFields = [
  'passTypeId',
  'validFrom',
  'note',
  'payerId',
  'price',
  'paymentMethod',
  'recipientName',
  'recipientEmail',
  'recipientAddress',
] as const satisfies (keyof PassSaleValues)[];

/**
 * The empty form of a sale paid in cash by `payer`, who is the recipient of
 * the receipt (or of a pass given away, without one).
 */
export const passSaleValues = (payer?: PersonMatch): PassSaleValues => ({
  passTypeId: '',
  validFrom: '',
  note: '',
  payerId: payer?.id ?? '',
  price: '',
  paymentMethod: 'cash',
  recipientName: payer ? `${payer.givenName} ${payer.familyName}` : '',
  recipientEmail: payer?.email ?? '',
  recipientAddress: '',
});

/**
 * Who paid by default: the person's first guardian (a parent pays for a
 * child), else the person.
 */
export function defaultPayer(person: PersonDetail): PersonMatch {
  return person.guardians[0] ?? person;
}
