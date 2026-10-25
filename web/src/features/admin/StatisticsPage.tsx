import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { useNavigate, useSearch } from '@tanstack/react-router';
import { type FormEvent, type ReactNode, useId, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ApiError, type Statistics, type StatisticsParams, statisticsQuery } from '@/api/queries';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import { formatCompactMoney, formatMoney } from '@/lib/money';
import { addDays, formatDate, formatDateTime } from '@/lib/schoolTime';
import { LoadError } from './crud/LoadError';
import { type ChartDatum, PeriodColumnChart, ShareBarChart } from './StatisticsCharts';
import {
  type StatisticsRange,
  type StatisticsSearch,
  statisticsRanges,
  validateStatisticsSearch,
} from './statisticsSearch';

/** The longest custom range in days; mirrors `service.MaxStatisticsDays`. */
export const maxStatisticsDays = 366;

type Figures = Statistics['totals'];
type Period = Statistics['periods'][number];
type ClassRow = Statistics['classes'][number];

/** The days from `from` to `to`, both included. */
function daysIn(from: string, to: string): number {
  return Math.round((Date.parse(to) - Date.parse(from)) / 86_400_000) + 1;
}

/** Why a custom range cannot be asked for, or undefined if it can. */
function customRangeProblem(
  from: string | undefined,
  to: string | undefined,
): 'incomplete' | 'backwards' | 'tooLong' | undefined {
  if (!from || !to) return 'incomplete';
  if (to < from) return 'backwards';
  if (daysIn(from, to) > maxStatisticsDays) return 'tooLong';
  return undefined;
}

/**
 * The statistics (/admin/statistics), for the office: totals of a range
 * of school dates as stat tiles, bookings and pass sales by week (or
 * month) and utilization by course type as charts, and every figure in
 * tables by period, class, room, course type and instructor. The range is
 * in the URL: a preset (the last 4 or 12 weeks, the current term) or a
 * custom from–to of at most a year. Aggregates only: nobody is named but
 * the instructors.
 */
export function StatisticsPage() {
  const { t } = useTranslation('statistics');
  const navigate = useNavigate();
  const search = validateStatisticsSearch(useSearch({ strict: false }));
  const range: StatisticsRange = search.range ?? 'last4Weeks';
  const problem = range === 'custom' ? customRangeProblem(search.from, search.to) : undefined;
  const params: StatisticsParams =
    range === 'custom' ? { range, from: search.from, to: search.to } : { range };
  const stats = useQuery({
    ...statisticsQuery(params),
    enabled: problem === undefined,
    placeholderData: keepPreviousData,
  });

  const go = (next: StatisticsSearch) =>
    void navigate({ to: '/admin/statistics', search: next, replace: true });

  const status = stats.error instanceof ApiError ? stats.error.status : undefined;

  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <h1 className="font-heading text-3xl font-semibold tracking-tight">{t('title')}</h1>
        <p className="max-w-prose text-muted-foreground">{t('description')}</p>
      </div>
      <RangeFilter
        range={range}
        search={search}
        shown={stats.data}
        problem={problem}
        onChange={go}
      />
      {problem !== undefined ? null : stats.isPending ? (
        <StatisticsLoading />
      ) : stats.isError && !stats.data ? (
        status === 404 && range === 'term' ? (
          <p role="status" className="text-muted-foreground">
            {t('range.noTerm')}
          </p>
        ) : status === 422 ? (
          <p role="alert" className="text-destructive">
            {t('range.invalid', { count: maxStatisticsDays })}
          </p>
        ) : (
          <LoadError onRetry={() => void stats.refetch()} />
        )
      ) : stats.data ? (
        // While another range loads, the figures stay, faded.
        <div
          className={stats.isPlaceholderData ? 'opacity-60 transition-opacity' : undefined}
          aria-busy={stats.isFetching}
        >
          <StatisticsView stats={stats.data} />
        </div>
      ) : null}
    </div>
  );
}

function RangeFilter({
  range,
  search,
  shown,
  problem,
  onChange,
}: {
  range: StatisticsRange;
  search: StatisticsSearch;
  shown: Statistics | undefined;
  problem: ReturnType<typeof customRangeProblem>;
  onChange: (next: StatisticsSearch) => void;
}) {
  const { t, i18n } = useTranslation('statistics');
  const lang = i18n.resolvedLanguage ?? 'en';

  const choose = (value: string) => {
    if (!value) return;
    const next = value as StatisticsRange;
    if (next !== 'custom') {
      onChange({ range: next });
      return;
    }
    // A custom range starts as the range shown.
    onChange({ range: 'custom', from: search.from ?? shown?.from, to: search.to ?? shown?.to });
  };

  return (
    <div className="space-y-3">
      <ToggleGroup
        type="single"
        variant="outline"
        value={range}
        onValueChange={choose}
        aria-label={t('range.label')}
        className="flex-wrap"
      >
        {statisticsRanges.map((r) => (
          <ToggleGroupItem key={r} value={r}>
            {t(`range.${r}`)}
          </ToggleGroupItem>
        ))}
      </ToggleGroup>
      {range === 'custom' && (
        // The drafts start over from the URL whenever it changes (a new
        // range, the browser's Back button).
        <CustomRangeForm
          key={`${search.from ?? ''}|${search.to ?? ''}`}
          search={search}
          problem={problem}
          onChange={onChange}
        />
      )}
      {shown && (
        <p className="text-sm text-muted-foreground">
          {t('range.summary', {
            from: formatDate(shown.from, lang, { day: 'numeric', month: 'short', year: 'numeric' }),
            to: formatDate(shown.to, lang, { day: 'numeric', month: 'short', year: 'numeric' }),
          })}{' '}
          · {t(shown.granularity === 'month' ? 'range.byMonth' : 'range.byWeek')}
          {shown.term && <> · {t('range.termName', { name: shown.term.name })}</>}
        </p>
      )}
    </div>
  );
}

/** The from and to dates of a custom range, checked before they are asked for. */
function CustomRangeForm({
  search,
  problem,
  onChange,
}: {
  search: StatisticsSearch;
  problem: ReturnType<typeof customRangeProblem>;
  onChange: (next: StatisticsSearch) => void;
}) {
  const { t } = useTranslation('statistics');
  const [from, setFrom] = useState(search.from ?? '');
  const [to, setTo] = useState(search.to ?? '');
  const [submitted, setSubmitted] = useState(false);
  const errorId = useId();
  const draftProblem = customRangeProblem(from || undefined, to || undefined);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setSubmitted(true);
    if (draftProblem === undefined) onChange({ range: 'custom', from, to });
  };

  const shownProblem = submitted ? draftProblem : problem;
  const message =
    shownProblem === undefined
      ? undefined
      : t(`range.${shownProblem}`, { count: maxStatisticsDays });

  return (
    <form className="flex flex-wrap items-end gap-3" onSubmit={submit} noValidate>
      <div className="grid gap-1.5">
        <Label htmlFor="statistics-from">{t('range.from')}</Label>
        <Input
          id="statistics-from"
          type="date"
          value={from}
          onChange={(e) => setFrom(e.target.value)}
          aria-describedby={message ? errorId : undefined}
          className="w-auto"
        />
      </div>
      <div className="grid gap-1.5">
        <Label htmlFor="statistics-to">{t('range.to')}</Label>
        <Input
          id="statistics-to"
          type="date"
          value={to}
          onChange={(e) => setTo(e.target.value)}
          aria-describedby={message ? errorId : undefined}
          className="w-auto"
        />
      </div>
      <Button type="submit">{t('range.show')}</Button>
      {message && (
        <p id={errorId} role="alert" className="basis-full text-sm text-destructive">
          {message}
        </p>
      )}
    </form>
  );
}

function StatisticsLoading() {
  return (
    <div className="space-y-6" aria-hidden="true">
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        {Array.from({ length: 8 }, (_, i) => (
          <Skeleton key={i} className="h-24" />
        ))}
      </div>
      <Skeleton className="h-72" />
    </div>
  );
}

const numberFormatOptions = {
  count: {},
  compact: { notation: 'compact', maximumFractionDigits: 1 },
  share: { style: 'percent', maximumFractionDigits: 0 },
  rate: { style: 'percent', maximumFractionDigits: 1 },
} satisfies Record<string, Intl.NumberFormatOptions>;

const numberFormats = new Map<string, Intl.NumberFormat>();

/** A number format of a language, made once (like `lib/money`'s). */
function numberFormat(lang: string, kind: keyof typeof numberFormatOptions): Intl.NumberFormat {
  const key = `${lang}|${kind}`;
  let f = numberFormats.get(key);
  if (!f) {
    f = new Intl.NumberFormat(lang, numberFormatOptions[kind]);
    numberFormats.set(key, f);
  }
  return f;
}

/** Number formats of the UI language, the same functions while it and the currency stay. */
function useFormats(currency: string) {
  const { i18n } = useTranslation('statistics');
  const lang = i18n.resolvedLanguage ?? 'en';
  return useMemo(
    () => ({
      lang,
      count: (n: number) => numberFormat(lang, 'count').format(n),
      compact: (n: number) => numberFormat(lang, 'compact').format(n),
      share: (n: number) => numberFormat(lang, 'share').format(n),
      rate: (n: number | undefined) =>
        n === undefined ? undefined : numberFormat(lang, 'rate').format(n),
      money: (minor: number) => formatMoney(minor, currency, lang),
      compactMoney: (minor: number) => formatCompactMoney(minor, currency, lang),
    }),
    [lang, currency],
  );
}

type Formats = ReturnType<typeof useFormats>;

function StatisticsView({ stats }: { stats: Statistics }) {
  const { t } = useTranslation('statistics');
  const f = useFormats(stats.currency);
  const monthly = stats.granularity === 'month';
  const periodsTable = t(monthly ? 'tables.periodsByMonth' : 'tables.periodsByWeek');
  const courseTypesTable = t('tables.courseTypes');

  const periodLabel = (p: Period) =>
    monthly
      ? formatDate(p.start, f.lang, { month: 'short', year: '2-digit' })
      : formatDate(p.start, f.lang, { day: 'numeric', month: 'short' });

  const bookings: ChartDatum[] = stats.periods.map((p) => ({
    key: p.start,
    label: periodLabel(p),
    value: p.bookings,
    text: f.count(p.bookings),
  }));
  const sales: ChartDatum[] = stats.periods.map((p) => ({
    key: p.start,
    label: periodLabel(p),
    value: p.salesGross,
    text: f.money(p.salesGross),
  }));
  const utilization: ChartDatum[] = stats.courseTypes.map((c) => ({
    key: c.courseTypeId,
    label: c.name,
    value: c.utilization,
    text: f.share(c.utilization),
  }));

  return (
    <div className="space-y-6">
      <StatTiles totals={stats.totals} f={f} />
      <div className="grid gap-6 xl:grid-cols-2">
        <ChartCard title={t(monthly ? 'charts.bookingsByMonth' : 'charts.bookingsByWeek')}>
          <PeriodColumnChart
            data={bookings}
            label={t(monthly ? 'charts.bookingsByMonth' : 'charts.bookingsByWeek')}
            alt={t('charts.periodsAlt', { table: periodsTable })}
            formatTick={f.compact}
          />
        </ChartCard>
        <ChartCard title={t(monthly ? 'charts.salesByMonth' : 'charts.salesByWeek')}>
          <PeriodColumnChart
            data={sales}
            label={t(monthly ? 'charts.salesByMonth' : 'charts.salesByWeek')}
            alt={t('charts.periodsAlt', { table: periodsTable })}
            formatTick={f.compactMoney}
          />
        </ChartCard>
      </div>
      <PeriodsTable stats={stats} caption={periodsTable} f={f} />
      <ChartCard title={t('charts.utilizationByCourseType')}>
        {utilization.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t('charts.noData')}</p>
        ) : (
          <ShareBarChart
            data={utilization}
            label={t('charts.utilizationByCourseType')}
            alt={t('charts.courseTypesAlt', { table: courseTypesTable })}
            formatTick={f.share}
          />
        )}
      </ChartCard>
      <FiguresTable
        caption={t('tables.classes')}
        nameHeader={t('tables.class')}
        rows={stats.classes.map((c) => ({
          key: c.classId ?? c.lessonId ?? '',
          name: <ClassName row={c} timezone={stats.timezone} lang={f.lang} />,
          figures: c,
        }))}
        f={f}
      />
      <FiguresTable
        caption={courseTypesTable}
        nameHeader={t('tables.courseType')}
        rows={stats.courseTypes.map((c) => ({ key: c.courseTypeId, name: c.name, figures: c }))}
        f={f}
      />
      <FiguresTable
        caption={t('tables.rooms')}
        nameHeader={t('tables.room')}
        rows={stats.rooms.map((r) => ({
          key: r.roomId,
          name: `${r.name} · ${r.locationName}`,
          figures: r,
        }))}
        f={f}
      />
      <FiguresTable
        caption={t('tables.instructors')}
        nameHeader={t('tables.instructor')}
        rows={stats.instructors.map((i) => ({ key: i.staffId, name: i.name, figures: i }))}
        f={f}
      />
    </div>
  );
}

function StatTiles({ totals, f }: { totals: Figures; f: Formats }) {
  const { t } = useTranslation('statistics');
  const taken = totals.attended + totals.noShows;
  const tiles: { key: string; label: string; value: string; detail: string }[] = [
    {
      key: 'bookings',
      label: t('tiles.bookings'),
      value: f.count(totals.bookings),
      detail: t('tiles.bookingsDetail', { count: totals.lessons }),
    },
    {
      key: 'utilization',
      label: t('tiles.utilization'),
      value: f.share(totals.utilization),
      detail: t('tiles.utilizationDetail', {
        bookings: f.count(totals.bookings),
        capacity: f.count(totals.capacity),
      }),
    },
    {
      key: 'noShowRate',
      label: t('tiles.noShowRate'),
      value: f.rate(totals.noShowRate) ?? '–',
      detail:
        taken === 0
          ? t('tiles.noAttendance')
          : t('tiles.noShowRateDetail', {
              noShows: f.count(totals.noShows),
              taken: f.count(taken),
            }),
    },
    {
      key: 'activeMembers',
      label: t('tiles.activeMembers'),
      value: f.count(totals.activeMembers),
      detail: t('tiles.activeMembersDetail'),
    },
    {
      key: 'sales',
      label: t('tiles.sales'),
      value: f.money(totals.salesGross),
      detail: t('tiles.salesDetail', { count: totals.passesSold }),
    },
    {
      key: 'lateCancellations',
      label: t('tiles.lateCancellations'),
      value: f.count(totals.lateCancellations),
      detail: t('tiles.lateCancellationsDetail'),
    },
    {
      key: 'trialsGuests',
      label: t('tiles.trialsGuests'),
      value: f.count(totals.trials + totals.guests),
      detail: t('tiles.trialsGuestsDetail', {
        trials: f.count(totals.trials),
        guests: f.count(totals.guests),
      }),
    },
    {
      key: 'waitlist',
      label: t('tiles.waitlist'),
      value: f.count(totals.waitlistEntries),
      detail: t('tiles.waitlistDetail'),
    },
  ];

  return (
    <section aria-label={t('tiles.label')} className="space-y-2">
      <dl className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        {tiles.map((tile) => (
          <div key={tile.key} className="flex flex-col gap-1 rounded-xl border bg-card p-4">
            <dt className="text-sm text-muted-foreground">{tile.label}</dt>
            <dd className="text-2xl font-semibold tracking-tight">{tile.value}</dd>
            <dd className="text-xs text-muted-foreground">{tile.detail}</dd>
          </div>
        ))}
      </dl>
      {totals.canceledLessons > 0 && (
        <p className="text-sm text-muted-foreground">
          {t('tiles.canceledLessons', { count: totals.canceledLessons })}
        </p>
      )}
    </section>
  );
}

function ChartCard({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          <h2 className="text-base font-semibold">{title}</h2>
        </CardTitle>
      </CardHeader>
      <CardContent>{children}</CardContent>
    </Card>
  );
}

const numeric = 'text-right tabular-nums';

function PeriodsTable({ stats, caption, f }: { stats: Statistics; caption: string; f: Formats }) {
  const { t } = useTranslation('statistics');
  const monthly = stats.granularity === 'month';
  const label = (p: Period) =>
    monthly
      ? formatDate(p.start, f.lang, { month: 'long', year: 'numeric' })
      : p.start === p.end
        ? formatDate(p.start, f.lang, { day: 'numeric', month: 'short' })
        : `${formatDate(p.start, f.lang, { day: 'numeric', month: 'short' })} – ${formatDate(p.end, f.lang, { day: 'numeric', month: 'short' })}`;

  return (
    <section className="space-y-2">
      <h2 className="text-lg font-semibold">{caption}</h2>
      <Table aria-label={caption}>
        <TableHeader>
          <TableRow>
            <TableHead scope="col">{t('tables.period')}</TableHead>
            <TableHead scope="col" className={numeric}>
              {t('tables.lessons')}
            </TableHead>
            <TableHead scope="col" className={numeric}>
              {t('tables.bookings')}
            </TableHead>
            <TableHead scope="col" className={numeric}>
              {t('tables.utilization')}
            </TableHead>
            <TableHead scope="col" className={numeric}>
              {t('tables.attended')}
            </TableHead>
            <TableHead scope="col" className={numeric}>
              {t('tables.noShows')}
            </TableHead>
            <TableHead scope="col" className={numeric}>
              {t('tables.lateCancellations')}
            </TableHead>
            <TableHead scope="col" className={numeric}>
              {t('tables.trials')}
            </TableHead>
            <TableHead scope="col" className={numeric}>
              {t('tables.guests')}
            </TableHead>
            <TableHead scope="col" className={numeric}>
              {t('tables.waitlist')}
            </TableHead>
            <TableHead scope="col" className={numeric}>
              {t('tables.passesSold')}
            </TableHead>
            <TableHead scope="col" className={numeric}>
              {t('tables.sales')}
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {stats.periods.map((p) => (
            <TableRow key={p.start}>
              <TableHead scope="row" className="font-normal whitespace-nowrap">
                <time dateTime={p.start}>{label(p)}</time>
              </TableHead>
              <TableCell className={numeric}>{f.count(p.lessons)}</TableCell>
              <TableCell className={numeric}>{f.count(p.bookings)}</TableCell>
              <TableCell className={numeric}>{f.share(p.utilization)}</TableCell>
              <TableCell className={numeric}>{f.count(p.attended)}</TableCell>
              <TableCell className={numeric}>{f.count(p.noShows)}</TableCell>
              <TableCell className={numeric}>{f.count(p.lateCancellations)}</TableCell>
              <TableCell className={numeric}>{f.count(p.trials)}</TableCell>
              <TableCell className={numeric}>{f.count(p.guests)}</TableCell>
              <TableCell className={numeric}>{f.count(p.waitlistEntries)}</TableCell>
              <TableCell className={numeric}>{f.count(p.passesSold)}</TableCell>
              <TableCell className={`${numeric} whitespace-nowrap`}>
                {f.money(p.salesGross)}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </section>
  );
}

type FigureRow = {
  key: string;
  name: ReactNode;
  figures: Omit<Figures, 'passesSold' | 'salesGross' | 'canceledLessons' | 'activeMembers'>;
};

/** A table of the lesson figures of some groups (classes, rooms, …). */
function FiguresTable({
  caption,
  nameHeader,
  rows,
  f,
}: {
  caption: string;
  nameHeader: string;
  rows: FigureRow[];
  f: Formats;
}) {
  const { t } = useTranslation('statistics');
  return (
    <section className="space-y-2">
      <h2 className="text-lg font-semibold">{caption}</h2>
      {rows.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t('tables.empty')}</p>
      ) : (
        <Table aria-label={caption}>
          <TableHeader>
            <TableRow>
              <TableHead scope="col">{nameHeader}</TableHead>
              <TableHead scope="col" className={numeric}>
                {t('tables.lessons')}
              </TableHead>
              <TableHead scope="col" className={numeric}>
                {t('tables.bookings')}
              </TableHead>
              <TableHead scope="col" className={numeric}>
                {t('tables.capacity')}
              </TableHead>
              <TableHead scope="col" className={numeric}>
                {t('tables.utilization')}
              </TableHead>
              <TableHead scope="col" className={numeric}>
                {t('tables.noShowRate')}
              </TableHead>
              <TableHead scope="col" className={numeric}>
                {t('tables.lateCancellations')}
              </TableHead>
              <TableHead scope="col" className={numeric}>
                {t('tables.waitlist')}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((r) => (
              <TableRow key={r.key}>
                <TableHead scope="row" className="font-normal whitespace-nowrap">
                  {r.name}
                </TableHead>
                <TableCell className={numeric}>{f.count(r.figures.lessons)}</TableCell>
                <TableCell className={numeric}>{f.count(r.figures.bookings)}</TableCell>
                <TableCell className={numeric}>{f.count(r.figures.capacity)}</TableCell>
                <TableCell className={numeric}>{f.share(r.figures.utilization)}</TableCell>
                <TableCell className={numeric}>
                  {f.rate(r.figures.noShowRate) ?? t('tables.notTaken')}
                </TableCell>
                <TableCell className={numeric}>{f.count(r.figures.lateCancellations)}</TableCell>
                <TableCell className={numeric}>{f.count(r.figures.waitlistEntries)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </section>
  );
}

/** A Sunday, from which the weekdays of the API (0 = Sunday) are counted. */
const aSunday = '2026-01-04';

function ClassName({ row, timezone, lang }: { row: ClassRow; timezone: string; lang: string }) {
  const { t } = useTranslation('statistics');
  if (row.startsAt) {
    return (
      <>
        {t('tables.oneOff', {
          courseType: row.courseTypeName,
          when: formatDateTime(row.startsAt, timezone, lang),
        })}
      </>
    );
  }
  return (
    <>
      {t('tables.weekly', {
        courseType: row.courseTypeName,
        weekday: formatDate(addDays(aSunday, row.weekday ?? 0), lang, { weekday: 'short' }),
        time: row.startTime ?? '',
      })}
    </>
  );
}
