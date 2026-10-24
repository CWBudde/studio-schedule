import { useNavigate, useSearch } from '@tanstack/react-router';
import { useCallback } from 'react';
import type { TeachSearch } from './search';

/**
 * The search params of /teach and a function to change some of them. A key
 * set to undefined is removed from the URL.
 */
export function useTeachSearch() {
  const search = useSearch({ from: '/teach' });
  const navigate = useNavigate({ from: '/teach' });

  const update = useCallback(
    (patch: Partial<TeachSearch>, options: { replace?: boolean } = {}) =>
      navigate({ search: (prev) => ({ ...prev, ...patch }), replace: options.replace }),
    [navigate],
  );

  return { search, update };
}
