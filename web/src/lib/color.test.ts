import { describe, expect, it } from 'vitest';
import { accentContrast, contrastRatio, darkAccent, parseColor, toHex } from './color';

const color = (hex: string) => {
  const c = parseColor(hex);
  if (!c) throw new Error(`not a color: ${hex}`);
  return c;
};

describe('parseColor', () => {
  it('reads #rrggbb in either case', () => {
    expect(toHex(color('#7A1F3D'))).toBe('#7a1f3d');
  });

  it.each(['', '7a1f3d', '#7a1f3', '#7a1g3d', 'red', '#fff', ' #7a1f3d'])('refuses %j', (s) => {
    expect(parseColor(s)).toBeUndefined();
  });
});

describe('contrastRatio', () => {
  it('follows WCAG 2', () => {
    expect(contrastRatio(color('#000000'), color('#ffffff'))).toBeCloseTo(21, 2);
    expect(contrastRatio(color('#767676'), color('#ffffff'))).toBeCloseTo(4.54, 2);
  });
});

// The vectors of internal/domain/color_test.go: both sides agree.
describe('accent', () => {
  it.each([
    ['#7a1f3d', '#ee9cae', 9.42],
    ['#1d4ed8', '#97b7f8', 6.28],
    ['#0f766e', '#72cac0', 5.13],
    ['#b45309', '#eca47e', 4.71],
    ['#ff0000', '#f19f92', 3.75],
    ['#808080', '#b7b7b7', 3.7],
    ['#000000', '#b7b7b7', 19.69],
  ])('%s: dark %s, contrast %d', (accent, dark, contrast) => {
    expect(toHex(darkAccent(color(accent)))).toBe(dark);
    expect(accentContrast(color(accent))).toBeCloseTo(contrast, 2);
  });
});
