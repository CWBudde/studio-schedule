import { renderHook } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import {
  announcementSchema,
  announcementValues,
  type AnnouncementValues,
  useAnnouncementErrorText,
} from './announcementForms';

const timeZone = 'Europe/Berlin';

const values = (v: Partial<AnnouncementValues>): AnnouncementValues => ({
  ...announcementValues(timeZone),
  title: 'Studio closed',
  body: 'The studio stays closed on Monday.',
  ...v,
});

/** The issues of the form values as [path, message], or the request body. */
const parse = (v: Partial<AnnouncementValues>) => {
  const r = announcementSchema(timeZone).safeParse(values(v));
  return r.success ? r.data : r.error.issues.map((i) => [i.path.join('.'), i.message]);
};

describe('announcementSchema', () => {
  it('turns existing dates into instants', () => {
    expect(
      parse({
        publishDate: '2028-02-29',
        publishTime: '09:00',
        expiresDate: '2028-03-01',
        expiresTime: '18:30',
      }),
    ).toMatchObject({
      publishAt: '2028-02-29T08:00:00.000Z',
      expiresAt: '2028-03-01T17:30:00.000Z',
    });
    expect(parse({})).not.toHaveProperty('publishAt');
  });

  it('refuses a publication or expiry date that does not exist', () => {
    expect(parse({ publishDate: '2026-02-30', publishTime: '09:00' })).toEqual([
      ['publishDate', 'date'],
    ]);
    expect(parse({ expiresDate: '2027-02-29', expiresTime: '09:00' })).toEqual([
      ['expiresDate', 'date'],
    ]);
  });

  it('checks the other fields at the same time', () => {
    expect(parse({ title: '', publishDate: '2026-02-30', publishTime: '09:00' })).toEqual([
      ['title', 'required'],
      ['publishDate', 'date'],
    ]);
  });

  it('still asks for the missing half of a date and time', () => {
    expect(parse({ publishTime: '09:00' })).toEqual([['publishDate', 'dateTime']]);
    expect(parse({ expiresDate: '2026-12-01' })).toEqual([['expiresDate', 'dateTime']]);
  });

  it('refuses a lesson date that does not exist', () => {
    expect(parse({ audience: 'lesson', lessonDate: '2026-02-30' })).toEqual([
      ['lessonDate', 'date'],
      ['lessonId', 'choose'],
    ]);
    expect(parse({ audience: 'lesson', lessonDate: '2026-02-28', lessonId: 'l1' })).toMatchObject({
      audience: 'lesson',
      lessonId: 'l1',
    });
    // The hidden lesson date of another audience is not checked.
    expect(parse({ audience: 'everyone', lessonDate: '2026-02-30' })).toMatchObject({
      audience: 'everyone',
    });
  });
});

describe('useAnnouncementErrorText', () => {
  it('translates its own keys and the admin forms', () => {
    const { result } = renderHook(() => useAnnouncementErrorText());
    expect(result.current({ type: 'custom', message: 'date' })).toBe('Enter a date.');
    expect(result.current({ type: 'custom', message: 'dateTime' })).toBe(
      'Enter a date and a time.',
    );
  });
});
