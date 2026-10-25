import { z } from 'zod';

/** The range presets of the statistics, as the API names them. */
export const statisticsRanges = ['last4Weeks', 'last12Weeks', 'term', 'custom'] as const;

export type StatisticsRange = (typeof statisticsRanges)[number];

const schoolDate = z.iso.date().optional().catch(undefined);

/**
 * Search params of /admin/statistics: the `range` (default: the last four
 * weeks) and, for a custom range, its `from` and `to` school dates. Other
 * values are dropped.
 */
export const statisticsSearchSchema = z.object({
  range: z.enum(statisticsRanges).optional().catch(undefined),
  from: schoolDate,
  to: schoolDate,
});

export type StatisticsSearch = z.infer<typeof statisticsSearchSchema>;

export const validateStatisticsSearch = (search: Record<string, unknown>): StatisticsSearch =>
  statisticsSearchSchema.parse(search);
