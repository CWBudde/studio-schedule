import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Merge } from 'lucide-react';
import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import {
  type DuplicateCandidate,
  type DuplicatePair,
  type MergeSummary,
  type MergeTake,
  mergePersonsMutation,
  mergePreviewQuery,
} from '@/api/queries';
import { ProblemAlert } from '@/components/ProblemAlert';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Skeleton } from '@/components/ui/skeleton';
import { TextareaField } from '@/features/admin/crud/fields';
import { personName } from '@/lib/personName';
import { useDebouncedValue } from '@/features/admin/crud/useDebouncedValue';
import { invalidFields } from '@/features/auth/form';
import { weekdayName } from '@/features/timetable/timetable';
import { publicSettingsQuery } from '@/api/queries';
import { formatDate, formatTimestamp } from '@/lib/schoolTime';

/** The fields whose value the office picks, with how a candidate shows it. */
const choices = ['name', 'email', 'phone', 'birthDate', 'memberNo'] as const;
type Choice = (typeof choices)[number];

const valueOf = (p: DuplicateCandidate, field: Choice): string | undefined =>
  field === 'name' ? personName(p) : p[field];

/** The notes of both, as the server keeps them unless the office edits them. */
export function combinedNotes(survivor?: string, merged?: string): string {
  const own = survivor ?? '';
  if (!merged || own.includes(merged)) return own;
  return `${own}\n\n${merged}`.trim();
}

/**
 * The merge of a pair; closed while `pair` is null. The office picks the
 * person that stays and, where they differ, whose value each field keeps;
 * the preview tells what moves and what is canceled before anything does.
 */
export function MergeDialog({
  pair,
  onClose,
}: {
  pair: DuplicatePair | null;
  onClose: () => void;
}) {
  const { t } = useTranslation('duplicates');
  return (
    <Dialog open={pair !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t('dialog.title')}</DialogTitle>
          <DialogDescription>{t('dialog.description')}</DialogDescription>
        </DialogHeader>
        {pair && <MergeForm key={`${pair.a.id}/${pair.b.id}`} pair={pair} onDone={onClose} />}
      </DialogContent>
    </Dialog>
  );
}

function MergeForm({ pair, onDone }: { pair: DuplicatePair; onDone: () => void }) {
  const { t, i18n } = useTranslation('duplicates');
  const lang = i18n.resolvedLanguage ?? 'en';
  /** A field's value as the choice shows it. */
  const display = (p: DuplicateCandidate, f: Choice) =>
    f === 'birthDate' && p.birthDate
      ? formatDate(p.birthDate, lang, { day: 'numeric', month: 'long', year: 'numeric' })
      : valueOf(p, f);
  const queryClient = useQueryClient();
  const merge = useMutation(mergePersonsMutation(queryClient));
  // The person with an account stays by default; it cannot move.
  const [keepB, setKeepB] = useState(
    pair.b.accountEmail !== undefined && pair.a.accountEmail === undefined,
  );
  const survivor = keepB ? pair.b : pair.a;
  const merged = keepB ? pair.a : pair.b;
  const [fromMerged, setFromMerged] = useState<Partial<Record<Choice, boolean>>>({});
  // The survivor's notes after the merge: both, which the office may edit
  // (at most 2000 characters, else the server refuses on notes).
  const hasNotes = Boolean(pair.a.notes || pair.b.notes);
  const [notes, setNotes] = useState(() => combinedNotes(survivor.notes, merged.notes));
  const previewNotes = useDebouncedValue(notes, 300);
  // A field the survivor lacks takes the duplicate's value unless chosen otherwise.
  const take: MergeTake = Object.fromEntries(
    choices.map((f) => [
      f,
      fromMerged[f] ?? (!valueOf(survivor, f) && Boolean(valueOf(merged, f))),
    ]),
  );
  const request = { survivorId: survivor.id, mergedId: merged.id, take };
  const preview = useQuery(
    mergePreviewQuery(hasNotes ? { ...request, notes: previewNotes } : request),
  );
  const notesError = [preview.error, merge.error].some(
    (e) => invalidFields(e, ['notes'] as const).length > 0,
  )
    ? t('dialog.notesTooLong', { max: 2000 })
    : undefined;
  const otherError = (error: unknown) =>
    invalidFields(error, ['notes'] as const).length > 0 ? null : error;
  const differing = choices.filter(
    (f) => (valueOf(survivor, f) ?? '') !== (valueOf(merged, f) ?? ''),
  );
  const conflicts = {
    'two-accounts': t('dialog.twoAccounts'),
    'guardian-cycle': t('dialog.cycle'),
  };

  return (
    <div className="space-y-5">
      <fieldset className="space-y-2">
        <legend className="mb-1 font-medium">{t('dialog.survivor')}</legend>
        {[pair.a, pair.b].map((p, i) => (
          <Radio
            key={p.id}
            name="survivor"
            checked={keepB === (i === 1)}
            onChange={() => {
              setKeepB(i === 1);
              setFromMerged({});
              const [s, m] = i === 1 ? [pair.b, pair.a] : [pair.a, pair.b];
              setNotes(combinedNotes(s.notes, m.notes));
            }}
            label={t('dialog.keep', { name: personName(p) })}
            hint={p.accountEmail ? t('dialog.hasAccount', { email: p.accountEmail }) : undefined}
          />
        ))}
      </fieldset>

      {differing.length > 0 && (
        <div className="space-y-3">
          <p className="font-medium">{t('dialog.fields')}</p>
          {differing.map((f) => (
            <fieldset key={f} className="space-y-1.5">
              <legend className="text-sm text-muted-foreground">{t(`dialog.field.${f}`)}</legend>
              <div className="flex flex-wrap gap-x-6 gap-y-1">
                {[survivor, merged].map((p, i) => (
                  <Radio
                    key={p.id}
                    name={`field-${f}`}
                    checked={take[f] === (i === 1)}
                    onChange={() => setFromMerged((cur) => ({ ...cur, [f]: i === 1 }))}
                    label={display(p, f) || t('dialog.empty')}
                  />
                ))}
              </div>
            </fieldset>
          ))}
        </div>
      )}

      {hasNotes && (
        <TextareaField
          label={t('dialog.notesLabel')}
          hint={t('dialog.notesHint')}
          error={notesError}
          value={notes}
          onChange={(e) => {
            setNotes(e.target.value);
            merge.reset();
          }}
          rows={4}
        />
      )}

      <section aria-label={t('dialog.preview')} className="space-y-2 rounded-lg border p-3">
        <h3 className="font-medium">{t('dialog.preview')}</h3>
        {preview.isError ? (
          notesError ? (
            <p className="text-sm text-muted-foreground">{t('dialog.fixNotes')}</p>
          ) : (
            <ProblemAlert
              error={preview.error}
              conflicts={conflicts}
              messages={{ 404: t('gone') }}
            />
          )
        ) : preview.data === undefined ? (
          <Skeleton className="h-16 w-full" />
        ) : (
          <Summary summary={preview.data} />
        )}
      </section>

      <ProblemAlert
        error={otherError(merge.error)}
        conflicts={conflicts}
        messages={{ 404: t('gone') }}
      />
      <DialogFooter>
        <Button variant="outline" onClick={onDone}>
          {t('dialog.cancel')}
        </Button>
        <Button
          disabled={merge.isPending || preview.data === undefined}
          onClick={() =>
            merge.mutate(
              { ...request, ...(hasNotes && { notes }), apply: true },
              {
                onSuccess: () => {
                  toast.success(
                    t('dialog.merged', {
                      merged: personName(merged),
                      survivor: personName(survivor),
                    }),
                  );
                  onDone();
                },
              },
            )
          }
        >
          <Merge aria-hidden="true" />
          {merge.isPending ? t('dialog.merging') : t('dialog.confirm')}
        </Button>
      </DialogFooter>
    </div>
  );
}

function Radio({
  name,
  checked,
  onChange,
  label,
  hint,
}: {
  name: string;
  checked: boolean;
  onChange: () => void;
  label: string;
  hint?: string;
}) {
  const id = useId();
  return (
    <div className="flex items-start gap-2">
      <input
        id={id}
        type="radio"
        name={name}
        checked={checked}
        onChange={onChange}
        className="mt-1 size-4 accent-primary"
      />
      <label htmlFor={id} className="text-sm">
        <span className="break-words">{label}</span>
        {hint && <span className="block text-xs text-muted-foreground">{hint}</span>}
      </label>
    </div>
  );
}

/** What the merge does, in sentences; what goes away first. */
function Summary({ summary }: { summary: MergeSummary }) {
  const { t, i18n } = useTranslation('duplicates');
  const lang = i18n.resolvedLanguage ?? 'en';
  const timeZone = useQuery(publicSettingsQuery).data?.timezone;
  const lines = [
    summary.bookingsMoved > 0 && t('summary.bookings', { count: summary.bookingsMoved }),
    summary.history > 0 && t('summary.history', { count: summary.history }),
    summary.waitlistMoved > 0 && t('summary.waitlist', { count: summary.waitlistMoved }),
    summary.membershipsMoved > 0 && t('summary.memberships', { count: summary.membershipsMoved }),
    summary.passesMoved + summary.paidPassesMoved > 0 &&
      t('summary.passes', { count: summary.passesMoved + summary.paidPassesMoved }),
    summary.linksMoved > 0 && t('summary.links', { count: summary.linksMoved }),
    summary.staffMoved > 0 && t('summary.staff', { count: summary.staffMoved }),
    summary.accountMoved && t('summary.account'),
    summary.receipts > 0 && t('summary.receipts', { count: summary.receipts }),
  ].filter((l): l is string => typeof l === 'string');
  const lost = [
    ...summary.canceled.map((c) =>
      t(c.reason === 'membership' ? 'summary.canceledByMembership' : 'summary.canceled', {
        lesson: c.courseTypeName,
        at: timeZone ? formatTimestamp(c.startsAt, timeZone, lang) : '',
      }),
    ),
    ...summary.membershipsDropped.map((c) =>
      t('summary.dropped', {
        class: c.courseTypeName,
        weekday: weekdayName(c.weekday, lang),
        time: c.startTime,
      }),
    ),
    summary.waitlistWithdrawn > 0 && t('summary.withdrawn', { count: summary.waitlistWithdrawn }),
    summary.linksDropped > 0 && t('summary.linksDropped', { count: summary.linksDropped }),
  ].filter((l): l is string => typeof l === 'string');

  const s = summary.survivor;
  const fields: [string, string | undefined][] = [
    [t('fields.email'), s.email],
    [t('fields.phone'), s.phone],
    [
      t('fields.birthDate'),
      s.birthDate &&
        formatDate(s.birthDate, lang, { day: 'numeric', month: 'long', year: 'numeric' }),
    ],
    [t('fields.memberNo'), s.memberNo],
  ];

  return (
    <div className="space-y-2 text-sm">
      <p>{t('summary.survivor', { name: personName(s) })}</p>
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5">
        {fields.map(([term, value]) => (
          <div key={term} className="contents">
            <dt className="text-muted-foreground">{term}</dt>
            <dd className="min-w-0 break-words">
              {value || <span className="text-muted-foreground">—</span>}
            </dd>
          </div>
        ))}
      </dl>
      {lines.length === 0 ? (
        <p className="text-muted-foreground">{t('summary.nothing')}</p>
      ) : (
        <ul className="list-disc space-y-0.5 pl-5">
          {lines.map((l) => (
            <li key={l}>{l}</li>
          ))}
        </ul>
      )}
      {lost.length > 0 && (
        <div className="space-y-1">
          <p className="font-medium">{t('summary.conflicts')}</p>
          <ul className="list-disc space-y-0.5 pl-5">
            {lost.map((l) => (
              <li key={l}>{l}</li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
