import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { ChevronLeft, ChevronRight } from 'lucide-react';
import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { allStaffQuery, type TeachParams, teachLessonsQuery } from '@/api/queries';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Skeleton } from '@/components/ui/skeleton';
import { canAdminister } from '@/features/admin/access';
import { LoadError } from '@/features/admin/crud/LoadError';
import { RoleGate } from '@/features/admin/RoleGate';
import { SubscribeButton } from '@/features/calendar-feeds/SubscribeDialog';
import { LessonDetailSheet } from '@/features/schedule/detail/LessonSheet';
import { addDays, formatDate } from '@/lib/schoolTime';
import { NotifyDialog, type NotifyTarget } from './NotifyDialog';
import { TeachList } from './TeachList';
import { canTeach } from './access';
import { useTeachSearch } from './useTeachSearch';

/** Days the page shows at once: the week before today, today and the week after. */
export const teachDays = 15;
/** Days the range moves by: two weeks, so the last day stays on the page. */
export const teachStep = 14;

/**
 * The instructor view (/teach): the lessons a staff member teaches, from a
 * week back to a week ahead, by school day, with their counts and where
 * attendance stands; a lesson opens in the lesson sheet (participants,
 * attendance), and its participants can be sent a message; "Subscribe" puts
 * the own teaching in a calendar app. The office sees
 * every lesson and can narrow them to one staff member. Members and
 * visitors see that the page is not for them; the API refuses them as well.
 */
export function TeachPage() {
  const { t } = useTranslation('teach');
  return (
    <RoleGate allow={canTeach} noAccess={t('noAccess')}>
      {(me) => <Teach office={canAdminister(me.role)} teaches={me.teaches} />}
    </RoleGate>
  );
}

/** The page for staff; `office` for the office and admins, who see every lesson. */
function Teach({ office, teaches }: { office: boolean; teaches: boolean }) {
  const { t } = useTranslation('teach');
  const { search, update } = useTeachSearch();
  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h1 className="font-heading text-4xl font-medium tracking-tight">{t('title')}</h1>
          {/* A teaching feed shows the lessons of the own staff records: none, no feed. */}
          {teaches && <SubscribeButton subject={{ scope: 'teaching' }} />}
        </div>
        <p className="max-w-prose text-muted-foreground">
          {office ? t('introOffice') : t('intro')}
        </p>
      </div>
      <TeachView office={office} />
      <LessonDetailSheet
        lessonId={search.lesson}
        onClose={() => void update({ lesson: undefined }, { replace: true })}
      />
    </div>
  );
}

function TeachView({ office }: { office: boolean }) {
  const { t, i18n } = useTranslation('teach');
  const lang = i18n.resolvedLanguage ?? 'en';
  const { search, update } = useTeachSearch();
  const [notify, setNotify] = useState<{ target: NotifyTarget | null; open: boolean; seq: number }>(
    { target: null, open: false, seq: 0 },
  );

  const params: TeachParams = {
    ...(search.from ? { from: search.from, to: addDays(search.from, teachDays) } : {}),
    ...(office && search.staff ? { staffId: search.staff } : {}),
  };
  const lessons = useQuery({ ...teachLessonsQuery(params), placeholderData: keepPreviousData });

  // Without `from` the server picks the range (its today); the answer says which.
  const start = search.from ?? lessons.data?.from;
  const end = search.from ? addDays(search.from, teachDays) : lessons.data?.to;
  const shortDate = (date: string) => formatDate(date, lang, { day: 'numeric', month: 'short' });

  return (
    <>
      <div className="flex flex-wrap items-end justify-between gap-3">
        <nav aria-label={t('range.label')} className="flex items-center gap-1">
          <Button
            variant="outline"
            size="icon"
            aria-label={t('range.previous')}
            disabled={start === undefined}
            onClick={() => start && void update({ from: addDays(start, -teachStep) })}
          >
            <ChevronLeft aria-hidden="true" />
          </Button>
          <span className="min-w-36 text-center text-sm font-medium tabular-nums">
            {start && end
              ? t('range.shown', { from: shortDate(start), to: shortDate(addDays(end, -1)) })
              : ' '}
          </span>
          <Button
            variant="outline"
            size="icon"
            aria-label={t('range.next')}
            disabled={start === undefined}
            onClick={() => start && void update({ from: addDays(start, teachStep) })}
          >
            <ChevronRight aria-hidden="true" />
          </Button>
          <Button
            variant="ghost"
            size="sm"
            disabled={search.from === undefined}
            onClick={() => void update({ from: undefined })}
          >
            {t('range.today')}
          </Button>
        </nav>
        {office && (
          <StaffFilter value={search.staff} onChange={(staff) => void update({ staff })} />
        )}
      </div>

      {lessons.data ? (
        <TeachList
          data={lessons.data}
          office={office}
          stale={lessons.isPlaceholderData}
          onOpen={(lesson) => void update({ lesson })}
          onNotify={(target) => setNotify((s) => ({ target, open: true, seq: s.seq + 1 }))}
        />
      ) : lessons.isError ? (
        <LoadError message={t('loadError')} onRetry={() => void lessons.refetch()} />
      ) : (
        <div role="status" className="space-y-3">
          <span className="sr-only">{t('loading')}</span>
          <Skeleton className="h-10 w-full" aria-hidden="true" />
          <Skeleton className="h-24 w-full" aria-hidden="true" />
          <Skeleton className="h-24 w-full" aria-hidden="true" />
        </div>
      )}

      <NotifyDialog
        key={notify.seq}
        open={notify.open}
        target={notify.target}
        onClose={() => setNotify((s) => ({ ...s, open: false }))}
      />
    </>
  );
}

const allStaff = 'all';

/** The office's choice of whose lessons to see: everyone's or one staff member's. */
function StaffFilter({
  value,
  onChange,
}: {
  value: string | undefined;
  onChange: (staff: string | undefined) => void;
}) {
  const { t } = useTranslation('teach');
  const staff = useQuery(allStaffQuery);
  const id = useId();

  return (
    <div className="grid w-full gap-1.5 sm:w-64">
      <Label htmlFor={id}>{t('staff.label')}</Label>
      <Select
        value={value ?? allStaff}
        onValueChange={(next) => onChange(next === allStaff ? undefined : next)}
      >
        <SelectTrigger id={id} className="w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={allStaff}>{t('staff.all')}</SelectItem>
          {(staff.data ?? []).map((s) => (
            <SelectItem key={s.id} value={s.id}>
              {s.active ? s.displayName : t('staff.inactive', { name: s.displayName })}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
