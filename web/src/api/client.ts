import createClient from 'openapi-fetch';
import type { components, paths } from './schema';
import { transportFetch } from './transport';

/** Typed API client. The transport (HTTP or WASM worker) is chosen at build time. */
export const api = createClient<paths>({ baseUrl: '', fetch: transportFetch });

export type { paths };

type Schemas = components['schemas'];
export type Schedule = Schemas['Schedule'];
export type LessonSummary = Schemas['LessonSummary'];
export type LessonDetail = Schemas['LessonDetail'];
export type LessonLevel = NonNullable<LessonSummary['level']>;
export type LessonKind = LessonSummary['kind'];
export type LessonStatus = LessonSummary['status'];
