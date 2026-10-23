import type { PersonMatch } from '@/api/queries';

/** The most persons the person search returns; mirrors `service.MaxPersonMatches`. */
export const maxPersonMatches = 50;

/** "Given Family", as the office's lists show a person. */
export const personName = (p: Pick<PersonMatch, 'givenName' | 'familyName'>) =>
  `${p.givenName} ${p.familyName}`;
