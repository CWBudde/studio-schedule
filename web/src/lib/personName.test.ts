import { afterEach, describe, expect, it } from 'vitest';
import i18n from '@/i18n';
import { personName, shownName } from './personName';

afterEach(async () => {
  await i18n.changeLanguage('en');
});

describe('personName', () => {
  it('joins the given and the family name', () => {
    expect(personName({ givenName: 'Mia', familyName: 'Sample' })).toBe('Mia Sample');
    expect(shownName({ name: 'Mia Sample' })).toBe('Mia Sample');
  });

  it('shows an anonymized person as "Deleted person" in the current language', async () => {
    // The API keeps a fixed placeholder for an anonymized person (ADR 0016).
    const stored = { givenName: 'Deleted', familyName: 'person' };
    expect(personName({ ...stored, anonymized: true })).toBe('Deleted person');
    expect(personName({ ...stored, anonymizedAt: '2026-10-25T10:00:00Z' })).toBe('Deleted person');
    expect(shownName({ name: 'Deleted person', anonymized: true })).toBe('Deleted person');

    await i18n.changeLanguage('de');
    expect(personName({ ...stored, anonymized: true })).toBe('Gelöschte Person');
    expect(shownName({ name: 'Deleted person', anonymized: true })).toBe('Gelöschte Person');
  });
});
