import type { Theme } from '../slots';
import { Home } from './Home';
import { Participants } from './Participants';
import { Profile } from './Profile';
import { Shell } from './Shell';
import './werkstatt.css';

/**
 * The look of a dance-school workshop (ADR 0018): condensed type, a plum
 * primary, a translucent page over the school's background. A chunk of its
 * own (registry.ts); the slots it leaves out are Programme's.
 */
export const werkstatt = {
  id: 'werkstatt',
  slots: { Shell, Home, Profile, Participants },
} as const satisfies Theme;
