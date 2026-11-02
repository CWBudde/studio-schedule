import { useQuery } from '@tanstack/react-query';
import { CircleAlert, RotateCcw, XIcon } from 'lucide-react';
import { type ReactNode, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { LessonDetail } from '@/api/client';
import { ApiError, lessonQuery, meQuery, publicSettingsQuery } from '@/api/queries';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet';
import { Skeleton } from '@/components/ui/skeleton';
import { canAdminister } from '@/features/admin/access';
import { formatAudience } from '@/lib/audience';
import { useOnline } from '@/lib/online';
import { formatDayHeading, formatTimeRange, formatTimestamp, schoolDateOf } from '@/lib/schoolTime';
import { LessonActions } from '../admin/LessonActions';
import { BookingSection } from '../booking/BookingSection';
import { ParticipantsSection } from '../participants/ParticipantsSection';
import { showsKind, useLessonLabel, useLessonTitle } from '../lessonLabel';
import { useScheduleSearch } from '../useScheduleSearch';
import { VisibilityBadge } from '../visibility/VisibilityBadge';

const isNotFound = (error: unknown) => error instanceof ApiError && error.status === 404;

/**
 * Details of the lesson in the `lesson` search param of /schedule, in a
 * sheet. The param makes the sheet a deep link; closing the sheet removes it.
 */
export function LessonSheet() {
  const { search, update } = useScheduleSearch();
  return (
    <LessonDetailSheet
      lessonId={search.lesson}
      onClose={() => void update({ lesson: undefined }, { replace: true })}
    />
  );
}

/**
 * Details of a lesson in a sheet, open while `lessonId` is set: the lesson,
 * booking, the participants for staff and the office's actions. `onClose`
 * is called when the sheet is closed and when the office deletes the lesson.
 */
export function LessonDetailSheet({
  lessonId,
  onClose,
}: {
  lessonId: string | undefined;
  onClose: () => void;
}) {
  const { t } = useTranslation('schedule');

  // Keep showing the last lesson while the sheet animates out.
  const [shownId, setShownId] = useState(lessonId);
  if (lessonId !== undefined && lessonId !== shownId) setShownId(lessonId);

  return (
    <Sheet
      open={lessonId !== undefined}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <SheetContent
        side="right"
        showCloseButton={false}
        className="w-full overflow-y-auto data-[side=right]:w-full data-[side=right]:sm:max-w-md"
      >
        {shownId !== undefined && <LessonSheetBody lessonId={shownId} onDeleted={onClose} />}
        <SheetClose asChild>
          <Button variant="ghost" size="icon-sm" className="absolute top-3 right-3">
            <XIcon aria-hidden="true" />
            <span className="sr-only">{t('detail.close')}</span>
          </Button>
        </SheetClose>
      </SheetContent>
    </Sheet>
  );
}

function LessonSheetBody({ lessonId, onDeleted }: { lessonId: string; onDeleted: () => void }) {
  const { t } = useTranslation('schedule');
  const settings = useQuery(publicSettingsQuery);
  const lesson = useQuery(lessonQuery(lessonId));
  const online = useOnline();

  if (isNotFound(lesson.error)) {
    return (
      <SheetHeader className="pr-12">
        <SheetTitle className="text-lg">{t('detail.notFound.title')}</SheetTitle>
        <SheetDescription>{t('detail.notFound.description')}</SheetDescription>
      </SheetHeader>
    );
  }

  if (lesson.isError || settings.isError) {
    return (
      <>
        <SheetHeader className="pr-12">
          <SheetTitle className="text-lg">{t('detail.title')}</SheetTitle>
          <SheetDescription className="sr-only">{t('detail.error')}</SheetDescription>
        </SheetHeader>
        <div className="px-4">
          <Alert variant="destructive">
            <CircleAlert aria-hidden="true" />
            <AlertTitle>{t('detail.error')}</AlertTitle>
            <AlertDescription>
              <Button
                variant="outline"
                size="sm"
                className="mt-2"
                onClick={() => {
                  if (lesson.isError) void lesson.refetch();
                  if (settings.isError) void settings.refetch();
                }}
              >
                <RotateCcw aria-hidden="true" />
                {t('detail.retry')}
              </Button>
            </AlertDescription>
          </Alert>
        </div>
      </>
    );
  }

  // Offline, a lesson opened before shows from the offline cache; any other
  // cannot load until the connection is back (ADR 0017).
  if (!online && (!lesson.data || !settings.data)) {
    return (
      <SheetHeader className="pr-12">
        <SheetTitle className="text-lg">{t('detail.title')}</SheetTitle>
        <SheetDescription>{t('detail.offline')}</SheetDescription>
      </SheetHeader>
    );
  }

  if (!lesson.data || !settings.data) {
    return (
      <>
        <SheetHeader className="pr-12">
          <SheetTitle className="sr-only">{t('detail.title')}</SheetTitle>
          <SheetDescription className="sr-only">{t('detail.loading')}</SheetDescription>
          <Skeleton className="h-6 w-40" />
          <Skeleton className="mt-1 h-4 w-56" />
        </SheetHeader>
        <div className="space-y-3 px-4" aria-hidden="true">
          <Skeleton className="h-5 w-24" />
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-3/4" />
          <Skeleton className="h-4 w-2/3" />
        </div>
      </>
    );
  }

  return (
    <LessonDetails lesson={lesson.data} timeZone={settings.data.timezone} onDeleted={onDeleted} />
  );
}

function LessonDetails({
  lesson,
  timeZone,
  onDeleted,
}: {
  lesson: LessonDetail;
  timeZone: string;
  onDeleted: () => void;
}) {
  const { t, i18n } = useTranslation('schedule');
  const { t: tAdmin } = useTranslation('admin');
  const lang = i18n.resolvedLanguage ?? 'en';
  const me = useQuery(meQuery);
  const label = useLessonLabel()(lesson, timeZone);
  const title = useLessonTitle();
  const canceled = lesson.status === 'canceled';
  const audience = formatAudience(lesson, t, 'long');
  const rental = lesson.kind === 'rental';
  // What the office sees of the cancellation (ADR 0007).
  const admin = lesson.admin;
  const canceledAt = admin?.canceledAt && formatTimestamp(admin.canceledAt, timeZone, lang);

  return (
    <>
      <SheetHeader className="gap-1 pr-12">
        <SheetTitle className={canceled ? 'text-lg line-through' : 'text-lg'}>
          {title(lesson)}
        </SheetTitle>
        <SheetDescription>
          <span>{formatDayHeading(schoolDateOf(lesson.startsAt, timeZone), lang)}</span>
          <span aria-hidden="true"> · </span>
          <span className="tabular-nums">
            {formatTimeRange(lesson.startsAt, lesson.endsAt, timeZone, lang)}
          </span>
        </SheetDescription>
        {(canceled || showsKind(lesson) || audience || lesson.visibility !== 'public') && (
          <div className="flex flex-wrap gap-1.5 pt-1">
            {canceled && <Badge variant="destructive">{t('status.canceled')}</Badge>}
            {showsKind(lesson) && <Badge variant="secondary">{t(`kind.${lesson.kind}`)}</Badge>}
            {audience && <Badge variant="outline">{audience}</Badge>}
            <VisibilityBadge visibility={lesson.visibility} />
          </div>
        )}
      </SheetHeader>

      <div className="space-y-6 px-4 pb-6">
        {canceled && (
          <Alert variant="destructive">
            <CircleAlert aria-hidden="true" />
            <AlertTitle>{t('detail.canceled')}</AlertTitle>
            {(canceledAt || admin?.cancelNote) && (
              <AlertDescription>
                {canceledAt && (
                  <p>
                    {admin?.canceledBy
                      ? tAdmin('lessons.canceledBy', {
                          name: admin.canceledBy.name,
                          at: canceledAt,
                        })
                      : tAdmin('lessons.canceledAt', { at: canceledAt })}
                  </p>
                )}
                {admin?.cancelNote && (
                  <p className="break-words">
                    {tAdmin('lessons.note', { note: admin.cancelNote })}
                  </p>
                )}
              </AlertDescription>
            )}
          </Alert>
        )}

        <dl className="grid grid-cols-[auto_1fr] gap-x-6 gap-y-3">
          <Row label={t('detail.location')}>
            <span className="block">{lesson.location.name}</span>
            {lesson.locationAddress && (
              <span className="block text-muted-foreground">{lesson.locationAddress}</span>
            )}
          </Row>
          <Row label={t('detail.room')}>{lesson.room.name}</Row>
          {lesson.instructor && (
            <Row label={t('detail.instructor')}>{lesson.instructor.displayName}</Row>
          )}
          {lesson.accompanist && (
            <Row label={t('detail.accompanist')}>{lesson.accompanist.displayName}</Row>
          )}
          {rental ? (
            <Row label={t('detail.renter')}>{lesson.renter}</Row>
          ) : (
            <Row label={t('detail.capacity')}>{t('detail.places', { count: lesson.capacity })}</Row>
          )}
        </dl>

        {/* A room rental takes no bookings and has no participants (ADR 0007, amendment). */}
        {rental ? (
          <p className="text-muted-foreground">{t('detail.rental')}</p>
        ) : (
          <>
            <BookingSection lesson={lesson} timeZone={timeZone} label={label} />
            <ParticipantsSection lesson={lesson} timeZone={timeZone} label={label} />
          </>
        )}

        {canAdminister(me.data?.role) && (
          <LessonActions lesson={lesson} timeZone={timeZone} onDeleted={onDeleted} />
        )}

        {lesson.courseTypeDescription && (
          <section aria-labelledby="lesson-about" className="space-y-1">
            <h3 id="lesson-about" className="font-medium">
              {t('detail.about')}
            </h3>
            <p className="text-muted-foreground">{lesson.courseTypeDescription}</p>
          </section>
        )}
      </div>
    </>
  );
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 font-medium break-words">{children}</dd>
    </>
  );
}
