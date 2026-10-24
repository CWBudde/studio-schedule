import { z } from 'zod';
import type { Location, LocationData, Room, RoomData } from '@/api/queries';

// Limits as in the API (LocationData, RoomData). Messages are keys of
// `auth:form.errors` or `admin:form.errors` (see useAdminErrorText).
/** The name of a catalogue record: 1 to 100 characters. */
export const catalogueName = z.string().trim().min(1, 'required').max(100, 'tooLong');
const name = catalogueName;

/** A location's form: an empty address means none. */
export const locationSchema = z.object({
  name,
  address: z.string().trim().max(200, 'tooLong'),
}) satisfies z.ZodType<LocationData>;

export type LocationValues = z.infer<typeof locationSchema>;

export const locationFields = ['name', 'address'] as const satisfies (keyof LocationValues)[];

export const locationValues = (location?: Location): LocationValues => ({
  name: location?.name ?? '',
  address: location?.address ?? '',
});

/** The request body of the form values: an empty address is left out. */
export const locationData = ({ name, address }: LocationValues): LocationData => ({
  name,
  ...(address ? { address } : {}),
});

/** The capacity as typed: a whole number from 1 to 1000. */
export const capacity = z
  .string()
  .trim()
  .regex(/^\d+$/, 'capacity')
  .transform(Number)
  .pipe(z.number().int().min(1, 'capacity').max(1000, 'capacity'));

/** A room's form; the capacity is the text of a number input. */
export const roomSchema = z.object({
  name,
  locationId: z.string().min(1, 'location'),
  capacity,
}) satisfies z.ZodType<RoomData, { name: string; locationId: string; capacity: string }>;

export type RoomInput = z.input<typeof roomSchema>;

export const roomFields = ['name', 'locationId', 'capacity'] as const satisfies (keyof RoomInput)[];

/** The form values of a room, or of a new one at `locationId`. */
export const roomValues = (room: Room | undefined, locationId = ''): RoomInput => ({
  name: room?.name ?? '',
  locationId: room?.locationId ?? locationId,
  capacity: room ? String(room.capacity) : '',
});
