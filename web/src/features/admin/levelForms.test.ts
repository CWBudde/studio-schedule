import { describe, expect, it } from 'vitest';
import { levelSchema } from './levelForms';

/** The message keys the schema reports for each field. */
const errors = (name: string, code = '') =>
  (levelSchema.safeParse({ name, code }).error?.issues ?? []).map(
    (i) => `${String(i.path[0])}: ${i.message}`,
  );

describe('levelSchema', () => {
  it('counts characters (code points), as the API does', () => {
    expect(errors('🩰'.repeat(60), '🩰'.repeat(8))).toEqual([]);
    expect(errors('🩰'.repeat(61), '🩰'.repeat(9))).toEqual(['name: tooLong', 'code: tooLong']);
  });

  it('needs a name and leaves the code optional', () => {
    expect(errors('  ')).toEqual(['name: required']);
    expect(errors('Beginner')).toEqual([]);
  });
});
