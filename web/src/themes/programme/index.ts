import type { Slots, Theme } from '../slots';
import { DayHeading } from './DayHeading';
import { EventDetail } from './EventDetail';
import { EventList } from './EventList';
import { Home } from './Home';
import { LessonCard } from './LessonCard';
import { Participants } from './Participants';
import { Profile } from './Profile';
import { ScheduleList } from './ScheduleList';
import { Shell } from './Shell';

/** Today's look, in the entry chunk: it provides every slot, the fallback of all other themes. */
export const programme = {
  id: 'programme',
  slots: {
    Shell,
    ScheduleList,
    DayHeading,
    LessonCard,
    Home,
    Profile,
    Participants,
    EventList,
    EventDetail,
  },
} as const satisfies Theme & { slots: Slots };
