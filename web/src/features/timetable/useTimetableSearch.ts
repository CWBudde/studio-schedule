import { useNavigate, useSearch } from '@tanstack/react-router';
import { useCallback } from 'react';
import type { TimetableSearch } from './search';

/**
 * The search params of /classes and a function to change some of them.
 * A key set to undefined is removed from the URL.
 */
export function useTimetableSearch() {
  const search = useSearch({ from: '/classes' });
  const navigate = useNavigate({ from: '/classes' });

  const update = useCallback(
    (patch: Partial<TimetableSearch>) => navigate({ search: (prev) => ({ ...prev, ...patch }) }),
    [navigate],
  );

  return { search, update };
}
