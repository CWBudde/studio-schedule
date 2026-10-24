import { z } from 'zod';
import type { LessonCloneData } from '@/api/queries';
import { addDays, type SchoolDate, startOfWeek, weekdayOf } from '@/lib/schoolTime';

// The rules of the API (previewLessonClone, cloneLessons). Messages are keys
// of `auth:form.errors` or `admin:form.errors` (see useAdminErrorText).

export const cloneUnits = ['day', 'week'] as const;
export type CloneUnit = (typeof cloneUnits)[number];

/** The form values: the unit and two school dates 'YYYY-MM-DD' as the inputs hold them. */
export type CloneInput = { unit: CloneUnit; source: string; target: string };

export const cloneFields = ['unit', 'source', 'target'] as const satisfies (keyof CloneInput)[];

const datePattern = /^\d{4}-\d{2}-\d{2}$/;
const isDate = (value: string) => datePattern.test(value);
const isMonday = (date: SchoolDate) => weekdayOf(date) === 1;

/**
 * The earliest target the API accepts on `today`: today for a day; for a
 * week its Monday, so the current week only on a Monday.
 */
export function earliestTarget(unit: CloneUnit, today: SchoolDate): SchoolDate {
  if (unit === 'day' || isMonday(today)) return today;
  return addDays(startOfWeek(today), 7);
}

/**
 * The clone form on `today`: two dates, both Mondays for a week; the
 * target differs from the source and does not lie before earliestTarget.
 */
export const cloneSchema = (today: SchoolDate) =>
  z
    .object({
      unit: z.enum(cloneUnits),
      source: z.string().regex(datePattern, 'date'),
      target: z.string().regex(datePattern, 'date'),
    })
    .superRefine((v, ctx) => {
      if (!isDate(v.source) || !isDate(v.target)) return;
      const week = v.unit === 'week';
      if (week && !isMonday(v.source)) {
        ctx.addIssue({ code: 'custom', path: ['source'], message: 'monday' });
      }
      if (week && !isMonday(v.target)) {
        ctx.addIssue({ code: 'custom', path: ['target'], message: 'monday' });
      } else if (v.target === v.source) {
        ctx.addIssue({ code: 'custom', path: ['target'], message: 'cloneSame' });
      } else if (v.target < earliestTarget(v.unit, today)) {
        ctx.addIssue({
          code: 'custom',
          path: ['target'],
          message: week ? 'cloneWeekBegun' : 'clonePast',
        });
      }
    }) satisfies z.ZodType<LessonCloneData, CloneInput>;

/**
 * The form values the schedule suggests: the shown day (a day) or the
 * shown week (a week) as the source, and the same day or week a week
 * later as the target, but not before earliestTarget.
 */
export function cloneDefaults(unit: CloneUnit, shown: SchoolDate, today: SchoolDate): CloneInput {
  const source = unit === 'week' ? startOfWeek(shown) : shown;
  const target = addDays(source, 7);
  const earliest = earliestTarget(unit, today);
  return { unit, source, target: target < earliest ? earliest : target };
}

/**
 * The form values after a switch to `unit`: a week snaps both dates to the
 * Monday of their week, a target before earliestTarget to that.
 */
export function switchUnit(values: CloneInput, unit: CloneUnit, today: SchoolDate): CloneInput {
  if (unit === 'day') return { ...values, unit };
  const snap = (date: string) => (isDate(date) ? startOfWeek(date) : date);
  const target = snap(values.target);
  const earliest = earliestTarget(unit, today);
  return {
    unit,
    source: snap(values.source),
    target: isDate(target) && target < earliest ? earliest : target,
  };
}
