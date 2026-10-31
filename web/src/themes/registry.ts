import { programme } from './programme';
import type { Theme, ThemeId } from './slots';

/**
 * How each theme loads: Programme is in the entry chunk, every other theme
 * is a chunk of its own (a dynamic import).
 */
const loaders: Record<ThemeId, () => Promise<Theme>> = {
  programme: () => Promise.resolve(programme),
  werkstatt: () => import('./werkstatt').then((m) => m.werkstatt),
};

/** Every theme's id. */
export const themeIds: readonly ThemeId[] = Object.keys(loaders) as ThemeId[];

/** Loads a theme (its chunk and styles). */
export function loadTheme(id: ThemeId): Promise<Theme> {
  return loaders[id]();
}
