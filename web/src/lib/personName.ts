import i18n from 'i18next';

/**
 * A person as the API names it. `anonymized` (or a person's `anonymizedAt`)
 * marks a person the office anonymized (ADR 0016): the API keeps a fixed
 * placeholder name for it, which the app shows translated.
 */
type NamedPerson = {
  givenName: string;
  familyName: string;
  anonymized?: boolean;
  anonymizedAt?: string;
};

/** The name the app shows for an anonymized person, in the current language. */
export const deletedPersonName = () => i18n.t('deletedPerson');

/** "Given Family", as the app shows a person; "Deleted person" for an anonymized one. */
export const personName = (p: NamedPerson) =>
  p.anonymized || p.anonymizedAt ? deletedPersonName() : `${p.givenName} ${p.familyName}`;

/** A full name the API put together (`name`), or "Deleted person" for an anonymized person. */
export const shownName = (p: { name: string; anonymized?: boolean }) =>
  p.anonymized ? deletedPersonName() : p.name;
