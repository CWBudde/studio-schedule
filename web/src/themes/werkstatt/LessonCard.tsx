import { DoorClosed, DoorOpen, Info, ListPlus, Loader2, type LucideIcon, Star } from 'lucide-react';
import { type CSSProperties, useId } from 'react';
import { useTranslation } from 'react-i18next';
import { Badge } from '@/components/ui/badge';
import {
  AvailabilityBadges,
  hasAvailabilityBadges,
} from '@/features/schedule/booking/AvailabilityBadges';
import type { QuickAction } from '@/features/schedule/booking/quickAction';
import { useLessonCardLabel } from '@/features/schedule/list/cardParts';
import { VisibilityBadge } from '@/features/schedule/visibility/VisibilityBadge';
import { formatTimeRange } from '@/lib/schoolTime';
import { cn } from '@/lib/utils';
import type { LessonCardProps } from '../slots';
import { courseTint } from './courseTint';

const focusRing = 'outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50';

/**
 * Werkstatt's lesson card: the lesson as a button that opens its sheet, and
 * beside it a square button for the quick action (book, cancel, waiting
 * list; "Details" where the sheet must choose the person; "Full"
 * disabled). Tinted by course type (werkstatt.css); a booked lesson gets a
 * primary frame and a star; ended lessons are dimmed by colour (not by
 * opacity, which would break the text contrast), canceled ones checkered.
 * The card is a group named like its button, so its quick action can be
 * told apart from the other cards' ones.
 */
export function LessonCard({ lesson, timeZone, now, past, quickAction, onOpen }: LessonCardProps) {
  const { t, i18n } = useTranslation('schedule');
  const lang = i18n.resolvedLanguage ?? 'en';
  const detailsId = useId();
  const availabilityId = useId();
  const label = useLessonCardLabel()(lesson, timeZone);
  const canceled = lesson.status === 'canceled';
  const quiet = canceled || past;
  const booked = (lesson.bookedFor ?? []).length > 0;
  // freePlaces already leaves out the places that waiting-list invites hold:
  // those count as taken here, as they do for a booking.
  const taken = lesson.capacity - lesson.freePlaces;
  const full = lesson.freePlaces <= 0;
  // Whom of the family it is booked for, and who waits or holds an invite: the
  // star says only that it is booked, the count not whose place is held. The
  // free places are the participants line's, so the badges leave them out.
  const badges = hasAvailabilityBadges(lesson, false, now);

  return (
    <div
      role="group"
      aria-label={label}
      data-canceled={canceled || undefined}
      data-past={past || undefined}
      data-booked={booked || undefined}
      style={
        quiet ? undefined : ({ '--course-tint': courseTint(lesson.courseType.id) } as CSSProperties)
      }
      className={cn(
        'werkstatt-card relative flex w-full rounded-[4px] border text-sm text-card-foreground',
        booked && 'border-primary ring-1 ring-primary',
        quiet && 'text-muted-foreground',
      )}
    >
      {booked && (
        <Star
          role="img"
          aria-label={t('werkstatt.booked')}
          aria-hidden={false}
          className="absolute -top-2 -left-2 size-5 fill-(--werkstatt-star) stroke-primary"
        />
      )}
      <button
        type="button"
        aria-label={label}
        aria-describedby={badges ? `${detailsId} ${availabilityId}` : detailsId}
        onClick={onOpen}
        className={cn(
          focusRing,
          'flex min-w-0 flex-1 flex-col gap-1 rounded-[4px] p-3 text-left hover:bg-foreground/[0.03]',
        )}
      >
        <span className="flex items-baseline justify-between gap-2">
          <span
            className={cn(
              'font-bold tracking-wide md:uppercase',
              canceled && 'line-through decoration-1',
            )}
          >
            {lesson.courseType.name}
          </span>
          {lesson.level && (
            <span className="shrink-0 text-right text-xs text-muted-foreground">
              {t(`level.${lesson.level}`)}
            </span>
          )}
        </span>
        <span className="flex flex-wrap items-center gap-1.5">
          <span
            className={cn(
              'font-bold tabular-nums',
              !quiet && 'text-primary',
              canceled && 'line-through',
            )}
          >
            {formatTimeRange(lesson.startsAt, lesson.endsAt, timeZone, lang)}
          </span>
          {canceled && <Badge variant="destructive">{t('status.canceled')}</Badge>}
          {lesson.kind !== 'regular' && <Badge variant="outline">{t(`kind.${lesson.kind}`)}</Badge>}
          <VisibilityBadge visibility={lesson.visibility} />
        </span>
        {/* The API has no course description: the muted line names the place, which the room column shows only from 768px. */}
        <span id={detailsId} className="flex flex-col gap-0.5">
          <span className="text-muted-foreground">
            {lesson.room.name} · {lesson.location.name}
          </span>
          {!quiet && (
            <span className={full ? 'text-destructive' : 'text-(--werkstatt-green)'}>
              {t('werkstatt.participants', { taken, capacity: lesson.capacity })}
            </span>
          )}
          <span className={cn(!quiet && 'text-primary')}>
            {lesson.accompanist
              ? `${lesson.instructor.displayName} (${lesson.accompanist.displayName})`
              : lesson.instructor.displayName}
          </span>
        </span>
        <AvailabilityBadges id={availabilityId} lesson={lesson} showFree={false} now={now} />
      </button>
      <QuickButton action={quickAction} onOpen={onOpen} />
    </div>
  );
}

const square =
  'm-2 flex size-18 shrink-0 flex-col items-center justify-center gap-1 self-start rounded-[3px] px-1 text-[0.7rem] font-bold tracking-wide uppercase transition-[filter] hover:brightness-95 disabled:cursor-not-allowed motion-reduce:transition-none';

/** The faces of the quick actions that run something. */
const faces: Record<
  'book' | 'cancel' | 'waitlist' | 'open',
  { icon: LucideIcon; className: string }
> = {
  book: {
    icon: DoorOpen,
    className: 'bg-(--werkstatt-mint) text-(--werkstatt-mint-foreground)',
  },
  // White on --werkstatt-red reaches only 4.0:1: the face is the darker red.
  cancel: {
    icon: DoorClosed,
    className: 'bg-(--werkstatt-red-dark) text-(--werkstatt-red-foreground)',
  },
  waitlist: {
    icon: ListPlus,
    className: 'bg-(--werkstatt-brown) text-(--werkstatt-brown-foreground)',
  },
  open: {
    icon: Info,
    className: 'bg-(--werkstatt-mint) text-(--werkstatt-mint-foreground)',
  },
};

const labels = {
  book: 'werkstatt.book',
  cancel: 'werkstatt.cancel',
  waitlist: 'werkstatt.waitlist',
  open: 'werkstatt.details',
} as const;

/** The card's square button for its quick action; none for `none`. */
function QuickButton({ action, onOpen }: { action: QuickAction; onOpen(): void }) {
  const { t } = useTranslation('schedule');
  if (action.kind === 'none') return null;

  if (action.kind === 'full') {
    // Red and mint stripes (werkstatt.css); the label on a plain chip keeps its contrast.
    return (
      <button type="button" disabled className={cn(square, 'werkstatt-full')}>
        <span className="rounded-[2px] bg-background px-1.5 py-0.5 text-foreground">
          {t('werkstatt.full')}
        </span>
      </button>
    );
  }

  const { icon: Icon, className } = faces[action.kind];
  const pending = action.kind !== 'open' && action.pending;
  return (
    <button
      type="button"
      disabled={pending}
      aria-busy={pending || undefined}
      onClick={action.kind === 'open' ? onOpen : action.run}
      className={cn(focusRing, square, className)}
    >
      {pending ? (
        <Loader2 aria-hidden="true" className="size-6 motion-safe:animate-spin" />
      ) : (
        <Icon aria-hidden="true" className="size-6" />
      )}
      {t(labels[action.kind])}
    </button>
  );
}
