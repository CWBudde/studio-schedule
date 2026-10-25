import { z } from 'zod';

/**
 * Search params of /admin/receipts: `receipt`, the receipt whose detail is
 * open (a link from a person's pass). Other values are dropped.
 */
export const receiptsSearchSchema = z.object({
  receipt: z.uuid().optional().catch(undefined),
});

export type ReceiptsSearch = z.infer<typeof receiptsSearchSchema>;

export const validateReceiptsSearch = (search: Record<string, unknown>): ReceiptsSearch =>
  receiptsSearchSchema.parse(search);
