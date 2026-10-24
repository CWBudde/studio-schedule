import type { Role } from '@/api/client';
import { canAdminister } from '@/features/admin/access';

/** The roles that teach or run the school, and so see /teach (the API's `staff` access). */
export const canTeach = (role: Role | undefined) => role === 'instructor' || canAdminister(role);
