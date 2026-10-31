import { describe, expect, expectTypeOf, it } from 'vitest';
import type { components } from '@/api/schema';
import { programme } from './programme';
import { loadTheme, themeIds } from './registry';
import type { Slots, ThemeId } from './slots';

/**
 * The generated API description (api/openapi.json, outside Vite's root), read
 * with Node's fs: the app's tsconfig has no Node types, hence the untyped import.
 */
async function apiThemes(): Promise<string[]> {
  const nodeFs = 'node:fs';
  const fs = (await import(/* @vite-ignore */ nodeFs)) as {
    readFileSync(path: string, encoding: 'utf8'): string;
  };
  // Vitest runs tests in Node, which sets import.meta.dirname.
  const { dirname } = import.meta as ImportMeta & { dirname: string };
  const file = `${dirname}/../../../api/openapi.json`;
  const spec = JSON.parse(fs.readFileSync(file, 'utf8')) as {
    components: { schemas: { PublicSettings: { properties: { theme: { enum: string[] } } } } };
  };
  return spec.components.schemas.PublicSettings.properties.theme.enum;
}

const slotNames: (keyof Slots)[] = [
  'Shell',
  'ScheduleList',
  'DayHeading',
  'LessonCard',
  'Home',
  'Profile',
  'Participants',
];

describe('registry', () => {
  it('loads Programme with every slot', async () => {
    const theme = await loadTheme('programme');

    expect(theme).toBe(programme);
    expect(Object.keys(theme.slots).sort()).toEqual([...slotNames].sort());
  });

  it('loads Werkstatt with its own Shell, the other slots falling back to Programme', async () => {
    const theme = await loadTheme('werkstatt');

    expect(theme.id).toBe('werkstatt');
    expect(theme.slots.Shell).toBeDefined();
    expect(theme.slots.Shell).not.toBe(programme.slots.Shell);
  });

  it("loads Werkstatt's own Home, Profile and Participants", async () => {
    const { slots } = await loadTheme('werkstatt');

    for (const name of ['Home', 'Profile', 'Participants'] as const) {
      expect(slots[name], name).toBeDefined();
      expect(slots[name], name).not.toBe(programme.slots[name]);
    }
  });

  it("knows exactly the API's themes", async () => {
    expect([...themeIds].sort()).toEqual((await apiThemes()).sort());
    expectTypeOf<ThemeId>().toEqualTypeOf<components['schemas']['PublicSettings']['theme']>();
  });
});
