import { useQuery } from '@tanstack/react-query';
import { CircleAlert, RotateCcw, XIcon } from 'lucide-react';
import { type ReactNode, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { LessonDetail } from '@/api/client';
import { ApiError, lessonQuery, publicSettingsQuery } from '@/api/queries';
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
import { formatDayHeading, formatTimeRange, schoolDateOf } from '@/lib/schoolTime';
import { useScheduleSearch } from '../useScheduleSearch';

const isNotFound = (error: unknown) => error instanceof ApiError && error.status === 404;

/**
 * Details of the lesson in the `lesson` search param, in a sheet. The param
 * makes the sheet a deep link; closing the sheet removes it.
 */
export function LessonSheet() {
  const { t } = useTranslation('schedule');
  const { search, update } = useScheduleSearch();
  const lessonId = search.lesson;

  // Keep showing the last lesson while the sheet animates out.
  const [shownId, setShownId] = useState(lessonId);
  if (lessonId !== undefined && lessonId !== shownId) setShownId(lessonId);

  return (
    <Sheet
      open={lessonId !== undefined}
      onOpenChange={(open) => {
        if (!open) void update({ lesson: undefined }, { replace: true });
      }}
    >
      <SheetContent
        side="right"
        showCloseButton={false}
        className="w-full overflow-y-auto data-[side=right]:w-full data-[side=right]:sm:max-w-md"
      >
        {shownId !== undefined && <LessonSheetBody lessonId={shownId} />}
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

function LessonSheetBody({ lessonId }: { lessonId: string }) {
  const { t } = useTranslation('schedule');
  const settings = useQuery(publicSettingsQuery);
  const lesson = useQuery(lessonQuery(lessonId));

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

  return <LessonDetails lesson={lesson.data} timeZone={settings.data.timezone} />;
}

function LessonDetails({ lesson, timeZone }: { lesson: LessonDetail; timeZone: string }) {
  const { t, i18n } = useTranslation('schedule');
  const lang = i18n.resolvedLanguage ?? 'en';
  const canceled = lesson.status === 'canceled';

  return (
    <>
      <SheetHeader className="gap-1 pr-12">
        <SheetTitle className={canceled ? 'text-lg line-through' : 'text-lg'}>
          {lesson.courseType.name}
        </SheetTitle>
        <SheetDescription>
          <span>{formatDayHeading(schoolDateOf(lesson.startsAt, timeZone), lang)}</span>
          <span aria-hidden="true"> · </span>
          <span className="tabular-nums">
            {formatTimeRange(lesson.startsAt, lesson.endsAt, timeZone, lang)}
          </span>
        </SheetDescription>
        {(canceled || lesson.kind !== 'regular' || lesson.level) && (
          <div className="flex flex-wrap gap-1.5 pt-1">
            {canceled && <Badge variant="destructive">{t('status.canceled')}</Badge>}
            {lesson.kind !== 'regular' && (
              <Badge variant="secondary">{t(`kind.${lesson.kind}`)}</Badge>
            )}
            {lesson.level && <Badge variant="outline">{t(`level.${lesson.level}`)}</Badge>}
          </div>
        )}
      </SheetHeader>

      <div className="space-y-6 px-4 pb-6">
        {canceled && (
          <Alert variant="destructive">
            <CircleAlert aria-hidden="true" />
            <AlertTitle>{t('detail.canceled')}</AlertTitle>
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
          <Row label={t('detail.instructor')}>{lesson.instructor.displayName}</Row>
          {lesson.accompanist && (
            <Row label={t('detail.accompanist')}>{lesson.accompanist.displayName}</Row>
          )}
          <Row label={t('detail.capacity')}>{t('detail.places', { count: lesson.capacity })}</Row>
        </dl>

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
