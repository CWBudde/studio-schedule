import { z } from 'zod';
import type { Announcement, AnnouncementAudience, AnnouncementData } from '@/api/queries';
import { instantAt, schoolDateOf, wallClockOf } from '@/lib/schoolTime';

// Limits as in the API (ADR 0014). Messages are keys of `auth:form.errors`,
// `admin:form.errors` or `announcements:admin.errors` (see useErrorText).

/** The longest title, in characters (code points, as the API counts). */
export const maxTitleLength = 120;
/** The longest text, in characters. */
export const maxBodyLength = 4000;

export const audiences = ['everyone', 'role', 'class', 'course-type', 'lesson'] as const;
export const roles = ['member', 'instructor', 'office', 'admin'] as const;

export type Audience = (typeof audiences)[number];

/** The number of code points, as the API counts characters. */
const length = (s: string) => [...s].length;

const dateTime = /^(\d{4}-\d{2}-\d{2})?$/;

/**
 * The form of an announcement, its dates and times in the school's time
 * zone `timeZone`. Every field is checked at once, and the output is the
 * request body (without a publication time: now).
 */
export const announcementSchema = (timeZone: string) =>
  z
    .object({
      title: z.string(),
      body: z.string(),
      audience: z.enum(audiences),
      role: z.string(),
      termId: z.string(),
      classId: z.string(),
      courseTypeId: z.string(),
      lessonDate: z.string(),
      lessonId: z.string(),
      publishDate: z.string().regex(dateTime),
      publishTime: z.string(),
      expiresDate: z.string().regex(dateTime),
      expiresTime: z.string(),
      email: z.boolean(),
    })
    .transform((v, ctx): AnnouncementData => {
      const issue = (path: keyof typeof v, message: string) =>
        ctx.addIssue({ code: 'custom', path: [path], message });
      const title = v.title.trim();
      if (!title) issue('title', 'required');
      else if (length(title) > maxTitleLength || /[\r\n]/.test(title)) issue('title', 'titleLine');
      const body = v.body.trim();
      if (!body) issue('body', 'required');
      else if (length(body) > maxBodyLength) issue('body', 'tooLong');

      const audience: AnnouncementAudience = { audience: v.audience };
      if (v.audience === 'role') {
        if ((roles as readonly string[]).includes(v.role)) {
          audience.role = v.role as (typeof roles)[number];
        } else issue('role', 'choose');
      }
      if (v.audience === 'class') {
        if (v.classId) audience.classId = v.classId;
        else issue('classId', 'choose');
      }
      if (v.audience === 'course-type') {
        if (v.courseTypeId) audience.courseTypeId = v.courseTypeId;
        else issue('courseTypeId', 'choose');
      }
      if (v.audience === 'lesson') {
        if (v.lessonId) audience.lessonId = v.lessonId;
        else issue('lessonId', 'choose');
      }

      const instant = (date: string, time: string, field: 'publishDate' | 'expiresDate') => {
        if (!date && !time) return undefined;
        if (!date || !/^\d{2}:\d{2}$/.test(time)) {
          issue(field, 'dateTime');
          return undefined;
        }
        return instantAt(date, time, timeZone);
      };
      const publishAt = instant(v.publishDate, v.publishTime, 'publishDate');
      const expiresAt = instant(v.expiresDate, v.expiresTime, 'expiresDate');
      if (expiresAt && Date.parse(expiresAt) <= Date.parse(publishAt ?? new Date().toISOString())) {
        issue('expiresDate', 'expiresBeforePublish');
      }

      return {
        ...audience,
        title,
        body,
        ...(publishAt ? { publishAt } : {}),
        ...(expiresAt ? { expiresAt } : {}),
        ...(v.email ? { email: true } : {}),
      };
    });

export type AnnouncementValues = z.input<ReturnType<typeof announcementSchema>>;

/** The fields a 422 of the API can name, as the form's fields. */
export const announcementFields = [
  'title',
  'body',
  'audience',
  'role',
  'classId',
  'courseTypeId',
  'lessonId',
] as const satisfies (keyof AnnouncementValues)[];

/** The API's names of the date fields, which the form splits into date and time. */
export const dateFields = { publishAt: 'publishDate', expiresAt: 'expiresDate' } as const;

/** The form values of an announcement, or of a new one for everyone, now, in the app only. */
export function announcementValues(
  timeZone: string,
  announcement?: Announcement,
): AnnouncementValues {
  const date = (instant: string | undefined) => (instant ? schoolDateOf(instant, timeZone) : '');
  const time = (instant: string | undefined) => (instant ? wallClockOf(instant, timeZone) : '');
  return {
    title: announcement?.title ?? '',
    body: announcement?.body ?? '',
    audience: announcement?.audience ?? 'everyone',
    role: announcement?.role ?? '',
    termId: announcement?.class?.termId ?? '',
    classId: announcement?.classId ?? '',
    courseTypeId: announcement?.courseTypeId ?? '',
    lessonDate: date(announcement?.lesson?.startsAt),
    lessonId: announcement?.lessonId ?? '',
    publishDate: date(announcement?.publishAt),
    publishTime: time(announcement?.publishAt),
    expiresDate: date(announcement?.expiresAt),
    expiresTime: time(announcement?.expiresAt),
    email: announcement?.email ?? false,
  };
}

/** The audience part of a request body, for counting its recipients. */
export const audienceOf = ({
  audience,
  role,
  classId,
  courseTypeId,
  lessonId,
}: AnnouncementData): AnnouncementAudience => ({
  audience,
  ...(role ? { role } : {}),
  ...(classId ? { classId } : {}),
  ...(courseTypeId ? { courseTypeId } : {}),
  ...(lessonId ? { lessonId } : {}),
});

/** Whether an announcement is scheduled, shown or expired at `now`. */
/**
 * Whether the mailing of the announcement has begun: it is emailed, or being
 * sent (some addresses have it). Its email can no longer be turned off.
 */
export function mailStarted(a: Pick<Announcement, 'emailedAt' | 'emailRecipients'>) {
  return Boolean(a.emailedAt) || a.emailRecipients > 0;
}

export function statusAt(a: Pick<Announcement, 'publishAt' | 'expiresAt'>, now: number) {
  if (a.publishAt && Date.parse(a.publishAt) > now) return 'scheduled' as const;
  if (a.expiresAt && Date.parse(a.expiresAt) <= now) return 'expired' as const;
  return 'shown' as const;
}
