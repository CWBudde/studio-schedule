import { useQuery } from '@tanstack/react-query';
import { familyQuery } from '@/api/queries';

/** Whether the family has dependents, so the rows of "My lessons" name the person they are for. */
export function useNamesPersons(): boolean {
  const family = useQuery(familyQuery);
  return (family.data?.dependents.length ?? 0) > 0;
}
