import { z } from 'zod';
import type { Level, LevelData } from '@/api/queries';

// Limits as in the API (LevelData), counted in code points like the server.
const length = (s: string) => [...s].length;

/** A level's form: an empty code means none. */
export const levelSchema = z.object({
  name: z
    .string()
    .trim()
    .min(1, 'required')
    .refine((s) => length(s) <= 60, 'tooLong'),
  code: z
    .string()
    .trim()
    .refine((s) => length(s) <= 8, 'tooLong'),
}) satisfies z.ZodType<LevelData>;

export type LevelValues = z.infer<typeof levelSchema>;

export const levelFields = ['name', 'code'] as const satisfies (keyof LevelValues)[];

export const levelValues = (level?: Level): LevelValues => ({
  name: level?.name ?? '',
  code: level?.code ?? '',
});

/** The request body of the form values: an empty code is left out. */
export const levelData = ({ name, code }: LevelValues): LevelData => ({
  name,
  ...(code ? { code } : {}),
});
