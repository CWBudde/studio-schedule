import { describe, expect, it } from 'vitest';
import { termSchema } from './termForms';

/** The message keys of a term's form, by field. */
const problems = (startsOn: string, endsOn: string) => {
  const r = termSchema.safeParse({ name: 'Autumn', startsOn, endsOn });
  // The form shows a field's first problem.
  return r.success
    ? {}
    : Object.fromEntries(r.error.issues.reverse().map((i) => [i.path[0], i.message]));
};

describe('termSchema', () => {
  it('takes a first and a last day that exist', () => {
    expect(problems('2026-09-01', '2027-02-28')).toEqual({});
  });

  it('refuses a day that does not exist or no date at all', () => {
    expect(problems('2026-02-30', '2026-04-31')).toEqual({ startsOn: 'date', endsOn: 'date' });
    expect(problems('2026-9-1', 'soon')).toEqual({ startsOn: 'date', endsOn: 'date' });
  });

  it('asks for both days', () => {
    expect(problems('', '')).toEqual({ startsOn: 'required', endsOn: 'required' });
    // An empty day is only missing, not also a wrong date.
    const r = termSchema.safeParse({ name: 'Autumn', startsOn: '', endsOn: '2026-09-01' });
    expect(r.error?.issues.map((i) => i.message)).toEqual(['required']);
  });

  it('refuses an end before the start', () => {
    expect(problems('2026-09-02', '2026-09-01')).toEqual({ endsOn: 'endBeforeStart' });
  });
});
