import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { ChevronsUpDown, X } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { type PersonMatch, personsQuery } from '@/api/queries';
import { Button } from '@/components/ui/button';
import {
  Command,
  CommandEmpty,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command';
import { Label } from '@/components/ui/label';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { maxPersonMatches, personName } from './person';
import { useDescription } from './useDescription';
import { useDebouncedValue } from './useDebouncedValue';

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
  onBlur,
  error,
  hint,
}: {
  label: string;
  value: string;
  person?: PersonMatch;
  onChange: (personId: string) => void;
  onBlur?: () => void;
  error?: string;
  hint?: string;
}) {
  const { t } = useTranslation('admin');
  const { id, describedBy, lines } = useDescription(hint, error);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  /** The person picked here, to name them without asking the API. */
  const [picked, setPicked] = useState<PersonMatch | undefined>(person);
  const search = useDebouncedValue(query, 250);
  const matches = useQuery({
    ...personsQuery(search),
    enabled: open,
    placeholderData: keepPreviousData,
  });

  const known = [picked, person].find((p) => p?.id === value);
  const shown = known ? personName(known) : t('personField.none');

  const choose = (next: PersonMatch | undefined) => {
    setPicked(next);
    onChange(next?.id ?? '');
  };

  return (
    <div className="grid gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      <div className="flex gap-2">
        <Popover
          open={open}
          onOpenChange={(next) => {
            setOpen(next);
            if (!next) {
              setQuery('');
              onBlur?.();
            }
          }}
          // Modal, so the list scrolls inside a sheet.
          modal
        >
          <PopoverTrigger asChild>
            <Button
              id={id}
              type="button"
              variant="outline"
              role="combobox"
              aria-expanded={open}
              aria-invalid={error ? true : undefined}
              aria-describedby={describedBy}
              className="min-w-0 flex-1 justify-between font-normal"
            >
              <span className={known ? 'truncate' : 'truncate text-muted-foreground'}>{shown}</span>
              <ChevronsUpDown className="text-muted-foreground" aria-hidden="true" />
            </Button>
          </PopoverTrigger>
          <PopoverContent align="start" className="w-(--radix-popover-trigger-width) min-w-64 p-0">
            <Command shouldFilter={false} label={t('personField.search')}>
              <CommandInput
                placeholder={t('personField.search')}
                value={query}
                onValueChange={setQuery}
              />
              <CommandList label={t('personField.list')}>
                {matches.data === undefined ? (
                  <p role="status" className="py-6 text-center text-sm text-muted-foreground">
                    {matches.isError ? t('personField.error') : t('personField.searching')}
                  </p>
                ) : (
                  <>
                    <CommandEmpty>
                      {search.trim()
                        ? t('personField.noMatches', { query: search.trim() })
                        : t('personField.nobody')}
                    </CommandEmpty>
                    {matches.data.map((p) => (
                      <CommandItem
                        key={p.id}
                        value={p.id}
                        data-checked={p.id === value}
                        onSelect={() => {
                          choose(p);
                          setOpen(false);
                          setQuery('');
                        }}
                      >
                        <span className="min-w-0">
                          <span className="block truncate">{personName(p)}</span>
                          {p.email && (
                            <span className="block truncate text-xs text-muted-foreground">
                              {p.email}
                            </span>
                          )}
                        </span>
                      </CommandItem>
                    ))}
                  </>
                )}
              </CommandList>
              {/* The search stops at the limit, so there may be more matches. */}
              {matches.data?.length === maxPersonMatches && (
                <p className="border-t px-3 py-2 text-xs text-muted-foreground">
                  {t('personField.more', { count: maxPersonMatches })}
                </p>
              )}
            </Command>
          </PopoverContent>
        </Popover>
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
