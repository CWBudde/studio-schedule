import { describe, expect, it } from 'vitest';
import { ApiError } from '@/api/queries';
import { characters, loginSchema, passwordProblem, registerSchema } from './form';

const registration = {
  givenName: 'Rae',
  familyName: 'Example',
  email: 'rae@example.org',
};

/** The message keys the schema reports for the password. */
const passwordErrors = (password: string) =>
  (registerSchema.safeParse({ ...registration, password }).error?.issues ?? [])
    .filter((i) => i.path[0] === 'password')
    .map((i) => i.message);

describe('password length', () => {
  it('counts characters (code points), as the API does', () => {
    expect(characters('🩰🩰🩰🩰')).toBe(4);
    expect('🩰🩰🩰🩰'.length).toBe(8);
  });

  it('needs at least 8 characters, however many UTF-16 units they take', () => {
    expect(passwordErrors('🩰🩰🩰🩰')).toEqual(['passwordMin']);
    expect(passwordErrors('🩰🩰🩰🩰🩰🩰🩰🩰')).toEqual([]);
  });

  it('allows at most 256 characters', () => {
    expect(passwordErrors('🩰'.repeat(256))).toEqual([]);
    expect(passwordErrors('🩰'.repeat(257))).toEqual(['passwordMax']);
    expect(
      loginSchema.safeParse({ email: 'rae@example.org', password: '🩰'.repeat(256) }).success,
    ).toBe(true);
  });
});

describe('passwordProblem', () => {
  const refused = (message: string, location = 'body.password') =>
    new ApiError(422, {
      status: 422,
      detail: 'validation failed',
      errors: [{ location, message }],
    });

  it.each([
    ['the password is one of the most common ones; choose another', 'passwordCommon'],
    ['the password must have at least 8 characters', 'passwordMin'],
    ['expected length >= 8', 'passwordMin'],
    ['the password must have at most 256 characters', 'passwordMax'],
    ['expected length <= 256', 'passwordMax'],
    ['something else', 'invalid'],
  ])('maps %j to %s', (message, key) => {
    expect(passwordProblem(refused(message), 'password')).toBe(key);
  });

  it('names nothing for another field or another error', () => {
    expect(
      passwordProblem(refused('expected length >= 8', 'body.email'), 'password'),
    ).toBeUndefined();
    expect(passwordProblem(new ApiError(401, {}), 'password')).toBeUndefined();
    expect(passwordProblem(new Error('offline'), 'password')).toBeUndefined();
  });
});
