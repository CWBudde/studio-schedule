import { useTranslation } from 'react-i18next';
import type { ImportResult, ImportRow } from '@/api/queries';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { personName } from '@/features/admin/crud/person';
import { useFieldLabel } from './useFieldLabel';

const actionVariant = {
  create: 'default',
  update: 'secondary',
  unchanged: 'outline',
  error: 'destructive',
} as const;

/** What a dry run would do, or an import did, row by row. */
export function ImportResults({ result }: { result: ImportResult }) {
  const { t } = useTranslation('imports');
  const c = result.counts;
  const title = result.applied ? t('result.appliedTitle') : t('result.dryTitle');
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>
          {result.applied ? t('result.appliedDescription') : t('result.dryDescription')}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <ul aria-label={t('result.counts')} className="flex flex-wrap gap-2 text-sm">
          {(
            [
              ['created', c.created],
              ['updated', c.updated],
              ['unchanged', c.unchanged],
              ['failed', c.failed],
              ['guardiansCreated', c.guardiansCreated],
              ['linksAdded', c.linksAdded],
            ] as const
          ).map(([key, count]) => (
            <li key={key} className="rounded-md border px-2.5 py-1">
              {t(`result.count.${key}`, { count })}
            </li>
          ))}
        </ul>
        <div className="overflow-x-auto">
          <Table aria-label={title}>
            <TableHeader>
              <TableRow>
                <TableHead>{t('result.line')}</TableHead>
                <TableHead>{t('result.memberNo')}</TableHead>
                <TableHead>{t('result.name')}</TableHead>
                <TableHead>{t('result.action')}</TableHead>
                <TableHead>{t('result.details')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {result.rows.map((row) => (
                <TableRow key={row.line}>
                  <TableCell>{row.line}</TableCell>
                  <TableCell className="whitespace-nowrap">{row.memberNo}</TableCell>
                  <TableCell>{personName(row)}</TableCell>
                  <TableCell>
                    <Badge variant={actionVariant[row.action]}>
                      {t(`result.actions.${row.action}`)}
                    </Badge>
                  </TableCell>
                  <TableCell className="min-w-64 text-sm">
                    <RowDetails row={row} />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </CardContent>
    </Card>
  );
}

function RowDetails({ row }: { row: ImportRow }) {
  const { t } = useTranslation('imports');
  const label = useFieldLabel();
  const lines: string[] = [];

  if (row.issues.length > 0) {
    for (const issue of row.issues) {
      lines.push(t(`issues.${issue.code}`, { field: label(issue.field) }));
    }
  } else if (row.changes.length > 0) {
    lines.push(t('result.changes', { fields: row.changes.map(label).join(', ') }));
  }

  for (const g of row.guardians) {
    const how =
      g.action === 'create'
        ? t('result.guardian.new')
        : g.matchedBy
          ? t(`result.guardian.matched.${g.matchedBy}`)
          : '';
    const parts = [
      how,
      g.action === 'update' ? t('result.guardian.updated') : '',
      g.linked ? t('result.guardian.linked') : '',
    ];
    lines.push(
      t('result.guardian.line', {
        name: personName(g),
        what: parts.filter(Boolean).join(', '),
      }),
    );
  }

  if (row.duplicates.length > 0) {
    lines.push(t('result.duplicates', { names: row.duplicates.map(personName).join(', ') }));
  }

  if (lines.length === 0) return <span className="text-muted-foreground">—</span>;
  return (
    <ul className="space-y-0.5">
      {lines.map((line) => (
        <li key={line} className={row.action === 'error' ? 'text-destructive' : undefined}>
          {line}
        </li>
      ))}
    </ul>
  );
}
