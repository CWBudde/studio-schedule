/** How strong a new password looks: a hint for the person typing it (ASVS 2.1.8). */
export type PasswordStrength = 'weak' | 'fair' | 'good' | 'strong';

/**
 * Estimates a password's strength from its length and the kinds of
 * characters it uses (lower case, upper case, digits, others). It knows no
 * list of common passwords: the server refuses those (ASVS 2.1.7), so the
 * meter is only a nudge towards longer passwords. Length counts characters,
 * as the server does.
 */
export function passwordStrength(password: string): PasswordStrength {
  const chars = [...password];
  if (chars.length < 8 || new Set(chars).size < 4) return 'weak';

  const classes = [/\p{Ll}/u, /\p{Lu}/u, /\p{N}/u, /[^\p{Ll}\p{Lu}\p{N}]/u].filter((re) =>
    re.test(password),
  ).length;

  let score = 1; // at least 8 characters
  if (chars.length >= 12) score++;
  if (chars.length >= 16) score++;
  if (classes >= 2) score++;
  if (classes >= 3) score++;

  if (score >= 4) return 'strong';
  if (score === 3) return 'good';
  if (score === 2) return 'fair';
  return 'weak';
}
