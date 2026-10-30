import { z } from 'zod';
import type {
  BookingRules,
  BookingRulesData,
  BrandingSettings,
  BrandingSettingsData,
  RegionalSettings,
  RegionalSettingsData,
  SchoolSettings,
} from '@/api/queries';
import { accentContrast, minAccentContrast, parseColor } from '@/lib/color';

// Limits as in the API (SchoolSettings, BrandingSettingsData,
// RegionalSettingsData, BookingRulesData). Messages are keys of
// `settings:errors`, `admin:form.errors` or `auth:form.errors` (see
// useSettingsErrorText).

/** The school's name and legal notice; an empty notice is left out. */
export const schoolSchema = z
  .object({
    schoolName: z.string().trim().min(1, 'required').max(200, 'tooLong'),
    legalNotice: z.string().trim().max(2000, 'tooLong'),
  })
  .transform(({ schoolName, legalNotice }): SchoolSettings => ({
    schoolName,
    ...(legalNotice ? { legalNotice } : {}),
  }));

export type SchoolValues = z.input<typeof schoolSchema>;
export const schoolFields = ['schoolName', 'legalNotice'] as const satisfies (keyof SchoolValues)[];
export const schoolValues = (s: SchoolSettings): SchoolValues => ({
  schoolName: s.schoolName,
  legalNotice: s.legalNotice ?? '',
});

export const themes = ['programme', 'werkstatt'] as const satisfies BrandingSettings['theme'][];

/**
 * The accent color: empty for the app's own, else #rrggbb that reaches
 * the contrast the server asks for (lower-cased here); and the theme.
 */
export const brandingSchema = z
  .object({
    accentColor: z
      .string()
      .trim()
      .refine((v) => v === '' || parseColor(v) !== undefined, 'hexColor')
      .refine((v) => {
        const c = parseColor(v);
        return !c || accentContrast(c) >= minAccentContrast;
      }, 'contrast'),
    theme: z.enum(themes),
  })
  .transform(({ accentColor, theme }): BrandingSettingsData => ({
    ...(accentColor ? { accentColor: accentColor.toLowerCase() } : {}),
    theme,
  }));

export type BrandingValues = z.input<typeof brandingSchema>;
export const brandingFields = ['accentColor', 'theme'] as const satisfies (keyof BrandingValues)[];
export const brandingValues = (s: BrandingSettings): BrandingValues => ({
  accentColor: s.accentColor ?? '',
  theme: s.theme,
});

/** Whether the browser knows the IANA time zone. */
export function knownTimeZone(zone: string): boolean {
  try {
    new Intl.DateTimeFormat('en', { timeZone: zone });
    return true;
  } catch {
    return false;
  }
}

export const locales = ['en', 'de'] as const satisfies RegionalSettingsData['defaultLocale'][];

/** The time zone, language and currency (upper-cased here). */
export const regionalSchema = z.object({
  timezone: z
    .string()
    .trim()
    .min(1, 'required')
    .max(64, 'tooLong')
    .refine((v) => v === '' || knownTimeZone(v), 'timezone'),
  defaultLocale: z.enum(locales),
  currency: z
    .string()
    .trim()
    .toUpperCase()
    .regex(/^[A-Z]{3}$/, 'currency'),
}) satisfies z.ZodType<RegionalSettingsData, unknown>;

export type RegionalValues = z.input<typeof regionalSchema>;
export const regionalFields = [
  'timezone',
  'defaultLocale',
  'currency',
] as const satisfies (keyof RegionalValues)[];
export const regionalValues = (s: RegionalSettings): RegionalValues => ({
  timezone: s.timezone,
  defaultLocale: s.defaultLocale,
  currency: s.currency,
});

/** The unit a duration is entered in. */
export const units = ['minutes', 'hours', 'days'] as const;
export type Unit = (typeof units)[number];
const unitMinutes: Record<Unit, number> = { minutes: 1, hours: 60, days: 1440 };

/** A duration in minutes as the largest unit that holds it whole, e.g. 1440 → 1 day. */
export function splitMinutes(minutes: number): { amount: string; unit: Unit } {
  const unit: Unit =
    minutes > 0 && minutes % 1440 === 0
      ? 'days'
      : minutes > 0 && minutes % 60 === 0
        ? 'hours'
        : 'minutes';
  return { amount: String(minutes / unitMinutes[unit]), unit };
}

const maxMinutes = 30 * 1440;
const whole = z
  .string()
  .trim()
  .regex(/^\d{1,6}$/, 'invalid');

/** The durations of the form, by the API's field of each. */
export const durations = [
  { field: 'bookingCutoff', api: 'bookingCutoffMinutes', min: 0, message: 'cutoff' },
  { field: 'cancellationCutoff', api: 'cancellationCutoffMinutes', min: 0, message: 'cutoff' },
  {
    field: 'waitlistInviteExpiry',
    api: 'waitlistInviteExpiryMinutes',
    min: 1,
    message: 'inviteExpiry',
  },
] as const;

/**
 * The booking rules: the release (weekday 0 = Sunday, time, weeks ahead),
 * three durations entered in a unit each, and the days of early access.
 * The output is the API's body, the durations in minutes.
 */
export const rulesSchema = z
  .object({
    releaseWeekday: z.enum(['0', '1', '2', '3', '4', '5', '6']),
    releaseTime: z.string().regex(/^([01]\d|2[0-3]):[0-5]\d$/, 'startTime'),
    releaseWeeksAhead: whole.refine((v) => +v >= 1 && +v <= 52, 'weeksAhead'),
    bookingCutoff: whole,
    bookingCutoffUnit: z.enum(units),
    cancellationCutoff: whole,
    cancellationCutoffUnit: z.enum(units),
    waitlistInviteExpiry: whole,
    waitlistInviteExpiryUnit: z.enum(units),
    defaultVisibleDays: whole.refine((v) => +v >= 1 && +v <= 366, 'visibleDays'),
  })
  .superRefine((v, ctx) => {
    for (const d of durations) {
      const minutes = Number(v[d.field]) * unitMinutes[v[`${d.field}Unit`]];
      if (minutes < d.min || minutes > maxMinutes) {
        ctx.addIssue({ code: 'custom', path: [d.field], message: d.message });
      }
    }
  })
  .transform((v): BookingRulesData => ({
    releaseWeekday: Number(v.releaseWeekday),
    releaseTime: v.releaseTime,
    releaseWeeksAhead: Number(v.releaseWeeksAhead),
    bookingCutoffMinutes: Number(v.bookingCutoff) * unitMinutes[v.bookingCutoffUnit],
    cancellationCutoffMinutes: Number(v.cancellationCutoff) * unitMinutes[v.cancellationCutoffUnit],
    waitlistInviteExpiryMinutes:
      Number(v.waitlistInviteExpiry) * unitMinutes[v.waitlistInviteExpiryUnit],
    defaultVisibleDays: Number(v.defaultVisibleDays),
  }));

export type RulesValues = z.input<typeof rulesSchema>;
export type RulesField = keyof RulesValues;

export function rulesValues(r: BookingRules): RulesValues {
  const booking = splitMinutes(r.bookingCutoffMinutes);
  const cancellation = splitMinutes(r.cancellationCutoffMinutes);
  const invite = splitMinutes(r.waitlistInviteExpiryMinutes);
  return {
    releaseWeekday: String(r.releaseWeekday) as RulesValues['releaseWeekday'],
    releaseTime: r.releaseTime,
    releaseWeeksAhead: String(r.releaseWeeksAhead),
    bookingCutoff: booking.amount,
    bookingCutoffUnit: booking.unit,
    cancellationCutoff: cancellation.amount,
    cancellationCutoffUnit: cancellation.unit,
    waitlistInviteExpiry: invite.amount,
    waitlistInviteExpiryUnit: invite.unit,
    defaultVisibleDays: String(r.defaultVisibleDays),
  };
}

/** The form fields that a 422 on the API's fields marks. */
export const rulesServerFields: Partial<Record<keyof BookingRulesData, RulesField>> = {
  releaseWeekday: 'releaseWeekday',
  releaseTime: 'releaseTime',
  releaseWeeksAhead: 'releaseWeeksAhead',
  bookingCutoffMinutes: 'bookingCutoff',
  cancellationCutoffMinutes: 'cancellationCutoff',
  waitlistInviteExpiryMinutes: 'waitlistInviteExpiry',
  defaultVisibleDays: 'defaultVisibleDays',
};
