import type { Theme } from '../slots';
import { DayHeading } from './DayHeading';
import { LessonCard } from './LessonCard';
import { ScheduleList } from './ScheduleList';
import { Shell } from './Shell';
import './werkstatt.css';

/**
 * The look of a dance-school workshop (ADR 0018): condensed type, a plum
 * primary, a translucent page over the school's background. A chunk of its
 * own (registry.ts); the slots it leaves out are Programme's.
 */
export const werkstatt = {
  id: 'werkstatt',
  slots: { Shell, ScheduleList, DayHeading, LessonCard },
} as const satisfies Theme;
