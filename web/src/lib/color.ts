/**
 * The accent color's checks, as the server makes them (internal/domain/color.go):
 * the WCAG 2 contrast against the light theme's text on the primary color and
 * its page, and the variant the dark theme derives. The form shows them while
 * an admin picks a color; the server decides.
 */

/** An sRGB color, 8 bits per channel. */
type Rgb = readonly [number, number, number];

/** The color of '#rrggbb' (either case), or undefined. */
export function parseColor(hex: string): Rgb | undefined {
  if (!/^#[0-9a-fA-F]{6}$/.test(hex)) return undefined;
  const v = Number.parseInt(hex.slice(1), 16);
  return [(v >> 16) & 0xff, (v >> 8) & 0xff, v & 0xff];
}

/** '#rrggbb' in lower case. */
export function toHex([r, g, b]: Rgb): string {
  return `#${[r, g, b].map((c) => c.toString(16).padStart(2, '0')).join('')}`;
}

/** The linear value (0–1) of an sRGB channel. */
const linear = (v: number) => {
  const c = v / 255;
  return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
};

/** The sRGB channel of a linear value, clipped to 0–255. */
const gamma = (v: number) => {
  const c = Math.min(Math.max(v, 0), 1);
  return Math.round((c <= 0.0031308 ? c * 12.92 : 1.055 * c ** (1 / 2.4) - 0.055) * 255);
};

const luminance = ([r, g, b]: Rgb) => 0.2126 * linear(r) + 0.7152 * linear(g) + 0.0722 * linear(b);

/** The WCAG 2 contrast ratio of two colors, from 1 to 21. */
export function contrastRatio(a: Rgb, b: Rgb): number {
  const [la, lb] = [luminance(a), luminance(b)];
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}

function toOkLch([r, g, b]: Rgb): [number, number, number] {
  const [lr, lg, lb] = [linear(r), linear(g), linear(b)];
  const l = Math.cbrt(0.4122214708 * lr + 0.5363325363 * lg + 0.0514459929 * lb);
  const m = Math.cbrt(0.2119034982 * lr + 0.6806995451 * lg + 0.1073969566 * lb);
  const s = Math.cbrt(0.0883024619 * lr + 0.2817188376 * lg + 0.6299787005 * lb);
  const L = 0.2104542553 * l + 0.793617785 * m - 0.0040720468 * s;
  const A = 1.9779984951 * l - 2.428592205 * m + 0.4505937099 * s;
  const B = 0.0259040371 * l + 0.7827717662 * m - 0.808675766 * s;
  let hue = (Math.atan2(B, A) * 180) / Math.PI;
  if (hue < 0) hue += 360;
  return [L, Math.hypot(A, B), hue];
}

/** The color of OKLCh, and whether it lies inside sRGB (else it is clipped). */
function fromOkLch(L: number, chroma: number, hue: number): [Rgb, boolean] {
  const A = chroma * Math.cos((hue * Math.PI) / 180);
  const B = chroma * Math.sin((hue * Math.PI) / 180);
  const l = (L + 0.3963377774 * A + 0.2158037573 * B) ** 3;
  const m = (L - 0.1055613458 * A - 0.0638541728 * B) ** 3;
  const s = (L - 0.0894841775 * A - 1.291485548 * B) ** 3;
  const r = 4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s;
  const g = -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s;
  const bl = -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s;
  const eps = 1e-4;
  const inside = [r, g, bl].every((v) => v >= -eps && v <= 1 + eps);
  return [[gamma(r), gamma(g), gamma(bl)], inside];
}

const oklch = (L: number, chroma: number, hue: number) => fromOkLch(L, chroma, hue)[0];

// The theme colors of web/src/index.css that an accent must stand out against.
const lightPrimaryForeground = oklch(0.985, 0.008, 75);
const lightBackground = oklch(0.978, 0.008, 75);

/** The contrast the accent needs: WCAG 2 AA for normal text. */
export const minAccentContrast = 4.5;

/** The lower contrast of an accent against the light text on it and the page. */
export function accentContrast(accent: Rgb): number {
  return Math.min(
    contrastRatio(accent, lightPrimaryForeground),
    contrastRatio(accent, lightBackground),
  );
}

/**
 * The dark theme's variant of an accent: its hue at OKLab lightness 0.78,
 * the chroma capped at 0.1 and reduced until it fits into sRGB. The fixed
 * lightness keeps the dark text on it readable for every accent (checked
 * once for all in internal/domain/color_internal_test.go).
 */
export function darkAccent(accent: Rgb): Rgb {
  const [, c, hue] = toOkLch(accent);
  for (let chroma = Math.min(c, 0.1); chroma > 0; chroma -= 0.002) {
    const [rgb, inside] = fromOkLch(0.78, chroma, hue);
    if (inside) return rgb;
  }
  return oklch(0.78, 0, 0);
}
