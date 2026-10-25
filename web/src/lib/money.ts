/**
 * Amounts of money. The API sends every amount in minor units (cents) of
 * the school's currency (`currency` of the public settings, ISO 4217); the
 * UI shows them in the UI language with Intl and reads typed amounts back
 * into minor units. Nothing here uses floating-point arithmetic on minor
 * units: a typed amount is parsed digit by digit.
 */

const formatters = new Map<string, Intl.NumberFormat>();

function currencyFormat(locale: string, currency: string): Intl.NumberFormat {
  const key = `${locale}|${currency}`;
  let f = formatters.get(key);
  if (!f) {
    f = new Intl.NumberFormat(locale, { style: 'currency', currency });
    formatters.set(key, f);
  }
  return f;
}

/** The number of minor-unit digits of a currency, e.g. 2 for EUR, 0 for JPY. */
export function currencyDigits(currency: string): number {
  return currencyFormat('en', currency).resolvedOptions().maximumFractionDigits ?? 2;
}

/** An amount in minor units as the UI language writes it, e.g. 15000 EUR → '€150.00' or '150,00 €'. */
export function formatMoney(minor: number, currency: string, locale: string): string {
  return currencyFormat(locale, currency).format(minor / 10 ** currencyDigits(currency));
}

const compactFormatters = new Map<string, Intl.NumberFormat>();

/**
 * An amount in minor units as a short label, e.g. for a chart axis:
 * 1234500 EUR → '€12.3K' (en) or '12.345 €' (de), 1500000 JPY → '¥1.5M'.
 */
export function formatCompactMoney(minor: number, currency: string, locale: string): string {
  const key = `${locale}|${currency}`;
  let f = compactFormatters.get(key);
  if (!f) {
    f = new Intl.NumberFormat(locale, {
      style: 'currency',
      currency,
      notation: 'compact',
      maximumFractionDigits: 1,
    });
    compactFormatters.set(key, f);
  }
  return f.format(minor / 10 ** currencyDigits(currency));
}

/**
 * An amount in minor units as a form field holds it, without the currency
 * and without grouping, in the UI language's decimal separator: 15000 EUR →
 * '150.00' (en) or '150,00' (de).
 */
export function moneyInput(minor: number, currency: string, locale: string): string {
  const digits = currencyDigits(currency);
  return new Intl.NumberFormat(locale, {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
    useGrouping: false,
  }).format(minor / 10 ** digits);
}

/**
 * The minor units of a typed amount, or undefined if it is none: digits
 * with at most `digits` decimals after a point or a comma, e.g. '150',
 * '150.5', '150,50' → 15050 for EUR. No sign, no grouping, no currency.
 */
export function parseMoney(text: string, digits: number): number | undefined {
  return parseDecimal(text, digits);
}

/**
 * A typed decimal as an integer of `digits` decimal places: '19' → 1900,
 * '7,5' → 750 for 2 digits; undefined for anything else (a sign, grouping,
 * more decimals, an empty text).
 */
export function parseDecimal(text: string, digits: number): number | undefined {
  const match = /^(\d+)(?:[.,](\d*))?$/.exec(text.trim());
  if (!match) return undefined;
  const [, whole = '', fraction = ''] = match;
  if (fraction.length > digits) return undefined;
  const value = Number(whole) * 10 ** digits + Number(fraction.padEnd(digits, '0') || '0');
  return Number.isSafeInteger(value) ? value : undefined;
}

/** A rate in basis points as the UI language writes a percentage, e.g. 1900 → '19 %' (de) or '19%' (en). */
export function formatRate(basisPoints: number, locale: string): string {
  return new Intl.NumberFormat(locale, { style: 'percent', maximumFractionDigits: 2 }).format(
    basisPoints / 10_000,
  );
}

/** A rate in basis points as a form field holds it, in percent: 1900 → '19', 750 → '7.5' (en) or '7,5' (de). */
export function rateInput(basisPoints: number, locale: string): string {
  return new Intl.NumberFormat(locale, { maximumFractionDigits: 2, useGrouping: false }).format(
    basisPoints / 100,
  );
}
