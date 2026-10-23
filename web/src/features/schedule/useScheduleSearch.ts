import { useNavigate, useSearch } from '@tanstack/react-router';
import { useCallback } from 'react';
import type { ScheduleSearch } from './search';

/**
 * The search params of /schedule and a function to change some of them.
 * A key set to undefined is removed from the URL.
 */
export function useScheduleSearch() {
  const search = useSearch({ from: '/schedule' });
  const navigate = useNavigate({ from: '/schedule' });

  const update = useCallback(
    (patch: Partial<ScheduleSearch>, options: { replace?: boolean } = {}) =>
      navigate({ search: (prev) => ({ ...prev, ...patch }), replace: options.replace }),
    [navigate],
  );

  return { search, update };
}
