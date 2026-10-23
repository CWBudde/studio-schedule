import { queryOptions } from '@tanstack/react-query';
import { api, type paths } from './client';

/** Error thrown by query functions when the API answers with a non-2xx status. */
export class ApiError extends Error {
  readonly status: number;
  readonly body: unknown;

  constructor(status: number, body: unknown) {
    super(`API request failed with status ${status}`);
    this.name = 'ApiError';
    this.status = status;
    this.body = body;
  }
}

export const healthQuery = queryOptions({
  queryKey: ['health'],
  queryFn: async ({ signal }) => {
    const { data, error, response } = await api.GET('/api/v1/health', { signal });
    if (data === undefined) throw new ApiError(response.status, error);
    return data;
  },
});

export const publicSettingsQuery = queryOptions({
  queryKey: ['settings', 'public'],
  queryFn: async ({ signal }) => {
    const { data, error, response } = await api.GET('/api/v1/settings/public', { signal });
    if (data === undefined) throw new ApiError(response.status, error);
    return data;
  },
  staleTime: 5 * 60_000,
});

export type ScheduleParams = NonNullable<paths['/api/v1/schedule']['get']['parameters']['query']>;

/** Prefix of every schedule query key, e.g. to refetch all schedules at a release. */
export const scheduleQueryKey = ['schedule'] as const;

/** The published schedule of a range of school dates (`to` is exclusive, at most 42 days). */
export const scheduleQuery = (params: ScheduleParams) =>
  queryOptions({
    queryKey: [...scheduleQueryKey, params],
    queryFn: async ({ signal }) => {
      const { data, error, response } = await api.GET('/api/v1/schedule', {
        params: { query: params },
        signal,
      });
      if (data === undefined) throw new ApiError(response.status, error);
      return data;
    },
  });

/** One published lesson. Unknown and invisible lessons fail alike with status 404. */
export const lessonQuery = (lessonId: string) =>
  queryOptions({
    queryKey: ['lesson', lessonId],
    queryFn: async ({ signal }) => {
      const { data, error, response } = await api.GET('/api/v1/lessons/{lessonId}', {
        params: { path: { lessonId } },
        signal,
      });
      if (data === undefined) throw new ApiError(response.status, error);
      return data;
    },
  });

export const locationsQuery = queryOptions({
  queryKey: ['locations'],
  queryFn: async ({ signal }) => {
    const { data, error, response } = await api.GET('/api/v1/locations', { signal });
    if (data === undefined) throw new ApiError(response.status, error);
    return data.items;
  },
  staleTime: 5 * 60_000,
});

export const staffQuery = queryOptions({
  queryKey: ['staff'],
  queryFn: async ({ signal }) => {
    const { data, error, response } = await api.GET('/api/v1/staff', { signal });
    if (data === undefined) throw new ApiError(response.status, error);
    return data.items;
  },
  staleTime: 5 * 60_000,
});

export const courseTypesQuery = queryOptions({
  queryKey: ['course-types'],
  queryFn: async ({ signal }) => {
    const { data, error, response } = await api.GET('/api/v1/course-types', { signal });
    if (data === undefined) throw new ApiError(response.status, error);
    return data.items;
  },
  staleTime: 5 * 60_000,
});
