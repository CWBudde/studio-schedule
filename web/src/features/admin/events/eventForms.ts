import { z } from 'zod';
import type {
  AdminEvent,
  AddEventRegistrationData,
  EventData,
  EventPaymentData,
} from '@/api/queries';
import { instantAt, isSchoolDate, schoolDateOf, wallClockOf } from '@/lib/schoolTime';
import { moneyInput, parseDecimal, rateInput } from '@/lib/money';
import { moneyValue } from '../passTypeForms';
import { receiptPaymentFields, receiptPaymentData } from '../passForms';

export const eventDateFields = ['signupOpensAt', 'signupClosesAt', 'cancelUntil'] as const;

const dateInput = (date: string, timeZone: string) =>
  `${schoolDateOf(date, timeZone)}T${wallClockOf(date, timeZone)}`;

const optionalDateTime = z
  .string()
  .refine(
    (value) =>
      value === '' ||
      (/^\d{4}-\d{2}-\d{2}T(?:[01]\d|2[0-3]):[0-5]\d$/.test(value) &&
        isSchoolDate(value.slice(0, 10))),
    'date',
  );

const eventFieldsSchema = z.object({
  title: z.string().trim().min(1, 'required').max(200, 'tooLong'),
  description: z.string().trim().max(10_000, 'tooLong'),
  slug: z
    .string()
    .trim()
    .max(80, 'tooLong')
    .regex(/^(?:[a-z0-9]+(?:-[a-z0-9]+)*)?$/, 'slugInvalid'),
  courseTypeId: z.string().min(1, 'choose'),
  capacity: z
    .string()
    .trim()
    .regex(/^\d+$/, 'capacityInvalid')
    .refine((value) => Number(value) >= 1 && Number(value) <= 10_000, 'capacityInvalid'),
  price: z.string().trim(),
  vatRate: z.string().trim(),
  signupOpensAt: optionalDateTime,
  signupClosesAt: optionalDateTime,
  cancelUntil: optionalDateTime,
});

export type EventValues = z.input<typeof eventFieldsSchema>;
export const eventFields = eventFieldsSchema.keyof().options;

export const eventSchema = (digits: number, timeZone: string, editing?: AdminEvent) =>
  eventFieldsSchema.transform((values, ctx): EventData => {
    const price = moneyValue(values.price, digits);
    const vatRate = parseDecimal(values.vatRate, 2);
    if (price === undefined) {
      ctx.addIssue({ code: 'custom', path: ['price'], message: 'money' });
    }
    if (vatRate === undefined || vatRate > 10_000) {
      ctx.addIssue({ code: 'custom', path: ['vatRate'], message: 'vatRate' });
    }

    const dates: Pick<EventData, (typeof eventDateFields)[number]> = {};
    for (const field of eventDateFields) {
      const value = values[field];
      if (!value) continue;
      const saved = editing?.[field];
      // Preserve the original instant if the displayed wall-clock value
      // stayed the same, including during the repeated hour of a DST fold.
      dates[field] =
        saved && dateInput(saved, timeZone) === value
          ? saved
          : instantAt(value.slice(0, 10), value.slice(11), timeZone);
    }
    if (
      dates.signupOpensAt &&
      dates.signupClosesAt &&
      Date.parse(dates.signupClosesAt) <= Date.parse(dates.signupOpensAt)
    ) {
      ctx.addIssue({ code: 'custom', path: ['signupClosesAt'], message: 'signupOrder' });
    }

    return {
      title: values.title,
      ...(values.slug ? { slug: values.slug } : {}),
      ...(values.description ? { description: values.description } : {}),
      courseTypeId: values.courseTypeId,
      capacity: Number(values.capacity),
      price: price ?? 0,
      vatRate: vatRate ?? 0,
      ...dates,
    };
  });

export function eventValues(
  currency: string,
  locale: string,
  timeZone: string,
  event?: AdminEvent,
): EventValues {
  const date = (value: string | undefined) => (value ? dateInput(value, timeZone) : '');
  return {
    title: event?.title ?? '',
    description: event?.description ?? '',
    slug: event?.slug ?? '',
    courseTypeId: event?.courseTypeId ?? '',
    capacity: event ? String(event.capacity) : '',
    price: moneyInput(event?.price ?? 0, currency, locale),
    vatRate: rateInput(event?.vatRate ?? 0, locale),
    signupOpensAt: date(event?.signupOpensAt),
    signupClosesAt: date(event?.signupClosesAt),
    cancelUntil: date(event?.cancelUntil),
  };
}

export const registrationSchema = z
  .object({
    mode: z.enum(['person', 'guest']),
    personId: z.string(),
    guest: z.object({ name: z.string().trim(), email: z.string().trim() }),
  })
  .transform((value, ctx): AddEventRegistrationData => {
    if (value.mode === 'person') {
      if (!value.personId) {
        ctx.addIssue({ code: 'custom', path: ['personId'], message: 'choosePerson' });
      }
      return { personId: value.personId };
    }
    const { name, email } = value.guest;
    if (!name || name.length > 200) {
      ctx.addIssue({
        code: 'custom',
        path: ['guest', 'name'],
        message: name ? 'tooLong' : 'required',
      });
    }
    if (!z.email().safeParse(email).success || email.length > 254) {
      ctx.addIssue({
        code: 'custom',
        path: ['guest', 'email'],
        message: email ? 'email' : 'required',
      });
    }
    return { guest: { name, email } };
  });

export type RegistrationValues = z.input<typeof registrationSchema>;

export const paymentSchema = z
  .object(receiptPaymentFields)
  .transform((values): EventPaymentData => receiptPaymentData(values));
export type PaymentValues = z.input<typeof paymentSchema>;
export const paymentFields = z.object(receiptPaymentFields).keyof().options;
