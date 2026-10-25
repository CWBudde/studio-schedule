import { describe, expect, it } from 'vitest';
import { passwordStrength } from './passwordStrength';

describe('passwordStrength', () => {
  it.each([
    ['', 'weak'],
    ['short1!', 'weak'],
    ['aaaaaaaaaaaaaaaaaaaa', 'weak'], // long, but hardly any different characters
    ['abcdefgh', 'weak'], // 8 characters of one kind
    ['abcdefgh1', 'fair'],
    ['abcdefghijkl', 'fair'],
    ['Abcdefgh1', 'good'],
    ['abcdefghijk1', 'good'],
    ['correct horse battery', 'strong'], // long, letters and spaces
    ['Tr4mpoline!', 'good'], // every kind, but short
    ['Tr4mpoline!s', 'strong'],
  ] as const)('rates %j as %s', (password, expected) => {
    expect(passwordStrength(password)).toBe(expected);
  });

  it('counts characters, not bytes', () => {
    // Eight three-byte characters of one kind: as weak as eight letters.
    expect(passwordStrength('€€€€€€€€')).toBe('weak');
    expect(passwordStrength('äöüßéèêë1')).toBe('fair');
  });
});
