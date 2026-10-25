import { z } from 'zod';
import type { PassType, PassTypeData } from '@/api/queries';
import { moneyInput, parseDecimal, parseMoney, rateInput } from '@/lib/money';
import { catalogueName } from './locationForms';

// Limits as in the API (PassTypeData). Messages are keys of
// `auth:form.errors` or `admin:form.errors` (see useAdminErrorText).

/** The most visits of a visit pass. */
export const maxPassVisits = 1000;
/** The longest validity of a pass, in days. */
export const maxPassValidityDays = 3660;
/** The highest price, in minor units. */
export const maxPassPrice = 100_000_000;

/** A whole number as typed into a number input; undefined unless it is one. */
const wholeNumber = (text: string) => (/^\d+$/.test(text.trim()) ? Number(text.trim()) : undefined);

/** A typed amount of money in minor units (`digits` decimals), if it is one from 0 to the maximum. */
export function moneyValue(text: string, digits: number): number | undefined {
  const minor = parseMoney(text, digits);
  return minor !== undefined && minor <= maxPassPrice ? minor : undefined;
}

/** A typed VAT rate in percent (up to two decimals) as basis points, if it is one from 0 to 10000. */
function vatValue(text: string): number | undefined {
  const basisPoints = parseDecimal(text, 2);
  return basisPoints !== undefined && basisPoints <= 10_000 ? basisPoints : undefined;
}

/**
 * A pass type's form, for a currency with `digits` decimals: the inputs
 * hold text. A visit pass has 1–1000 visits and is valid 0 (no expiry) to
 * 3660 days; a time pass lasts 1–3660 days and has no visits. Every field
 * is checked at once (a field schema that fails would skip the others),
 * and the output is the request body.
 */
export const passTypeSchema = (digits: number) =>
  z
    .object({
      name: z.string(),
      kind: z.enum(['visits', 'time']),
      visits: z.string(),
      validityDays: z.string(),
      price: z.string(),
      vatRate: z.string(),
      active: z.boolean(),
    })
    .transform((v, ctx): PassTypeData => {
      const issue = (path: keyof typeof v, message: string) =>
        ctx.addIssue({ code: 'custom', path: [path], message });
      const name = catalogueName.safeParse(v.name);
      if (!name.success) issue('name', name.error.issues[0]?.message ?? 'invalid');
      const visitPass = v.kind === 'visits';
      const visits = wholeNumber(v.visits);
      if (visitPass && (visits === undefined || visits < 1 || visits > maxPassVisits)) {
        issue('visits', 'visits');
      }
      const days = wholeNumber(v.validityDays);
      if (days === undefined || days < (visitPass ? 0 : 1) || days > maxPassValidityDays) {
        issue('validityDays', visitPass ? 'validityDays' : 'timeDays');
      }
      const price = moneyValue(v.price, digits);
      if (price === undefined) issue('price', v.price.trim() ? 'money' : 'required');
      const vatRate = vatValue(v.vatRate);
      if (vatRate === undefined) issue('vatRate', v.vatRate.trim() ? 'vatRate' : 'required');
      return {
        name: name.data ?? '',
        kind: v.kind,
        ...(visitPass ? { visits: visits ?? 0 } : {}),
        validityDays: days ?? 0,
        price: price ?? 0,
        vatRate: vatRate ?? 0,
        active: v.active,
      };
    });

export type PassTypeValues = z.input<ReturnType<typeof passTypeSchema>>;

export const passTypeFields = [
  'name',
  'kind',
  'visits',
  'validityDays',
  'price',
  'vatRate',
  'active',
] as const satisfies (keyof PassTypeValues)[];

/**
 * The form values of a pass type, or of a new, active 10-visit pass; the
 * amounts as the UI language writes them.
 */
export const passTypeValues = (
  currency: string,
  locale: string,
  passType?: PassType,
): PassTypeValues => ({
  name: passType?.name ?? '',
  kind: passType?.kind ?? 'visits',
  visits: String(passType ? (passType.visits ?? '') : 10),
  validityDays: String(passType?.validityDays ?? 180),
  price: passType ? moneyInput(passType.price, currency, locale) : '',
  vatRate: passType ? rateInput(passType.vatRate, locale) : '',
  active: passType?.active ?? true,
});
