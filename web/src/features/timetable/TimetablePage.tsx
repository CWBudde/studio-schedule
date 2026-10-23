import { type UseQueryResult, useQuery } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { CalendarDays } from 'lucide-react';
import { useId } from 'react';
import { useTranslation } from 'react-i18next';
import {
  classesQuery,
  courseTypesQuery,
  locationsQuery,
  publicSettingsQuery,
  roomsQuery,
  staffQuery,
  termsQuery,
} from '@/api/queries';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { todayIn } from '@/lib/schoolTime';
import { type Catalogue, WeekList } from './WeekList';
import { defaultTerm, type Term, termRange } from './timetable';
import { TimetableEmpty, TimetableError, TimetableLoading } from './TimetableStates';
import { useTimetableSearch } from './useTimetableSearch';

/** Refetches the queries that failed. */
function retryFailed(queries: UseQueryResult[]) {
  for (const q of queries) if (q.isError) void q.refetch();
}

/**
 * The weekly classes of one term (/classes): the term from the URL, else the
 * current one (see defaultTerm), grouped by weekday.
 */
export function TimetablePage() {
  const { t } = useTranslation('timetable');
  const settings = useQuery(publicSettingsQuery);
  const terms = useQuery(termsQuery);

  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <h1 className="font-heading text-3xl font-semibold tracking-tight">{t('title')}</h1>
        <p className="max-w-prose text-muted-foreground">
          {t('intro')}{' '}
          <Link
            to="/schedule"
            className="inline-flex items-center gap-1 font-medium text-foreground underline-offset-4 hover:underline"
          >
            <CalendarDays className="size-4" aria-hidden="true" />
            {t('toSchedule')}
          </Link>
        </p>
      </div>
      {settings.data && terms.data ? (
        terms.data.length === 0 ? (
          <TimetableEmpty
            title={t('empty.noTerms.title')}
            description={t('empty.noTerms.description')}
          />
        ) : (
          <TermView terms={terms.data} timeZone={settings.data.timezone} />
        )
      ) : settings.isError || terms.isError ? (
        <TimetableError onRetry={() => retryFailed([settings, terms])} />
      ) : (
        <TimetableLoading />
      )}
    </div>
  );
}

function TermView({ terms, timeZone }: { terms: Term[]; timeZone: string }) {
  const { search, update } = useTimetableSearch();
  // A linked term that does not exist (any more) falls back to the default.
  const term = terms.find((x) => x.id === search.term) ?? defaultTerm(terms, todayIn(timeZone));
  if (!term) return null;

  return (
    <>
      <TermPicker terms={terms} term={term} onChange={(id) => void update({ term: id })} />
      <TermClasses term={term} />
    </>
  );
}

function TermPicker({
  terms,
  term,
  onChange,
}: {
  terms: Term[];
  term: Term;
  onChange: (termId: string) => void;
}) {
  const { t, i18n } = useTranslation('timetable');
  const lang = i18n.resolvedLanguage ?? 'en';
  const id = useId();

  return (
    <div className="flex flex-col gap-1.5 sm:flex-row sm:items-center sm:gap-3">
      <label htmlFor={id} className="text-sm font-medium">
        {t('term.label')}
      </label>
      <Select value={term.id} onValueChange={onChange}>
        <SelectTrigger id={id} className="w-full sm:w-64">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {terms.map((x) => (
            <SelectItem key={x.id} value={x.id}>
              {x.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <p className="text-sm text-muted-foreground tabular-nums">
        {t('term.range', termRange(term, lang))}
      </p>
    </div>
  );
}

function TermClasses({ term }: { term: Term }) {
  const { t } = useTranslation('timetable');
  const classes = useQuery(classesQuery(term.id));
  const courseTypes = useQuery(courseTypesQuery);
  const rooms = useQuery(roomsQuery);
  const locations = useQuery(locationsQuery);
  const staff = useQuery(staffQuery);
  const queries = [classes, courseTypes, rooms, locations, staff];

  if (queries.some((q) => q.isError)) {
    return <TimetableError onRetry={() => retryFailed(queries)} />;
  }
  if (!classes.data || !courseTypes.data || !rooms.data || !locations.data || !staff.data) {
    return <TimetableLoading />;
  }
  if (classes.data.length === 0) {
    return (
      <TimetableEmpty
        title={t('empty.noClasses.title')}
        description={t('empty.noClasses.description')}
      />
    );
  }

  const catalogue: Catalogue = {
    courseTypes: new Map(courseTypes.data.map((x) => [x.id, x.name])),
    rooms: new Map(rooms.data.map((x) => [x.id, x])),
    locations: new Map(locations.data.map((x) => [x.id, x.name])),
    staff: new Map(staff.data.map((x) => [x.id, x.displayName])),
  };
  return <WeekList classes={classes.data} catalogue={catalogue} />;
}
