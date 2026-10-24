import createClient from 'openapi-fetch';
import type { components, paths } from './schema';
import { transportFetch } from './transport';

/** Typed API client. The transport (HTTP or WASM worker) is chosen at build time. */
export const api = createClient<paths>({ baseUrl: '', fetch: transportFetch });

export type { paths };

type Schemas = components['schemas'];
export type Schedule = Schemas['Schedule'];
export type LessonSummary = Schemas['LessonSummary'];
export type ScheduleLesson = Schemas['ScheduleLesson'];
export type LessonDetail = Schemas['LessonDetail'];
export type LessonLevel = NonNullable<LessonSummary['level']>;
export type LessonKind = LessonSummary['kind'];
export type LessonStatus = LessonSummary['status'];
export type LessonVisibility = LessonSummary['visibility'];

export type Me = Schemas['Me'];
export type Role = Me['role'];
export type Impersonator = Schemas['Impersonator'];
export type AccountEntry = Schemas['AccountEntry'];
export type AuditEventEntry = Schemas['AuditEventEntry'];
export type LoginBody = Schemas['LoginInputBody'];
export type RegisterBody = Schemas['RegisterInputBody'];
export type ConfirmEmailBody = Schemas['ConfirmEmailInputBody'];
export type MailboxMail = Schemas['MailboxMail'];
export type MailStatus = MailboxMail['status'];
export type DemoLogin = Schemas['DemoLogin'];
export type ProblemDetails = Schemas['ErrorModel'];
