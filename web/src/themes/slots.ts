import type { LinkProps } from '@tanstack/react-router';
import type { LucideIcon } from 'lucide-react';
import type { ComponentType, ReactNode } from 'react';
import type { Me, ScheduleLesson } from '@/api/client';
import type {
  AttendanceStatus,
  LessonParticipant,
  LessonParticipants,
  LessonWaitlistEntry,
} from '@/api/queries';
import type { components } from '@/api/schema';
import type { QuickAction } from '@/features/schedule/booking/quickAction';
import type { DayTone } from '@/features/schedule/list/days';
import type { SchoolDate } from '@/lib/schoolTime';

/**
 * The layout slots of a theme (ADR 0018). Each slot renders a view model and
 * calls back; its page or container computes both. A slot never calls
 * useQuery, useMutation or the API; it may use useTranslation, router links,
 * shadcn components, pure helpers and other slots (useSlot).
 */

/** A theme's id: the school's `theme` setting. */
export type ThemeId = components['schemas']['PublicSettings']['theme'];

// --- Shell -----------------------------------------------------------------

/** An entry of the main navigation. */
export interface NavItem {
  to: NonNullable<LinkProps['to']>;
  label: string;
  icon: LucideIcon;
}

/**
 * The visitor's session, for a theme with a sign-in/sign-out button of its
 * own: unknown until the account is known (so neither flashes).
 */
export type ShellSession =
  | { state: 'unknown' }
  | { state: 'signedOut' }
  | {
      state: 'signedIn';
      /** Signs out, with the account menu's notices. */
      signOut(): void;
      /** Whether a sign-out runs. */
      signingOut: boolean;
    };

/** The app around every page that is not bare. */
export interface ShellProps {
  /** The page: the backend gate around the route's outlet. */
  children: ReactNode;
  /** The banners above the header (demo, offline, impersonation). */
  banners: ReactNode;
  /** The announcements' banner, below the header. */
  announcements: ReactNode;
  header: {
    /** The school's name, once the public settings are there. */
    schoolName?: string;
    /** The school's logo as a data URL, while it has one. */
    logoUrl?: string;
    nav: NavItem[];
    /** The demo's mailbox and reset (each renders nothing outside the demo). */
    tools: ReactNode;
    /** "Sign in", or the signed-in person's menu. */
    account: ReactNode;
    language: ReactNode;
    session: ShellSession;
  };
  footer: ReactNode;
}

// --- Schedule --------------------------------------------------------------

/** The lessons of one school date, as the schedule shows them. */
export interface ScheduleDay {
  date: SchoolDate;
  tone: DayTone;
  /** The id of the day's heading, unique on the page. */
  headingId: string;
  lessons: ScheduleLesson[];
}

export interface ScheduleListProps {
  /** The days with lessons, in order; empty when there are none. */
  days: ScheduleDay[];
  timeZone: string;
  /** The server's clock, in ms. */
  now: number;
  /** What to show without lessons. */
  empty?: ReactNode;
  onOpen(lessonId: string): void;
  /** A lesson's card: the LessonCard slot, connected to its quick action. */
  renderLesson(lesson: ScheduleLesson): ReactNode;
}

export interface DayHeadingProps {
  id: string;
  date: SchoolDate;
  tone: DayTone;
  /** The heading id of the day before and after, where the list has one. */
  prevId?: string;
  nextId?: string;
}

export interface LessonCardProps {
  lesson: ScheduleLesson;
  timeZone: string;
  /** The server's clock, in ms. */
  now: number;
  /** Whether the lesson has ended. */
  past: boolean;
  quickAction: QuickAction;
  /** Opens the lesson's sheet. */
  onOpen(): void;
}

// --- Home ------------------------------------------------------------------

/** The backend's and the school's state, as the home page's status card shows it. */
export interface HomeStatus {
  /** Pending while the health check runs; ok only when the backend says so. */
  health: 'pending' | 'ok' | 'error';
  /** The app's version: only the office and admins learn it (ASVS 14.3.3). */
  version?: string;
  /** The school's time zone, once the public settings are there. */
  timezone?: string;
  /** Whether the public settings are still loading. */
  settingsPending: boolean;
  /** Whether the health check or the public settings failed. */
  failed: boolean;
  /** Asks for the health and the public settings again. */
  onRetry(): void;
}

/**
 * The signed-in visitor's own lessons of the coming week: those they or a
 * dependent are booked for and that have not ended (features/home/useMyLessons.tsx).
 */
export type HomeMyLessons =
  /** Whether the visitor is signed in is not known yet. */
  | { state: 'unknown' }
  | { state: 'signedOut' }
  /** The school's time zone or the schedule is still loading. */
  | { state: 'loading' }
  | { state: 'error'; onRetry(): void }
  | {
      state: 'ready';
      /** The days with such a lesson, in order; empty when there is none. */
      days: ScheduleDay[];
      /** A lesson's card: the LessonCard slot, connected to its quick action; opens it in the schedule. */
      renderLesson(lesson: ScheduleLesson): ReactNode;
    };

/** The home page (features/home/HomePage.tsx). */
export interface HomeProps {
  /** The school's name, once the public settings are there. */
  schoolName?: string;
  /** The announcements for the visitor. */
  announcements: ReactNode;
  /** The teaser of the next lessons, with the theme's layout classes. */
  renderNextLessons(className?: string): ReactNode;
  status: HomeStatus;
  myLessons: HomeMyLessons;
}

// --- Profile ---------------------------------------------------------------

/**
 * The account page's sections, each a connected card. The credential ones
 * (and push) are absent while an admin is signed in as the account.
 */
export interface ProfileSections {
  profile: ReactNode;
  dependents: ReactNode;
  calendarFeeds: ReactNode;
  push?: ReactNode;
  myData: ReactNode;
  password?: ReactNode;
  email?: ReactNode;
  deleteAccount?: ReactNode;
}

/** The account page (features/account/AccountPage.tsx). */
export type ProfileProps =
  | { state: 'loading' }
  | { state: 'error'; error: unknown }
  | { state: 'signedOut' }
  | {
      state: 'ready';
      me: Me;
      sections: ProfileSections;
      /** The account's name while an admin is signed in as it. */
      impersonating?: { name: string };
    };

// --- Participants ----------------------------------------------------------

/** A dialog about one participant. */
export type ParticipantDialog = 'trial' | 'remove' | 'history';

/** The participants' first load failed. */
export interface ParticipantsErrorProps {
  state: 'error';
  /** The id of the section's heading. */
  headingId: string;
  onRetry(): void;
}

/** A lesson's participants for staff, and what the caller may do with them now. */
export interface ParticipantsReadyProps {
  state: 'ready';
  /** The id of the section's heading. */
  headingId: string;
  counts: Pick<LessonParticipants, 'active' | 'capacity' | 'freePlaces' | 'held' | 'waiting'>;
  /** Whether the last refetch failed, so the lists may be out of date. */
  stale: boolean;
  onRetry(): void;
  timeZone: string;
  /** Whether the caller may change participants: the office always, others until the day is over. */
  canEdit: boolean;
  /** Whether the caller may add a participant (not to a canceled lesson). */
  canAdd: boolean;
  /** Whether the caller may take attendance (from the start, while editable, not canceled). */
  canAttend: boolean;
  /** Whether the caller is the office, which sees a booking's history. */
  office: boolean;
  /** The bookings that are not canceled, in the API's order. */
  active: LessonParticipant[];
  /** The canceled bookings (with `canceledAt`). */
  canceled: LessonParticipant[];
  waitlist: LessonWaitlistEntry[];
  /** Opens the dialog that adds a participant. */
  onAdd(): void;
  /** "All attended": whether it applies (attendance is open for someone), whether it runs, and running it. */
  allAttended: { show: boolean; pending: boolean; run(): void };
  /** Records a participant's attendance; `booked` resets it. */
  mark(participant: LessonParticipant, status: AttendanceStatus): void;
  /** Whether an attendance of the booking is being recorded (other rows stay usable). */
  marking(bookingId: string): boolean;
  /** Opens a dialog about a participant. */
  open(participant: LessonParticipant, dialog: ParticipantDialog): void;
  /** The connected dialogs (add, trial, remove, history): render them once, inside the section. */
  dialogs: ReactNode;
}

/** The view model of a lesson's participants (features/schedule/participants/ParticipantsSection.tsx). */
export type ParticipantsProps = ParticipantsErrorProps | ParticipantsReadyProps;

// --- Theme -----------------------------------------------------------------

export interface Slots {
  Shell: ComponentType<ShellProps>;
  ScheduleList: ComponentType<ScheduleListProps>;
  DayHeading: ComponentType<DayHeadingProps>;
  LessonCard: ComponentType<LessonCardProps>;
  Home: ComponentType<HomeProps>;
  Profile: ComponentType<ProfileProps>;
  Participants: ComponentType<ParticipantsProps>;
}

/** A theme: its id and the slots it provides; the others are Programme's. */
export interface Theme {
  id: ThemeId;
  slots: Partial<Slots>;
}
