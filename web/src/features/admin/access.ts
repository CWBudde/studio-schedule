import type { Role } from '@/api/client';

/** The roles that see the administration (ADR 0005: office operations, which admins may do too). */
export const canAdminister = (role: Role | undefined) => role === 'office' || role === 'admin';
