import { describe, expect, it } from 'vitest';
import {
  currencyDigits,
  formatCompactMoney,
  formatMoney,
  formatRate,
  moneyInput,
  parseDecimal,
  parseMoney,
  rateInput,
} from './money';

describe('money', () => {
  it('knows the minor units of a currency', () => {
    expect(currencyDigits('EUR')).toBe(2);
    expect(currencyDigits('JPY')).toBe(0);
  });

  it('formats minor units in the UI language', () => {
    expect(formatMoney(15000, 'EUR', 'en')).toBe('€150.00');
    expect(formatMoney(15050, 'EUR', 'de')).toMatch(/^150,50\s€$/);
    expect(formatMoney(1500, 'JPY', 'en')).toBe('¥1,500');
  });

  it('shortens minor units by the digits of the currency', () => {
    expect(formatCompactMoney(1234500, 'EUR', 'en')).toBe('€12.3K');
    expect(formatCompactMoney(15000, 'EUR', 'en')).toBe('€150');
    // No minor units: 1,500,000 yen.
    expect(formatCompactMoney(1500000, 'JPY', 'en')).toBe('¥1.5M');
    // Three digits: 1,234.500 dinars.
    expect(formatCompactMoney(1234500, 'KWD', 'en')).toMatch(/^KWD\s1\.2K$/);
  });

  it('writes an amount as a form field holds it', () => {
    expect(moneyInput(15000, 'EUR', 'en')).toBe('150.00');
    expect(moneyInput(123456, 'EUR', 'de')).toBe('1234,56');
  });

  it('reads typed amounts into minor units without floating-point errors', () => {
    expect(parseMoney('150', 2)).toBe(15000);
    expect(parseMoney(' 150.5 ', 2)).toBe(15050);
    expect(parseMoney('150,50', 2)).toBe(15050);
    expect(parseMoney('0.29', 2)).toBe(29);
    expect(parseMoney('1.005', 2)).toBeUndefined();
    expect(parseMoney('-1', 2)).toBeUndefined();
    expect(parseMoney('1.234,50', 2)).toBeUndefined();
    expect(parseMoney('€5', 2)).toBeUndefined();
    expect(parseMoney('', 2)).toBeUndefined();
    expect(parseMoney('1500', 0)).toBe(1500);
    expect(parseMoney('1500.5', 0)).toBeUndefined();
  });

  it('reads and writes VAT rates as basis points', () => {
    expect(parseDecimal('19', 2)).toBe(1900);
    expect(parseDecimal('7,5', 2)).toBe(750);
    expect(formatRate(1900, 'en')).toBe('19%');
    expect(formatRate(750, 'de')).toMatch(/^7,5\s%$/);
    expect(rateInput(750, 'de')).toBe('7,5');
    expect(rateInput(1900, 'en')).toBe('19');
  });
});
