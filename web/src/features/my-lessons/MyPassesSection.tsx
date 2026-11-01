import { useQuery } from '@tanstack/react-query';
import { useId } from 'react';
import { useTranslation } from 'react-i18next';
import { type Pass, type PassUse, myPassesQuery } from '@/api/queries';
import { Badge } from '@/components/ui/badge';
import { LoadError } from '@/features/admin/crud/LoadError';
import { PassStateBadge } from '@/features/passes/PassBits';
import { usePassText } from '@/features/passes/passText';
import { passState } from '@/features/passes/passes';
import { useLessonTitle } from '@/features/schedule/lessonLabel';
import { shownName } from '@/lib/personName';
import {
  formatDate,
  formatTime,
  type SchoolDate,
  schoolDateOf,
  yearUnlessIn,
} from '@/lib/schoolTime';
import { Loading } from './Loading';
import { useNamesPersons } from './useNamesPersons';

/**
 * "My passes": the passes of the signed-in person and their dependents,
 * usable ones first: whose it is, its visits and days, its state (usable,
 * used up, expired, voided, not valid yet), who paid, and the latest
 * lessons it paid for with how many in all. Left out for a family without
 * a pass, which pays at the desk.
 */
export function MyPassesSection() {
  const { t } = useTranslation('passes');
  const headingId = useId();
  const passes = useQuery(myPassesQuery);
  const named = useNamesPersons();

  if (passes.data && passes.data.items.length === 0) return null;
  return (
    <section aria-labelledby={headingId} className="space-y-4">
      <h2 id={headingId} className="font-heading text-2xl font-medium tracking-tight">
        {t('mine.title')}
      </h2>
      {passes.data ? (
        <ul className="grid gap-3 sm:grid-cols-2">
          {passes.data.items.map((pass) => (
            <PassCard
              key={pass.id}
              pass={pass}
              today={passes.data.today}
              timeZone={passes.data.timezone}
              named={named}
            />
          ))}
        </ul>
      ) : passes.isError ? (
        <LoadError message={t('mine.loadError')} onRetry={() => void passes.refetch()} />
      ) : (
        <Loading />
      )}
    </section>
  );
}

function PassCard({
  pass,
  today,
  timeZone,
  named,
}: {
  pass: Pass;
  today: SchoolDate;
  timeZone: string;
  named: boolean;
}) {
  const { t } = useTranslation('passes');
  const text = usePassText();
  const titleId = useId();
  const state = passState(pass, today);
  const payer = pass.payer && pass.payer.id !== pass.owner.id ? pass.payer : undefined;
  return (
    <li
      aria-label={
        named
          ? t('mine.cardLabel', { pass: pass.passType.name, name: shownName(pass.owner) })
          : undefined
      }
      aria-labelledby={named ? undefined : titleId}
      data-state={state}
      className="space-y-3 rounded-xl p-4 text-sm ring-1 ring-foreground/10 data-[state=usable]:ring-primary/40"
    >
      <div className="flex flex-wrap items-start justify-between gap-2">
        <h3 id={titleId} className="min-w-0 font-heading text-base font-medium break-words">
          {pass.passType.name}
          {named && (
            <span className="block text-sm font-normal text-muted-foreground">
              {shownName(pass.owner)}
            </span>
          )}
        </h3>
        <PassStateBadge state={state} />
      </div>
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
        <dt className="text-muted-foreground">{t('mine.visits')}</dt>
        <dd className="tabular-nums">{text.visits(pass)}</dd>
        <dt className="text-muted-foreground">{t('mine.valid')}</dt>
        <dd>{text.days(pass, today)}</dd>
        {(payer || pass.origin === 'assignment') && (
          <>
            <dt className="text-muted-foreground">{t('mine.payment')}</dt>
            <dd>
              {pass.origin === 'assignment'
                ? t('mine.given')
                : t('mine.paidBy', { name: payer ? shownName(payer) : '' })}
            </dd>
          </>
        )}
      </dl>
      <Uses pass={pass} today={today} timeZone={timeZone} />
    </li>
  );
}

/** The latest lessons the pass paid for, and how many in all. */
function Uses({ pass, today, timeZone }: { pass: Pass; today: SchoolDate; timeZone: string }) {
  const { t } = useTranslation('passes');
  if (pass.useCount === 0) return <p className="text-muted-foreground">{t('mine.noUses')}</p>;
  return (
    <div className="space-y-1.5">
      <h4 className="font-medium">{t('mine.latestUses')}</h4>
      <ul aria-label={t('mine.usesLabel', { pass: pass.passType.name })} className="space-y-1">
        {pass.uses.map((use) => (
          <UseRow key={use.bookingId} use={use} today={today} timeZone={timeZone} />
        ))}
      </ul>
      <p className="text-muted-foreground">{t('mine.useCount', { count: pass.useCount })}</p>
    </div>
  );
}

function UseRow({ use, today, timeZone }: { use: PassUse; today: SchoolDate; timeZone: string }) {
  const { t, i18n } = useTranslation('passes');
  const lang = i18n.resolvedLanguage ?? 'en';
  const title = useLessonTitle();
  const date = schoolDateOf(use.lesson.startsAt, timeZone);
  const when = `${formatDate(date, lang, {
    weekday: 'short',
    day: 'numeric',
    month: 'short',
    ...yearUnlessIn(date, today),
  })}, ${formatTime(use.lesson.startsAt, timeZone, lang)}`;
  const status =
    use.bookingStatus === 'no_show'
      ? t('mine.useStatus.noShow')
      : use.bookingStatus === 'canceled' && use.late
        ? t('mine.useStatus.late')
        : undefined;
  return (
    <li className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
      <span className="font-medium">{title(use.lesson)}</span>
      <span className="text-muted-foreground tabular-nums">{when}</span>
      <span className="text-muted-foreground">· {use.personName}</span>
      {status && <Badge variant="outline">{status}</Badge>}
    </li>
  );
}
