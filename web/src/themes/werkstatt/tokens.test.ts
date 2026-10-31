import { describe, expect, it } from 'vitest';
import { contrastRatio, parseColor } from '@/lib/color';
import { courseTints } from './courseTint';

/**
 * Werkstatt's colour tokens (werkstatt.css), read as text: every text colour
 * must stand out against what it sits on (WCAG 2 AA), in light and dark.
 */

// The token blocks and their selectors. Their specificity is the point: the
// school's accent (`html:root`, `html.dark`; branding.ts) must beat the
// token blocks (0,1,0), and the day colours must beat the accent (0,2,0+).
const lightSelector = "[data-theme='werkstatt']";
const darkSelector = ":where(.dark)[data-theme='werkstatt']";
const lightDaySelector = ":root[data-theme='werkstatt']";
const darkDaySelector = ":root.dark[data-theme='werkstatt']";

/**
 * werkstatt.css as text. Not `?raw`: with `css: false` (vite.config.ts) Vitest
 * hands that out empty. Node's fs instead; the app's tsconfig has no Node
 * types, hence the untyped import (as in registry.test.ts).
 */
async function readCss(): Promise<string> {
  const nodeFs = 'node:fs';
  const fs = (await import(/* @vite-ignore */ nodeFs)) as {
    readFileSync(path: string, encoding: 'utf8'): string;
  };
  // Vitest runs tests in Node, which sets import.meta.dirname.
  const { dirname } = import.meta as ImportMeta & { dirname: string };
  return fs.readFileSync(`${dirname}/werkstatt.css`, 'utf8');
}

const source = (await readCss()).replace(/\/\*[\s\S]*?\*\//g, '');

/** The rules of the file: selector text and body, in order. */
const rules = [...source.matchAll(/([^{}]+)\{([^{}]*)\}/g)].map((m) => ({
  selector: (m[1] ?? '').trim().replace(/\s+/g, ' '),
  body: m[2] ?? '',
}));

/** The custom properties of the one rule with exactly this selector. */
function block(selector: string): Map<string, string> {
  const found = rules.filter((r) => r.selector === selector);
  if (found.length !== 1) throw new Error(`${found.length} rules for ${selector}, want one`);
  const props = new Map<string, string>();
  for (const m of (found[0]?.body ?? '').matchAll(/(--[\w-]+)\s*:\s*([^;]+);/g)) {
    props.set(m[1] ?? '', (m[2] ?? '').trim());
  }
  return props;
}

type Rgb = readonly [number, number, number];
interface Rgba {
  rgb: Rgb;
  alpha: number;
}

/** '#rrggbb' or 'rgb(r g b / a%)'. */
function parse(value: string): Rgba {
  const hex = parseColor(value);
  if (hex) return { rgb: hex, alpha: 1 };
  const m = /^rgb\((\d+) (\d+) (\d+)(?: \/ (\d+(?:\.\d+)?)%)?\)$/.exec(value);
  if (!m) throw new Error(`not a colour this test reads: ${value}`);
  return { rgb: [Number(m[1]), Number(m[2]), Number(m[3])], alpha: m[4] ? Number(m[4]) / 100 : 1 };
}

/** A colour composited over an opaque one, as the browser paints it. */
function over(top: Rgba, below: Rgb): Rgb {
  const mix = (i: 0 | 1 | 2) => Math.round(top.rgb[i] * top.alpha + below[i] * (1 - top.alpha));
  return [mix(0), mix(1), mix(2)];
}

/** The tokens of a mode: dark mode is the light block overridden by the dark one. */
function tokens(mode: 'light' | 'dark'): (name: string) => Rgba {
  const light = new Map([...block(lightSelector), ...block(lightDaySelector)]);
  const all =
    mode === 'light'
      ? light
      : new Map([...light, ...block(darkSelector), ...block(darkDaySelector)]);
  return (name) => {
    const v = all.get(name);
    if (v === undefined) throw new Error(`${mode} has no ${name}`);
    return parse(v);
  };
}

/** The opaque colour of a surface: translucent ones over the page background. */
function surfaces(mode: 'light' | 'dark') {
  const t = tokens(mode);
  const background = t('--background').rgb;
  const on = (name: string, below: Rgb = background) => over(t(name), below);
  const panel = on('--werkstatt-panel');
  return {
    t,
    background,
    card: on('--card'),
    cardOnPanel: on('--card', panel),
    popover: on('--popover'),
    panel,
    header: on('--werkstatt-header'),
    menu: on('--werkstatt-menu'),
    muted: on('--muted'),
    secondary: on('--secondary'),
    accent: on('--accent'),
    sidebar: on('--sidebar'),
    destructiveTint: on('--destructive', background),
  };
}

/** Text on a surface: the text colour is composited over it too. */
function contrast(text: Rgba, surface: Rgb): number {
  return contrastRatio(over(text, surface), surface);
}

describe.each(['light', 'dark'] as const)('werkstatt tokens, %s', (mode) => {
  const s = surfaces(mode);
  const { t } = s;

  // [what protects, text token, surface]
  const pairs: [string, string, Rgb][] = [
    ['body text on the page', '--foreground', s.background],
    ['body text on a card', '--foreground', s.card],
    ['body text on the translucent page panel', '--foreground', s.panel],
    ['body text on the header bar (school name)', '--foreground', s.header],
    ['body text on a popover/dialog', '--foreground', s.popover],
    ['card text on a card', '--card-foreground', s.card],
    ['card text on a card that sits on the panel', '--card-foreground', s.cardOnPanel],
    ['popover text on a popover/dialog', '--popover-foreground', s.popover],
    ['muted text on the page', '--muted-foreground', s.background],
    ['muted text on a card', '--muted-foreground', s.card],
    ['muted text on the page panel', '--muted-foreground', s.panel],
    ['muted text on a muted surface', '--muted-foreground', s.muted],
    ['muted text in a popover/dialog', '--muted-foreground', s.popover],
    ['links (primary) on the page', '--primary', s.background],
    ['links (primary) on a card', '--primary', s.card],
    ['links (primary) on the page panel', '--primary', s.panel],
    ['links (primary) in a popover/dialog', '--primary', s.popover],
    ['destructive text on the page', '--destructive', s.background],
    ['destructive text on a card', '--destructive', s.card],
    ['secondary button text', '--secondary-foreground', s.secondary],
    ['accent (hover/selected) text', '--accent-foreground', s.accent],
    ['sidebar text', '--sidebar-foreground', s.sidebar],
    ['tab text on the header bar', '--werkstatt-tab', s.header],
    ['tab text in the phone drop menu', '--werkstatt-tab', s.menu],
    ['body text in the phone drop menu', '--foreground', s.menu],
    ['participant count (free places) on a card', '--werkstatt-green', s.card],
    ['participant count (free places) on a card on the panel', '--werkstatt-green', s.cardOnPanel],
    ['participant count (full) on a card on the panel', '--destructive', s.cardOnPanel],
  ];

  it.each(pairs)('%s reaches 4.5:1 (%s)', (_what, text, surface) => {
    expect(contrast(t(text), surface)).toBeGreaterThanOrEqual(4.5);
  });

  // [what protects, text token, background token]
  const solid: [string, string, string][] = [
    ['text on a primary button', '--primary-foreground', '--primary'],
    ['text on a sidebar primary item', '--sidebar-primary-foreground', '--sidebar-primary'],
    ["tomorrow's day band", '--day-tomorrow-foreground', '--day-tomorrow'],
    ['the later days’ band', '--day-later-foreground', '--day-later'],
    ['the book button (mint)', '--werkstatt-mint-foreground', '--werkstatt-mint'],
    [
      'the cancel button (its face is red-dark)',
      '--werkstatt-red-foreground',
      '--werkstatt-red-dark',
    ],
    ['the waiting list button (brown)', '--werkstatt-brown-foreground', '--werkstatt-brown'],
  ];

  it.each(solid)('%s reaches 4.5:1 (%s on %s)', (_what, text, surface) => {
    const below = over(t(surface), s.background);
    expect(contrast(t(text), below)).toBeGreaterThanOrEqual(4.5);
  });

  it('destructive text on its tinted button reaches 4.5:1', () => {
    // The destructive button and badge: text-destructive on bg-destructive/10 (dark: /20).
    const tint = { rgb: t('--destructive').rgb, alpha: mode === 'light' ? 0.1 : 0.2 };
    expect(contrast(t('--destructive'), over(tint, s.background))).toBeGreaterThanOrEqual(4.5);
  });

  it("today's day band reaches 4.5:1 at any text size (pages without a Werkstatt slot use it too)", () => {
    const band = over(t('--day-today'), s.background);
    expect(contrast(t('--day-today-foreground'), band)).toBeGreaterThanOrEqual(4.5);
  });
});

describe('werkstatt lesson card tints (light mode)', () => {
  const { t } = surfaces('light');
  const texts = [
    '--card-foreground',
    '--muted-foreground',
    '--primary',
    '--werkstatt-green',
    '--destructive',
  ];

  it.each(courseTints.flatMap((tint) => texts.map((text) => [text, tint] as const)))(
    '%s on %s reaches 4.5:1',
    (text, tint) => {
      const surface = parse(tint).rgb;
      expect(contrast(t(text), surface)).toBeGreaterThanOrEqual(4.5);
    },
  );
});

describe('werkstatt token blocks', () => {
  it('the dark block redefines every property of the light block (the light one beats index.css .dark)', () => {
    expect([...block(darkSelector).keys()].sort()).toEqual([...block(lightSelector).keys()].sort());
  });

  it('the dark day block redefines every day colour of the light one', () => {
    expect([...block(darkDaySelector).keys()].sort()).toEqual(
      [...block(lightDaySelector).keys()].sort(),
    );
  });

  it('the token blocks are 0,1,0, so the school accent (html:root, html.dark) beats them', () => {
    const accentTokens = ['--primary', '--ring', '--sidebar-primary', '--sidebar-ring'];
    for (const selector of [lightSelector, darkSelector]) {
      const props = block(selector);
      for (const name of accentTokens) expect(props.has(name), `${selector} ${name}`).toBe(true);
    }
    // No other rule may set an accent token: a stronger one would beat the accent.
    for (const r of rules) {
      if (r.selector === lightSelector || r.selector === darkSelector) continue;
      for (const name of accentTokens) {
        expect(r.body.includes(`${name}:`), `${r.selector} sets ${name}`).toBe(false);
      }
    }
  });

  it('the day colours sit in 0,2,0+ blocks, so the accent does not repaint the today band', () => {
    expect(block(lightDaySelector).has('--day-today')).toBe(true);
    expect(block(darkDaySelector).has('--day-today')).toBe(true);
    expect(block(lightSelector).has('--day-today')).toBe(false);
  });

  it('the page wash has no fallback image, so a school without a background shows the plain colour', () => {
    const uses = [...source.matchAll(/var\(--school-background([^)]*)\)/g)];
    expect(uses.length).toBeGreaterThan(0);
    for (const m of uses) expect(m[1]).toBe('');
  });

  it("headings use Roboto Condensed, never Programme's Bodoni", () => {
    const heading = rules.find((r) =>
      r.selector.includes("[data-theme='werkstatt'] .font-heading"),
    );
    expect(heading?.body).toMatch(/font-family:\s*'Roboto Condensed'/);
  });

  it('loads only the latin subsets of Roboto Condensed, each with its range (the PWA precaches every woff2)', () => {
    expect(source).not.toMatch(/@import/);
    const faces = rules.filter((r) => r.selector === '@font-face');
    expect(faces).toHaveLength(6);
    for (const face of faces) {
      expect(face.body).toMatch(
        /url\('@fontsource\/roboto-condensed\/files\/roboto-condensed-latin(-ext)?-[347]00-normal\.woff2'\)/,
      );
      // Without a range the later face would hide the earlier one for every character.
      expect(face.body).toMatch(/unicode-range:/);
    }
  });
});
