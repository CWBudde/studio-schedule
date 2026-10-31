import { useContext } from 'react';
import { programme } from './programme';
import type { Slots, Theme } from './slots';
import { ThemeContext } from './themeContext';

/** The active theme's component for a slot; Programme's where the theme has none. */
export function useSlot<K extends keyof Slots>(name: K): Slots[K] {
  const theme = useContext(ThemeContext);
  return slotOf(theme, name);
}

/** A theme's component for a slot, or Programme's. */
export function slotOf<K extends keyof Slots>(theme: Theme | undefined, name: K): Slots[K] {
  return theme?.slots[name] ?? programme.slots[name];
}
