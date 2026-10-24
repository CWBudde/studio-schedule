import { z } from 'zod';

/**
 * Search params of /admin/persons: `q`, the words searched for, so going
 * back from a person's page keeps the search. Other values are dropped.
 */
export const personsSearchSchema = z.object({
  q: z
    .union([z.string(), z.number().transform(String)])
    .transform((q) => q.trim() || undefined)
    .optional()
    .catch(undefined),
});

export type PersonsSearch = z.infer<typeof personsSearchSchema>;

export const validatePersonsSearch = (search: Record<string, unknown>): PersonsSearch =>
  personsSearchSchema.parse(search);
