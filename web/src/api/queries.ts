import { queryOptions } from '@tanstack/react-query';
import { api } from './client';

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
