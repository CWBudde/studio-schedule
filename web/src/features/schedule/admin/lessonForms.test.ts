import { describe, expect, it } from 'vitest';
import { type LessonInput, lessonSchema } from './lessonForms';

/** A one-off lesson's form values that pass, but for `changes`. */
const lesson = (changes: Partial<LessonInput> = {}): LessonInput => ({
  courseTypeId: 'course-type',
  date: '2026-11-10',
  startTime: '18:00',
  durationMinutes: '60',
  roomId: 'room',
  instructorId: 'instructor',
  accompanistId: '',
  capacity: '12',
  visibility: 'public',
  kind: 'regular',
  renter: '',
  ...changes,
});

/** The message keys of a lesson form's values by field, none when it passes. */
const errorsOf = (values: LessonInput) => {
  const r = lessonSchema.safeParse(values);
  return r.success ? {} : Object.fromEntries(r.error.issues.map((i) => [i.path[0], i.message]));
};

describe('lessonSchema', () => {
  it('refuses a date that does not exist', () => {
    expect(errorsOf(lesson({ date: '2026-02-30' }))).toEqual({ date: 'date' });
    expect(errorsOf(lesson({ date: '10.11.2026' }))).toEqual({ date: 'date' });
  });

  it('accepts a date that exists', () => {
    expect(errorsOf(lesson())).toEqual({});
    expect(errorsOf(lesson({ date: '2028-02-29' }))).toEqual({});
  });
});
