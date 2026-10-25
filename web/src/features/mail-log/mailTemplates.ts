import type { MailTemplate } from '@/api/queries';

/**
 * Every mail template of the API. The type checks it against the generated
 * enum (from mail.TemplateNames): a template added or removed on the server
 * fails the type check until it is listed here, and the tests check that
 * each has a label in every language.
 */
const templateSet = {
  'confirm-email': true,
  'reset-password': true,
  'change-email': true,
  'email-changed': true,
  invite: true,
  'lesson-canceled': true,
  'lesson-restored': true,
  'lesson-changed': true,
  'booking-confirmed': true,
  'booking-canceled': true,
  'waitlist-invite': true,
  'waitlist-expired': true,
  'lesson-notice': true,
  receipt: true,
  announcement: true,
  'account-deleted': true,
} as const satisfies Record<MailTemplate, true>;
/** The templates, in the order of the filter. */
export const mailTemplates = Object.keys(templateSet) as MailTemplate[];
/** Whether the name is a template of the API. */
export const isTemplate = (name: string | undefined): name is MailTemplate =>
  name !== undefined && Object.hasOwn(templateSet, name);
