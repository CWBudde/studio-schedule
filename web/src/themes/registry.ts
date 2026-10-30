import { programme } from './programme';
import type { Theme, ThemeId } from './slots';

/**
 * How each theme loads: Programme is in the entry chunk, every other theme
 * is a chunk of its own (a dynamic import).
 */
const loaders: Record<ThemeId, () => Promise<Theme>> = {
  programme: () => Promise.resolve(programme),
  // Werkstatt's slots and styles arrive with its own chunk (task 3.1 of the
  // themes plan: `import('./werkstatt')`). Until then it provides no slot,
  // so it looks like Programme under its own id.
  werkstatt: () => Promise.resolve({ id: 'werkstatt', slots: {} }),
};

/** Every theme's id. */
export const themeIds: readonly ThemeId[] = Object.keys(loaders) as ThemeId[];

/** Loads a theme (its chunk and styles). */
export function loadTheme(id: ThemeId): Promise<Theme> {
  return loaders[id]();
}
