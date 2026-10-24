import { useTranslation } from 'react-i18next';
import {
  maxStaffPersonMatches,
  maxStaffPersonQuery,
  minStaffPersonQuery,
  type StaffPersonMatch,
  staffPersonsQuery,
} from '@/api/queries';
import { PersonCombobox } from '@/components/PersonCombobox';
import { Label } from '@/components/ui/label';
import { useDescription } from '@/features/admin/crud/useDescription';

/**
 * A labelled choice of one person of the school for staff: a button that
 * opens a search over given and family names (from two characters on),
 * listing names and birth years. `person` is the chosen person, or undefined.
 */
export function StaffPersonField({
  label,
  person,
  onChange,
  onBlur,
  error,
}: {
  label: string;
  person: StaffPersonMatch | undefined;
  onChange: (person: StaffPersonMatch) => void;
  onBlur?: () => void;
  error?: string;
}) {
  const { t } = useTranslation('schedule');
  const { id, describedBy, lines } = useDescription(undefined, error);

  return (
    <div className="grid gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      <PersonCombobox
        id={id}
        shown={person ? person.name : t('participants.add.choose')}
        chosen={person !== undefined}
        selectedId={person?.id}
        query={staffPersonsQuery}
        minLength={minStaffPersonQuery}
        maxLength={maxStaffPersonQuery}
        limit={maxStaffPersonMatches}
        invalid={!!error}
        describedBy={describedBy}
        className="w-full"
        texts={{
          search: t('participants.add.search'),
          list: t('participants.add.list'),
          searching: t('participants.add.searching'),
          error: t('participants.add.searchError'),
          tooShort: t('participants.add.minChars', { count: minStaffPersonQuery }),
          noMatches: (query) => t('participants.add.noMatches', { query }),
          more: t('participants.add.more', { count: maxStaffPersonMatches }),
        }}
        renderItem={(p) => (
          <>
            <span className="min-w-0 truncate">{p.name}</span>
            {p.birthYear !== undefined && (
              <span className="ml-auto shrink-0 text-xs text-muted-foreground">
                {t('participants.add.born', { year: p.birthYear })}
              </span>
            )}
          </>
        )}
        onSelect={onChange}
        onClose={onBlur}
      />
      {lines}
    </div>
  );
}
