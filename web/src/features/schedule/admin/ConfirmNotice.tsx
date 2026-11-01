import { TriangleAlert } from 'lucide-react';
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import type { ConflictLesson } from '@/api/conflict';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { formatDayHeading, formatTimeRange, schoolDateOf } from '@/lib/schoolTime';
import { useLessonTitle } from '../lessonLabel';

/**
 * A write the API refused until it is confirmed (a 409 `room-taken` or
 * `past`): what is the matter, and a button that sends it again confirmed.
 */
export function ConfirmNotice({
  title,
  children,
  action,
  pending,
  onConfirm,
}: {
  title: string;
  children: ReactNode;
  action: string;
  pending: boolean;
  onConfirm: () => void;
}) {
  return (
    <Alert>
      <TriangleAlert aria-hidden="true" />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>
        <p>{children}</p>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="mt-2"
          disabled={pending}
          onClick={onConfirm}
        >
          {action}
        </Button>
      </AlertDescription>
    </Alert>
  );
}

/** A 409 `room-taken`: the lesson that has `room` then, in the school's time zone. */
export function RoomTakenNotice({
  room,
  lesson,
  timeZone,
  action,
  pending,
  onConfirm,
}: {
  room: string;
  lesson: ConflictLesson;
  timeZone: string;
  action: string;
  pending: boolean;
  onConfirm: () => void;
}) {
  const { t, i18n } = useTranslation('admin');
  const lang = i18n.resolvedLanguage ?? 'en';
  const title = useLessonTitle();
  return (
    <ConfirmNotice
      title={t('lessons.roomTaken', { room })}
      action={action}
      pending={pending}
      onConfirm={onConfirm}
    >
      {t('lessons.roomTakenBy', {
        lesson: t('lessons.label', {
          courseType: title(lesson),
          day: formatDayHeading(schoolDateOf(lesson.startsAt, timeZone), lang),
          time: formatTimeRange(lesson.startsAt, lesson.endsAt, timeZone, lang),
        }),
      })}
    </ConfirmNotice>
  );
}
