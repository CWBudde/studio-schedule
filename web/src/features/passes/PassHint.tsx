import { useQuery } from '@tanstack/react-query';
import { Ticket } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { type MyPasses, myLessonsQuery } from '@/api/queries';
import { type SchoolDate, schoolDateOf } from '@/lib/schoolTime';
import { pickPass } from './passes';

/**
 * What a booking of `personId` for the lesson `lessonId` on `date` would
 * cost: the pass that would pay for it (and the visits it has left after
 * this booking, as the payment badges count them), when
 * (on booking or on attendance, as the school uses passes), or that no pass
 * covers the day, so it is paid at the desk. On attendance, the person's
 * other bookings whose attendance is not taken yet may use those visits
 * first: when they would use them all, the hint says so. It never blocks
 * booking.
 */
export function PassHint({
  id,
  passes,
  personId,
  lessonId,
  date,
}: {
  id: string;
  passes: MyPasses;
  personId: string;
  lessonId: string;
  date: SchoolDate;
}) {
  const { t } = useTranslation('passes');
  const pass = pickPass(passes.items, personId, date);
  const visitPass = pass?.kind === 'visits';
  const mode = passes.redemptionMode;
  // Only on attendance do bookings wait to use a visit.
  const lessons = useQuery({ ...myLessonsQuery, enabled: visitPass && mode === 'attendance' });

  // The person's coming bookings that would use the same pass once attended.
  const planned =
    visitPass && mode === 'attendance'
      ? (lessons.data?.upcoming ?? []).filter(
          (item) =>
            item.booking.personId === personId &&
            item.lesson.id !== lessonId &&
            item.payment.status === 'pending' &&
            pickPass(passes.items, personId, schoolDateOf(item.lesson.startsAt, passes.timezone))
              ?.id === pass.id,
        ).length
      : 0;

  const text = !pass
    ? t('hint.none')
    : pass.kind === 'time'
      ? t(`hint.time.${mode}`, { pass: pass.passType.name })
      : planned > 0 && planned >= pass.visitsRemaining
        ? t('hint.visits.planned', {
            pass: pass.passType.name,
            left: pass.visitsRemaining,
            count: planned,
          })
        : // The visits left once this booking (and those planned before it) used theirs.
          t(`hint.visits.${mode}`, {
            pass: pass.passType.name,
            count: pass.visitsRemaining - planned - 1,
          });
  return (
    <p id={id} className="flex w-full items-start gap-1.5 text-sm text-muted-foreground">
      <Ticket className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
      {text}
    </p>
  );
}
