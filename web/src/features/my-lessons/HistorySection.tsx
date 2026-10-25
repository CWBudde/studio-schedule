import { useInfiniteQuery } from '@tanstack/react-query';
import { History } from 'lucide-react';
import { useId } from 'react';
import { type TFunction } from 'i18next';
import { useTranslation } from 'react-i18next';
import { type MyLessonBooking, type MyLessonHistory, myLessonHistoryQuery } from '@/api/queries';
import { Button } from '@/components/ui/button';
import { LoadError } from '@/features/admin/crud/LoadError';
import { useServerClock } from '@/features/schedule/useServerClock';
import { formatDate, type SchoolDate, schoolDateOf, todayIn } from '@/lib/schoolTime';
import { MyLessonRow, type RowBadge } from './MyLessonRow';
import { Loading } from './Loading';
import { useNamesPersons } from './useNamesPersons';

/**
 * "History": the family's bookings of lessons that are over, newest first,
 * with what became of them. The API sends three calendar months at a time;
 * "Load earlier" asks for the three before, as long as older bookings
 * exist. A page without bookings says so, with the months it covers.
 */
export function HistorySection({ onOpen }: { onOpen: (lessonId: string) => void }) {
  const { t } = useTranslation('myLessons');
  const headingId = useId();
  const history = useInfiniteQuery(myLessonHistoryQuery);
  const named = useNamesPersons();
  const pages = history.data?.pages ?? [];
  const nothing =
    pages.length > 0 && !history.hasNextPage && pages.every((p) => p.items.length === 0);

  return (
    <section aria-labelledby={headingId} className="space-y-4">
      <h2 id={headingId} className="font-heading text-2xl font-medium tracking-tight">
        {t('history.title')}
      </h2>
      {history.isPending ? (
        <Loading />
      ) : history.isError && pages.length === 0 ? (
        <LoadError message={t('loadError')} onRetry={() => void history.refetch()} />
      ) : nothing ? (
        <p className="text-muted-foreground">{t('history.empty')}</p>
      ) : (
        <div className="space-y-2">
          {pages.map((page) => (
            <HistoryPage key={page.before} page={page} named={named} onOpen={onOpen} />
          ))}
          {history.isFetchNextPageError && (
            <LoadError
              message={t('history.loadError')}
              onRetry={() => void history.fetchNextPage()}
            />
          )}
          {history.hasNextPage && !history.isFetchNextPageError && (
            <Button
              variant="outline"
              className="w-full sm:w-auto"
              disabled={history.isFetchingNextPage}
              onClick={() => void history.fetchNextPage()}
            >
              <History aria-hidden="true" />
              {history.isFetchingNextPage ? t('history.loadingEarlier') : t('history.loadEarlier')}
            </Button>
          )}
        </div>
      )}
    </section>
  );
}

function HistoryPage({
  page,
  named,
  onOpen,
}: {
  page: MyLessonHistory;
  named: boolean;
  onOpen: (lessonId: string) => void;
}) {
  const { t, i18n } = useTranslation('myLessons');
  const lang = i18n.resolvedLanguage ?? 'en';
  const now = useServerClock();

  if (page.items.length === 0) {
    const date = (instant: string) =>
      formatDate(schoolDateOf(instant, page.timezone), lang, {
        day: 'numeric',
        month: 'long',
        year: 'numeric',
      });
    return (
      <p className="rounded-lg border border-dashed px-4 py-3 text-muted-foreground">
        {t('history.emptyPage', { from: date(page.nextBefore), to: date(page.before) })}
      </p>
    );
  }
  return (
    <ul className="space-y-2">
      {page.items.map((item) => (
        <HistoryRow
          key={item.booking.id}
          item={item}
          timeZone={page.timezone}
          today={todayIn(page.timezone, now)}
          named={named}
          onOpen={() => onOpen(item.lesson.id)}
        />
      ))}
    </ul>
  );
}

/**
 * What became of a past booking: attended, no-show, canceled (and whether
 * late) or not marked. A lesson the school canceled says so on its card,
 * and had no attendance to take.
 */
function historyBadges(
  { booking, lesson }: MyLessonBooking,
  t: TFunction<'myLessons'>,
): RowBadge[] {
  switch (booking.status) {
    case 'attended':
      return [{ label: t('status.attended'), variant: 'secondary' }];
    case 'no_show':
      return [{ label: t('status.noShow'), variant: 'destructive' }];
    case 'canceled':
      return [
        { label: t('status.canceled'), variant: 'outline' },
        ...(booking.late ? [{ label: t('status.late'), variant: 'destructive' } as const] : []),
      ];
    case 'booked':
      return lesson.status === 'canceled'
        ? []
        : [{ label: t('status.notMarked'), variant: 'outline' }];
  }
}

function HistoryRow({
  item,
  timeZone,
  today,
  named,
  onOpen,
}: {
  item: MyLessonBooking;
  timeZone: string;
  today: SchoolDate;
  named: boolean;
  onOpen: () => void;
}) {
  const { t } = useTranslation('myLessons');
  return (
    <MyLessonRow
      lesson={item.lesson}
      timeZone={timeZone}
      today={today}
      withDate
      past
      person={named ? item.personName : undefined}
      badges={historyBadges(item, t)}
      onOpen={onOpen}
    />
  );
}
