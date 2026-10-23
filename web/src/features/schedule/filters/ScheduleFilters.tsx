import { useQuery } from '@tanstack/react-query';
import { X } from 'lucide-react';
import { useId } from 'react';
import { useTranslation } from 'react-i18next';
import { courseTypesQuery, locationsQuery, staffQuery } from '@/api/queries';
import { Button } from '@/components/ui/button';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { useScheduleSearch } from '../useScheduleSearch';
import { clearedFilters, hasActiveFilter, levelOptions } from './filterParams';

/** Value of the "all" option; Radix Select does not allow an empty value. */
const ALL = 'all';

type Option = { value: string; label: string };

/**
 * Selects for location, instructor, course type and level, bound to the
 * search params of /schedule, plus a button that clears them all.
 */
export function ScheduleFilters() {
  const { t } = useTranslation('schedule');
  const { search, update } = useScheduleSearch();
  const locations = useQuery(locationsQuery);
  const staff = useQuery(staffQuery);
  const courseTypes = useQuery(courseTypesQuery);

  return (
    <div
      role="group"
      aria-label={t('filters.label')}
      className="grid grid-cols-2 gap-x-3 gap-y-2 lg:flex lg:flex-wrap lg:items-end"
    >
      <FilterSelect
        label={t('filters.location')}
        allLabel={t('filters.allLocations')}
        value={search.location}
        options={locations.data?.map((l) => ({ value: l.id, label: l.name }))}
        onChange={(location) => void update({ location })}
      />
      <FilterSelect
        label={t('filters.instructor')}
        allLabel={t('filters.allInstructors')}
        value={search.instructor}
        options={staff.data?.map((s) => ({ value: s.id, label: s.displayName }))}
        onChange={(instructor) => void update({ instructor })}
      />
      <FilterSelect
        label={t('filters.courseType')}
        allLabel={t('filters.allCourseTypes')}
        value={search.courseType}
        options={courseTypes.data?.map((c) => ({ value: c.id, label: c.name }))}
        onChange={(courseType) => void update({ courseType })}
      />
      <FilterSelect
        label={t('filters.level')}
        allLabel={t('filters.anyLevel')}
        value={search.level}
        options={levelOptions.map((level) => ({ value: level, label: t(`level.${level}`) }))}
        onChange={(value) => void update({ level: levelOptions.find((l) => l === value) })}
      />
      {hasActiveFilter(search) && (
        <Button
          variant="ghost"
          className="col-span-2 justify-self-start lg:col-auto"
          onClick={() => void update(clearedFilters)}
        >
          <X aria-hidden="true" />
          {t('filters.reset')}
        </Button>
      )}
    </div>
  );
}

/**
 * One labelled filter select. Without options (catalogue loading or failed)
 * it is disabled.
 */
function FilterSelect({
  label,
  allLabel,
  value,
  options,
  onChange,
}: {
  label: string;
  allLabel: string;
  value: string | undefined;
  options: Option[] | undefined;
  onChange: (value: string | undefined) => void;
}) {
  const id = useId();
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <label htmlFor={id} className="text-xs font-medium text-muted-foreground">
        {label}
      </label>
      <Select
        value={value ?? ALL}
        onValueChange={(v) => onChange(v === ALL ? undefined : v)}
        disabled={options === undefined}
      >
        <SelectTrigger id={id} className="w-full lg:w-48">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ALL}>{allLabel}</SelectItem>
          {options?.map((o) => (
            <SelectItem key={o.value} value={o.value}>
              {o.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
