import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { CircleAlert, Merge, RotateCcw, X } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import {
  type DuplicateCandidate,
  type DuplicatePair,
  dismissDuplicateMutation,
  duplicatesQuery,
} from '@/api/queries';
import { ProblemAlert } from '@/components/ProblemAlert';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { personName } from '@/features/admin/crud/person';
import { formatDate } from '@/lib/schoolTime';
import { MergeDialog } from './MergeDialog';

/**
 * The possible duplicates among the persons (/admin/duplicates), for the
 * office (ADR 0015): pairs by the same email, or the same name and birth
 * date (strong), or the same name with a birth date unknown (weak). A pair
 * is merged in a dialog, or dismissed as not the same.
 */
export function DuplicatesPage() {
  const { t } = useTranslation('duplicates');
  const pairs = useQuery(duplicatesQuery);
  const [merging, setMerging] = useState<DuplicatePair | null>(null);

  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <h1 className="font-heading text-3xl font-semibold tracking-tight">{t('title')}</h1>
        <p className="max-w-prose text-muted-foreground">{t('description')}</p>
      </div>
      {pairs.isError ? (
        <Alert variant="destructive">
          <CircleAlert aria-hidden="true" />
          <AlertTitle>{t('loadError')}</AlertTitle>
          <AlertDescription>
            <Button
              variant="outline"
              size="sm"
              className="mt-2"
              onClick={() => void pairs.refetch()}
            >
              <RotateCcw aria-hidden="true" />
              {t('retry')}
            </Button>
          </AlertDescription>
        </Alert>
      ) : pairs.data === undefined ? (
        <Skeleton className="h-40 w-full" />
      ) : pairs.data.length === 0 ? (
        <p className="rounded-xl border border-dashed px-4 py-10 text-center text-muted-foreground">
          {t('empty')}
        </p>
      ) : (
        <ul aria-label={t('list')} className="space-y-4">
          {pairs.data.map((pair) => (
            <PairCard
              key={`${pair.a.id}/${pair.b.id}`}
              pair={pair}
              onMerge={() => setMerging(pair)}
            />
          ))}
        </ul>
      )}
      <MergeDialog pair={merging} onClose={() => setMerging(null)} />
    </div>
  );
}

function PairCard({ pair, onMerge }: { pair: DuplicatePair; onMerge: () => void }) {
  const { t } = useTranslation('duplicates');
  const queryClient = useQueryClient();
  const dismiss = useMutation(dismissDuplicateMutation(queryClient));
  const names = `${personName(pair.a)} / ${personName(pair.b)}`;

  return (
    <li
      aria-label={t('pair', { names })}
      className="space-y-3 rounded-xl border bg-card p-4 text-card-foreground"
    >
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant={pair.strength === 'strong' ? 'default' : 'outline'}>
          {t(`strength.${pair.strength}`)}
        </Badge>
        <span className="text-sm text-muted-foreground">
          {pair.reasons.map((r) => t(`reasons.${r}`)).join(' · ')}
        </span>
      </div>
      <div className="grid gap-3 sm:grid-cols-2">
        <Candidate person={pair.a} />
        <Candidate person={pair.b} />
      </div>
      <ProblemAlert error={dismiss.error} messages={{ 404: t('gone') }} />
      <div className="flex flex-wrap gap-2">
        <Button size="sm" aria-label={t('mergeLabel', { names })} onClick={onMerge}>
          <Merge aria-hidden="true" />
          {t('merge')}
        </Button>
        <Button
          variant="outline"
          size="sm"
          aria-label={t('dismissLabel', { names })}
          disabled={dismiss.isPending}
          onClick={() =>
            dismiss.mutate(
              { personA: pair.a.id, personB: pair.b.id },
              { onSuccess: () => toast.success(t('dismissed', { names })) },
            )
          }
        >
          <X aria-hidden="true" />
          {t('dismiss')}
        </Button>
      </div>
    </li>
  );
}

/** A person of a pair, with what tells them apart. */
export function Candidate({ person }: { person: DuplicateCandidate }) {
  const { t, i18n } = useTranslation('duplicates');
  const lang = i18n.resolvedLanguage ?? 'en';
  const rows: [string, string | undefined][] = [
    [t('fields.email'), person.email],
    [t('fields.phone'), person.phone],
    [
      t('fields.birthDate'),
      person.birthDate &&
        formatDate(person.birthDate, lang, { day: 'numeric', month: 'long', year: 'numeric' }),
    ],
    [t('fields.memberNo'), person.memberNo],
    [t('fields.account'), person.accountEmail],
  ];
  return (
    <div className="space-y-1.5 rounded-lg bg-muted/40 p-3">
      <Link
        to="/admin/persons/$personId"
        params={{ personId: person.id }}
        className="font-medium underline-offset-4 hover:underline focus-visible:underline"
      >
        {personName(person)}
      </Link>
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-sm">
        {rows.map(([term, value]) => (
          <div key={term} className="contents">
            <dt className="text-muted-foreground">{term}</dt>
            <dd className="min-w-0 break-words">
              {value || <span className="text-muted-foreground">—</span>}
            </dd>
          </div>
        ))}
      </dl>
    </div>
  );
}
