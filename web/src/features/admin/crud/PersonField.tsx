import { X } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { type PersonMatch, personsQuery } from '@/api/queries';
import { PersonCombobox } from '@/components/PersonCombobox';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import { maxPersonMatches, personName } from './person';
import { useDescription } from './useDescription';

/**
 * A labelled choice of one person of the school, or none: a button that
 * opens a search over names and emails (the office's person search, asked
 * once the typing pauses) and a button that removes the choice. `value` is
 * the person's id ('' for none); `person` is the person of the initial value
 * (e.g. the one a record links to), so the field can name them.
 */
export function PersonField({
  label,
  value,
  person,
  onChange,
  onPick,
  onBlur,
  error,
  hint,
}: {
  label: string;
  value: string;
  person?: PersonMatch;
  onChange: (personId: string) => void;
  /** The person picked (undefined when the choice is removed), e.g. to fill in their details. */
  onPick?: (person: PersonMatch | undefined) => void;
  onBlur?: () => void;
  error?: string;
  hint?: string;
}) {
  const { t } = useTranslation('admin');
  const { id, describedBy, lines } = useDescription(hint, error);
  /** The person picked here, to name them without asking the API. */
  const [picked, setPicked] = useState<PersonMatch | undefined>(person);

  const known = [picked, person].find((p) => p?.id === value);

  const choose = (next: PersonMatch | undefined) => {
    setPicked(next);
    onChange(next?.id ?? '');
    onPick?.(next);
  };

  return (
    <div className="grid gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      <div className="flex gap-2">
        <PersonCombobox
          id={id}
          shown={known ? personName(known) : t('personField.none')}
          chosen={known !== undefined}
          selectedId={value}
          query={personsQuery}
          limit={maxPersonMatches}
          invalid={!!error}
          describedBy={describedBy}
          className="flex-1"
          texts={{
            search: t('personField.search'),
            list: t('personField.list'),
            searching: t('personField.searching'),
            error: t('personField.error'),
            nobody: t('personField.nobody'),
            noMatches: (query) => t('personField.noMatches', { query }),
            more: t('personField.more', { count: maxPersonMatches }),
          }}
          renderItem={(p) => (
            <span className="min-w-0">
              <span className="block truncate">{personName(p)}</span>
              {p.email && (
                <span className="block truncate text-xs text-muted-foreground">{p.email}</span>
              )}
            </span>
          )}
          onSelect={choose}
          onClose={onBlur}
        />
        {value && (
          <Button
            type="button"
            variant="outline"
            size="icon"
            aria-label={t('personField.clear')}
            onClick={() => choose(undefined)}
          >
            <X aria-hidden="true" />
          </Button>
        )}
      </div>
      {lines}
    </div>
  );
}
