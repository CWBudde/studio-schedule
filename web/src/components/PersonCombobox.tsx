import {
  keepPreviousData,
  type QueryKey,
  useQuery,
  type UseQueryOptions,
} from '@tanstack/react-query';
import { ChevronsUpDown } from 'lucide-react';
import { cn } from 'cn';
import { type ReactNode, useState } from 'react';
import { Button } from '@/components/ui/button';
import {
  Command,
  CommandEmpty,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { useDebouncedValue } from '@/features/admin/crud/useDebouncedValue';

/** The words of a person combobox, already translated. */
export type PersonComboboxTexts = {
  /** Placeholder and accessible name of the search input. */
  search: string;
  /** Accessible name of the list of matches. */
  list: string;
  searching: string;
  error: string;
  /** What to type first while the search is shorter than `minLength`. */
  tooShort?: string;
  /** No match for the (trimmed) search. */
  noMatches: (query: string) => string;
  /** No match for an empty search, e.g. "There are no persons yet." */
  nobody?: string;
  /** The search stopped at `limit`, so there may be more. */
  more: string;
};

/**
 * A button that opens a search over the persons of the school, asked once
 * the typing pauses, and lists the matches to pick one. The query, the
 * rendering of a match and the words come from the caller, so the office's
 * person search and the staff's share it. `limit` is the most matches the
 * search returns: a full page says there may be more.
 */
export function PersonCombobox<P extends { id: string }, K extends QueryKey>({
  id,
  shown,
  chosen,
  selectedId,
  query: queryFor,
  minLength = 0,
  maxLength,
  limit,
  renderItem,
  texts,
  invalid,
  describedBy,
  className,
  onSelect,
  onClose,
}: {
  /** The id of the trigger button, for its label. */
  id: string;
  /** What the trigger shows: the chosen person or a placeholder. */
  shown: ReactNode;
  /** Whether a person is chosen (else `shown` is muted as a placeholder). */
  chosen: boolean;
  selectedId?: string;
  query: (search: string) => UseQueryOptions<P[], Error, P[], K>;
  minLength?: number;
  /** The longest search to type. */
  maxLength?: number;
  limit: number;
  renderItem: (person: P) => ReactNode;
  texts: PersonComboboxTexts;
  invalid?: boolean;
  describedBy?: string;
  className?: string;
  onSelect: (person: P) => void;
  /** Called when the list closes, e.g. to mark the field as touched. */
  onClose?: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [typed, setTyped] = useState('');
  const search = useDebouncedValue(typed, 250).trim();
  const searchable = search.length >= minLength;
  const matches = useQuery({
    ...queryFor(search),
    enabled: open && searchable,
    placeholderData: keepPreviousData,
  });

  let status: string | undefined;
  if (!searchable) status = texts.tooShort;
  else if (matches.isError) status = texts.error;
  else if (matches.data === undefined) status = texts.searching;

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) {
          setTyped('');
          onClose?.();
        }
      }}
      // Modal, so the list scrolls inside a sheet or dialog.
      modal
    >
      <PopoverTrigger asChild>
        <Button
          id={id}
          type="button"
          variant="outline"
          role="combobox"
          aria-expanded={open}
          aria-invalid={invalid ? true : undefined}
          aria-describedby={describedBy}
          className={cn('min-w-0 justify-between font-normal', className)}
        >
          <span className={chosen ? 'truncate' : 'truncate text-muted-foreground'}>{shown}</span>
          <ChevronsUpDown className="text-muted-foreground" aria-hidden="true" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-(--radix-popover-trigger-width) min-w-64 p-0">
        <Command shouldFilter={false} label={texts.search}>
          <CommandInput
            placeholder={texts.search}
            value={typed}
            maxLength={maxLength}
            onValueChange={setTyped}
          />
          <CommandList label={texts.list}>
            {status ? (
              <p role="status" className="py-6 text-center text-sm text-muted-foreground">
                {status}
              </p>
            ) : (
              <>
                <CommandEmpty>
                  {search || !texts.nobody ? texts.noMatches(search) : texts.nobody}
                </CommandEmpty>
                {matches.data?.map((p) => (
                  <CommandItem
                    key={p.id}
                    value={p.id}
                    data-checked={p.id === selectedId}
                    onSelect={() => {
                      onSelect(p);
                      setOpen(false);
                      setTyped('');
                    }}
                  >
                    {renderItem(p)}
                  </CommandItem>
                ))}
              </>
            )}
          </CommandList>
          {/* The search stops at the limit, so there may be more matches. */}
          {searchable && matches.data?.length === limit && (
            <p className="border-t px-3 py-2 text-xs text-muted-foreground">{texts.more}</p>
          )}
        </Command>
      </PopoverContent>
    </Popover>
  );
}
