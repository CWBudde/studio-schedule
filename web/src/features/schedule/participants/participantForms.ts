import { z } from 'zod';
import type { AddParticipantData, UpdateParticipantData } from '@/api/queries';

// Messages are keys of `auth:form.errors` or `admin:form.errors` (see
// useAdminErrorText). Limits as in the API (AddParticipantData, GuestData).

/** The longest trial note, in characters (code points, as the API counts). */
export const maxTrialNote = 500;
const trialNote = z
  .string()
  .trim()
  .refine((note) => [...note].length <= maxTrialNote, 'tooLong');

/**
 * The form that adds a participant: a person of the school (`mode`
 * 'person', `personId`) or a guest with a name and an optional email, and
 * whether it is a trial with a note for the instructor.
 */
export const addParticipantSchema = z
  .object({
    mode: z.enum(['person', 'guest']),
    personId: z.string(),
    guestName: z.string().trim().max(200, 'tooLong'),
    guestEmail: z.string().trim().max(254, 'tooLong'),
    trial: z.boolean(),
    trialNote,
  })
  .superRefine((v, ctx) => {
    if (v.mode === 'person' && !v.personId) {
      ctx.addIssue({ code: 'custom', path: ['personId'], message: 'choosePerson' });
    }
    if (v.mode === 'guest') {
      if (!v.guestName) ctx.addIssue({ code: 'custom', path: ['guestName'], message: 'required' });
      if (v.guestEmail && !z.email().safeParse(v.guestEmail).success) {
        ctx.addIssue({ code: 'custom', path: ['guestEmail'], message: 'email' });
      }
    }
  });

export type AddParticipantValues = z.infer<typeof addParticipantSchema>;

export const emptyParticipant: AddParticipantValues = {
  mode: 'person',
  personId: '',
  guestName: '',
  guestEmail: '',
  trial: false,
  trialNote: '',
};

/** The trial part of a body: a note only with the flag, an empty one left out. */
const trialOf = ({ trial, trialNote }: { trial: boolean; trialNote: string }) =>
  trial ? { trial, ...(trialNote ? { trialNote } : {}) } : {};

/** The request body of the form values; only the fields of the chosen mode. */
export function addParticipantData(
  v: AddParticipantValues,
  confirmOverCapacity = false,
): AddParticipantData {
  const who: AddParticipantData =
    v.mode === 'person'
      ? { personId: v.personId }
      : { guest: { name: v.guestName, ...(v.guestEmail ? { email: v.guestEmail } : {}) } };
  return { ...who, ...trialOf(v), ...(confirmOverCapacity ? { confirmOverCapacity } : {}) };
}

/** Where the API names the fields of the add form in a 422 (`body.…`). */
export const addParticipantLocations = {
  personId: 'body.personId',
  guestName: 'body.guest.name',
  guestEmail: 'body.guest.email',
  trialNote: 'body.trialNote',
} as const satisfies Partial<Record<keyof AddParticipantValues, string>>;

/** The form that marks a booking as a trial, with a note for the instructor. */
export const trialSchema = z.object({ trial: z.boolean(), trialNote });

export type TrialValues = z.infer<typeof trialSchema>;

/** The request body of the trial form: without the flag, no note. */
export const trialData = (v: TrialValues): UpdateParticipantData => ({
  trial: v.trial,
  ...(v.trial && v.trialNote ? { trialNote: v.trialNote } : {}),
});
