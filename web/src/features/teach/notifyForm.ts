import { z } from 'zod';
import { maxNoticeMessage } from '@/api/queries';

/** Line breaks as the API takes them: CR LF, CR, the line and the paragraph separator. */
const lineBreaks = /\r\n|[\r\u{2028}\u{2029}]/gu;

/** The message as the API counts it: line breaks normalized, the space around it trimmed. */
export const normalizeMessage = (message: string) => message.replace(lineBreaks, '\n').trim();

/** The length of a message as the API counts it, in code points (an emoji counts once). */
export const messageLength = (message: string) => [...normalizeMessage(message)].length;

/** The form of a message to a lesson's participants; the API checks the characters. */
export const notifySchema = z.object({
  message: z
    .string()
    .refine((m) => messageLength(m) > 0, 'required')
    .refine((m) => messageLength(m) <= maxNoticeMessage, 'tooLong'),
});

export type NotifyValues = z.infer<typeof notifySchema>;
