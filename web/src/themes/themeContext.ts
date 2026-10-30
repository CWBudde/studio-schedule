import { createContext } from 'react';
import type { Theme } from './slots';

/**
 * The active theme, provided by ThemeProvider. Undefined outside it (tests
 * of single pages), where every slot is Programme's. No default value: the
 * Programme module imports slots that import this, so it is read at render
 * time only (useSlot).
 */
export const ThemeContext = createContext<Theme | undefined>(undefined);
