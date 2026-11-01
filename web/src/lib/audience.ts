import type { TFunction } from 'i18next';
import type { LessonSummary } from '@/api/client';

/**
 * Who a class is for, as the API states it on a lesson, a class and a
 * statistics row (ADR 0021): a span of levels (lower first, the same level
 * twice for one), or all levels; a stage; an age range in months. Each part
 * is optional.
 */
export type AudienceFields = Pick<
  LessonSummary,
  'levelFrom' | 'levelTo' | 'allLevels' | 'stage' | 'ageFromMonths' | 'ageToMonths'
>;

type LevelRef = NonNullable<LessonSummary['levelFrom']>;

/**
 * The audience in the user's language: 'long' with level names (Programme:
 * "Beginner–Intermediate · Stage 2 · 4–6 yrs"), 'short' with codes, falling
 * back to the name (Werkstatt: "A/M · Stufe 2 · 4–6 J."). '' when nothing is
 * stated.
 */
export function formatAudience(
  a: AudienceFields,
  t: TFunction<'schedule'>,
  style: 'long' | 'short',
): string {
  return [
    levelsText(a, t, style),
    a.stage ? t('audience.stage', { stage: a.stage }) : '',
    agesText(a, t),
  ]
    .filter(Boolean)
    .join(' · ');
}

function levelsText(a: AudienceFields, t: TFunction<'schedule'>, style: 'long' | 'short') {
  if (a.allLevels) return t('audience.allLevels');
  if (!a.levelFrom || !a.levelTo) return '';
  const label = (l: LevelRef) => (style === 'short' ? (l.code ?? l.name) : l.name);
  if (a.levelFrom.id === a.levelTo.id) return label(a.levelFrom);
  return `${label(a.levelFrom)}${style === 'short' ? '/' : '–'}${label(a.levelTo)}`;
}

function agesText(
  { ageFromMonths: from, ageToMonths: to }: AudienceFields,
  t: TFunction<'schedule'>,
) {
  if (from && to) {
    return t('audience.ageRange', { from: formatAgeYears(from), to: formatAgeYears(to) });
  }
  if (from) return t('audience.ageFrom', { age: formatAgeYears(from) });
  if (to) return t('audience.ageTo', { age: formatAgeYears(to) });
  return '';
}

/**
 * Years with halves: 42 → "3½", 48 → "4"; months that are not a half year
 * round down to one (40 → "3"), half a year alone is "½".
 */
export function formatAgeYears(months: number): string {
  const halves = Math.floor(months / 6);
  const years = Math.floor(halves / 2);
  if (halves % 2 === 0) return String(years);
  return years === 0 ? '½' : `${years}½`;
}
