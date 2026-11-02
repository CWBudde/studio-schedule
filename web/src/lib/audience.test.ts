import { describe, expect, it } from 'vitest';
import i18n from '@/i18n';
import { type AudienceFields, formatAgeYears, formatAudience } from './audience';

const beginner = { id: 'l1', name: 'Beginner', code: 'B' };
const intermediate = { id: 'l2', name: 'Intermediate', code: 'I' };
const anfaenger = { id: 'l1', name: 'Anfänger', code: 'A' };
const mittelstufe = { id: 'l2', name: 'Mittelstufe', code: 'M' };
const noCode = { id: 'l3', name: 'Open Floor' };

const en = i18n.getFixedT('en', 'schedule');
const de = i18n.getFixedT('de', 'schedule');

const all = (a: AudienceFields) => ({
  enLong: formatAudience(a, en, 'long'),
  enShort: formatAudience(a, en, 'short'),
  deLong: formatAudience(a, de, 'long'),
  deShort: formatAudience(a, de, 'short'),
});

describe('formatAudience', () => {
  it('names a span, the stage and the ages', () => {
    const span = { stage: 2, ageFromMonths: 48, ageToMonths: 72 };
    expect(
      formatAudience({ ...span, levelFrom: beginner, levelTo: intermediate }, en, 'long'),
    ).toBe('Beginner–Intermediate · Stage 2 · 4–6 yrs');
    expect(
      formatAudience({ ...span, levelFrom: beginner, levelTo: intermediate }, en, 'short'),
    ).toBe('B/I · Stage 2 · 4–6 yrs');
    expect(
      formatAudience({ ...span, levelFrom: anfaenger, levelTo: mittelstufe }, de, 'long'),
    ).toBe('Anfänger–Mittelstufe · Stufe 2 · 4–6 J.');
    expect(
      formatAudience({ ...span, levelFrom: anfaenger, levelTo: mittelstufe }, de, 'short'),
    ).toBe('A/M · Stufe 2 · 4–6 J.');
  });

  it('shows one level by its name or code only', () => {
    expect(all({ levelFrom: beginner, levelTo: beginner })).toEqual({
      enLong: 'Beginner',
      enShort: 'B',
      deLong: 'Beginner',
      deShort: 'B',
    });
  });

  it('falls back to the name in the short form of a level without a code', () => {
    expect(formatAudience({ levelFrom: noCode, levelTo: noCode }, en, 'short')).toBe('Open Floor');
    expect(formatAudience({ levelFrom: beginner, levelTo: noCode }, de, 'short')).toBe(
      'B/Open Floor',
    );
  });

  it('names all levels', () => {
    expect(all({ allLevels: true })).toEqual({
      enLong: 'All levels',
      enShort: 'All levels',
      deLong: 'alle Level',
      deShort: 'alle Level',
    });
    expect(formatAudience({ allLevels: true, stage: 1 }, en, 'long')).toBe('All levels · Stage 1');
  });

  it('names an age range open at one end', () => {
    expect(all({ ageFromMonths: 42 })).toEqual({
      enLong: 'from 3½ yrs',
      enShort: 'from 3½ yrs',
      deLong: 'ab 3½ J.',
      deShort: 'ab 3½ J.',
    });
    expect(all({ ageToMonths: 72 })).toEqual({
      enLong: 'up to 6 yrs',
      enShort: 'up to 6 yrs',
      deLong: 'bis 6 J.',
      deShort: 'bis 6 J.',
    });
  });

  it('is empty when the class states nothing', () => {
    expect(all({})).toEqual({ enLong: '', enShort: '', deLong: '', deShort: '' });
    expect(formatAudience({ allLevels: false, stage: 0 }, en, 'long')).toBe('');
  });
});

describe('formatAgeYears', () => {
  it('shows years with halves, rounding down to a half year', () => {
    expect(formatAgeYears(40)).toBe('3');
    expect(formatAgeYears(42)).toBe('3½');
    expect(formatAgeYears(47)).toBe('3½');
    expect(formatAgeYears(48)).toBe('4');
    expect(formatAgeYears(6)).toBe('½');
    expect(formatAgeYears(216)).toBe('18');
  });
});
