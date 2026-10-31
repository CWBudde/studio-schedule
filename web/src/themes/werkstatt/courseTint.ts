/**
 * The light tints of Werkstatt's lesson cards, one per course type. Light
 * mode only: in dark mode the cards keep --card (werkstatt.css).
 */
export const courseTints = [
  '#ffffe0',
  '#fff8f0',
  '#fff7fe',
  '#f3fff4',
  '#f7f3ff',
  '#f0f8ff',
] as const;

export type CourseTint = (typeof courseTints)[number];

/** The tint of a course type: its id hashed (32-bit FNV-1a) onto courseTints, stable across loads. */
export function courseTint(courseTypeId: string): CourseTint {
  let hash = 0x811c9dc5;
  for (let i = 0; i < courseTypeId.length; i++) {
    hash ^= courseTypeId.charCodeAt(i);
    hash = Math.imul(hash, 0x01000193);
  }
  return courseTints[(hash >>> 0) % courseTints.length] ?? courseTints[0];
}
