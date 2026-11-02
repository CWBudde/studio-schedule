import { describe, expect, it } from 'vitest';
import { assignPassSchema, passSaleValues, sellPassSchema } from './passForms';

const form = (validFrom: string) => ({ ...passSaleValues(), passTypeId: 't', validFrom });

/** The message key `validFrom` fails with in `schema`, or undefined. */
const validFrom = (
  schema: typeof assignPassSchema | ReturnType<typeof sellPassSchema>,
  v: string,
) => {
  const r = schema.safeParse(form(v));
  return r.success ? undefined : r.error.issues.find((i) => i.path[0] === 'validFrom')?.message;
};

describe.each([
  ['assignPassSchema', assignPassSchema],
  ['sellPassSchema', sellPassSchema(2, 'owner')],
])('%s', (_, schema) => {
  it('takes no first day (today) or one that exists', () => {
    expect(validFrom(schema, '')).toBeUndefined();
    expect(validFrom(schema, ' 2026-11-02 ')).toBeUndefined();
  });

  it('refuses a first day that does not exist or no date at all', () => {
    expect(validFrom(schema, '2026-02-30')).toBe('date');
    expect(validFrom(schema, 'tomorrow')).toBe('date');
  });
});

describe('assignPassSchema', () => {
  it('sends the first day only when one is given', () => {
    expect(assignPassSchema.parse(form(''))).toEqual({ passTypeId: 't' });
    expect(assignPassSchema.parse(form('2026-11-02'))).toEqual({
      passTypeId: 't',
      validFrom: '2026-11-02',
    });
  });
});
