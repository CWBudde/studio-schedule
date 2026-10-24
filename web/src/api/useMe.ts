import { useQuery } from '@tanstack/react-query';
import { useBackendStatus } from './backendStatus';
import { meQuery } from './queries';

/**
 * The signed-in account (null: signed out), asked once the backend is ready:
 * in the web demo it starts in a worker first, and asking before would only
 * fail. Pending until then.
 */
export function useMe() {
  const backend = useBackendStatus();
  return useQuery({ ...meQuery, enabled: backend.state === 'ready' });
}
