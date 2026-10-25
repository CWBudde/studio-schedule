import { useQuery } from '@tanstack/react-query';
import { CircleAlert, RotateCcw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { importRunsQuery, type ImportRunEntry, publicSettingsQuery } from '@/api/queries';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { personName } from '@/features/admin/crud/person';
import { formatTimestamp } from '@/lib/schoolTime';
import { useFieldLabel } from './useFieldLabel';

/** The applied imports, newest first, with who ran them and what they did. */
export function ImportHistory() {
  const { t, i18n } = useTranslation('imports');
  const lang = i18n.resolvedLanguage ?? 'en';
  const runs = useQuery(importRunsQuery);
  const settings = useQuery(publicSettingsQuery);
  const timeZone = settings.data?.timezone;

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('history.title')}</CardTitle>
        <CardDescription>{t('history.description')}</CardDescription>
      </CardHeader>
      <CardContent>
        {runs.isError || settings.isError ? (
          <Alert variant="destructive">
            <CircleAlert aria-hidden="true" />
            <AlertTitle>{t('history.loadError')}</AlertTitle>
            <AlertDescription>
              <Button
                variant="outline"
                size="sm"
                className="mt-2"
                onClick={() => {
                  if (runs.isError) void runs.refetch();
                  if (settings.isError) void settings.refetch();
                }}
              >
                <RotateCcw aria-hidden="true" />
                {t('history.retry')}
              </Button>
            </AlertDescription>
          </Alert>
        ) : runs.data === undefined || timeZone === undefined ? (
          <Skeleton className="h-24 w-full" />
        ) : runs.data.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t('history.empty')}</p>
        ) : (
          <div className="overflow-x-auto">
            <Table aria-label={t('history.title')}>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('history.at')}</TableHead>
                  <TableHead>{t('history.file')}</TableHead>
                  <TableHead>{t('history.by')}</TableHead>
                  <TableHead>{t('history.outcome')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {runs.data.map((run) => (
                  <TableRow key={run.id}>
                    <TableCell className="whitespace-nowrap">
                      <time dateTime={run.startedAt}>
                        {formatTimestamp(run.startedAt, timeZone, lang)}
                      </time>
                    </TableCell>
                    <TableCell className="break-all">{run.fileName}</TableCell>
                    <TableCell>
                      {run.by
                        ? run.by.givenName !== undefined
                          ? personName({
                              givenName: run.by.givenName,
                              familyName: run.by.familyName ?? '',
                            })
                          : (run.by.email ?? run.by.id)
                        : t('history.commandLine')}
                    </TableCell>
                    <TableCell className="text-sm">
                      <Outcome run={run} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function Outcome({ run }: { run: ImportRunEntry }) {
  const { t } = useTranslation('imports');
  const label = useFieldLabel();
  const c = run.counts;
  return (
    <div className="space-y-1">
      <p>
        {t('history.counts', {
          created: c.created,
          updated: c.updated,
          unchanged: c.unchanged,
          failed: c.failed,
        })}
      </p>
      {run.errors.length > 0 && (
        <details>
          <summary className="cursor-pointer text-muted-foreground">
            {t('history.errors', { count: run.errors.length })}
          </summary>
          <ul className="mt-1 space-y-0.5">
            {run.errors.map((e) => (
              <li key={e.line}>
                {t('history.errorLine', {
                  line: e.line,
                  text: t(`issues.${e.code}` as 'issues.required', {
                    field: e.field ? label(e.field) : '',
                  }),
                })}
              </li>
            ))}
          </ul>
        </details>
      )}
    </div>
  );
}
