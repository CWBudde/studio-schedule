/**
 * Whether canceling a booking at `now` (the server's clock, in ms) is late:
 * from the cancellation cutoff (`lateCancelFrom`) on. A lesson the school
 * canceled has none, and is never canceled late.
 */
export function cancelIsLate(
  { lateCancelFrom }: { lateCancelFrom?: string },
  now: number,
): boolean {
  return lateCancelFrom !== undefined && now >= Date.parse(lateCancelFrom);
}
