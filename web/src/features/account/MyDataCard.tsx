import { useQuery } from '@tanstack/react-query';
import { CircleAlert, Download } from 'lucide-react';
import { useId } from 'react';
import { useTranslation } from 'react-i18next';
import { exportMyDataMutation, familyQuery } from '@/api/queries';
import { Alert, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { FormError } from '@/features/auth/FormError';
import { useExportDownload } from '@/features/privacy/download';
import { fullName } from './personForm';

/**
 * The data export (ADR 0016): everything the school stores about the
 * signed-in person, or about one of their children, as a JSON file.
 */
export function MyDataCard() {
  const { t } = useTranslation('account');
  const titleId = useId();
  const family = useQuery(familyQuery);
  const exportData = useExportDownload(exportMyDataMutation);

  const people = family.data ? [family.data.me, ...family.data.dependents] : [];

  return (
    <Card role="region" aria-labelledby={titleId}>
      <CardHeader>
        <CardTitle>
          <h2 id={titleId} className="font-heading text-lg font-semibold">
            {t('myData.title')}
          </h2>
        </CardTitle>
        <CardDescription>{t('myData.description')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        <FormError error={exportData.error} messages={{ 404: t('myData.notFound') }} />
        {family.isPending ? (
          <div role="status">
            <span className="sr-only">{t('dependents.loading')}</span>
            <Skeleton className="h-12 w-full" aria-hidden="true" />
          </div>
        ) : family.isError ? (
          <Alert variant="destructive">
            <CircleAlert aria-hidden="true" />
            <AlertTitle>{t('dependents.loadFailed')}</AlertTitle>
          </Alert>
        ) : (
          <ul className="divide-y rounded-lg border">
            {people.map((person, i) => {
              const name = fullName(person);
              const pending = exportData.pendingFor === person.id;
              return (
                <li
                  key={person.id}
                  className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2 px-3 py-2.5"
                >
                  <span className="min-w-0 font-medium break-words">
                    {i === 0 ? t('myData.me', { name }) : name}
                  </span>
                  <Button
                    variant="outline"
                    size="sm"
                    aria-label={t('myData.downloadLabel', { name })}
                    disabled={exportData.pendingFor !== undefined}
                    onClick={() => exportData.download(person.id)}
                  >
                    <Download aria-hidden="true" />
                    {pending ? t('myData.downloading') : t('myData.download')}
                  </Button>
                </li>
              );
            })}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
