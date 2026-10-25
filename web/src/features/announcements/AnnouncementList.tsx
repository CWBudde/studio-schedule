import { useQuery } from '@tanstack/react-query';
import { CircleAlert, RotateCcw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { myAnnouncementsQuery } from '@/api/queries';
import { useMe } from '@/api/useMe';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { AnnouncementText } from './AnnouncementBanner';

/**
 * The announcements shown to the signed-in account, newest first, the
 * dismissed ones included (ADR 0014): the home page's permanent list.
 * Nothing for visitors.
 */
export function AnnouncementList({ className }: { className?: string }) {
  const { t } = useTranslation('announcements');
  const me = useMe();
  const signedIn = Boolean(me.data);
  const announcements = useQuery({ ...myAnnouncementsQuery, enabled: signedIn });
  if (!signedIn) return null;

  return (
    <Card role="region" aria-labelledby="announcements-title" className={className}>
      <CardHeader>
        <CardTitle>
          <h2 id="announcements-title" className="text-xl">
            {t('home.title')}
          </h2>
        </CardTitle>
        <CardDescription>{t('home.description')}</CardDescription>
      </CardHeader>
      <CardContent>
        {announcements.isPending ? (
          <Skeleton className="h-12 w-full" />
        ) : announcements.isError ? (
          <Alert variant="destructive">
            <CircleAlert aria-hidden="true" />
            <AlertTitle>{t('home.loadError')}</AlertTitle>
            <AlertDescription>
              <Button
                variant="outline"
                size="sm"
                className="mt-2"
                onClick={() => void announcements.refetch()}
              >
                <RotateCcw aria-hidden="true" />
                {t('home.retry')}
              </Button>
            </AlertDescription>
          </Alert>
        ) : announcements.data.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t('home.empty')}</p>
        ) : (
          <ul className="divide-y text-sm">
            {announcements.data.map((a) => (
              <li key={a.id} className="flex items-start gap-3 py-3 first:pt-0 last:pb-0">
                <AnnouncementText announcement={a} className="min-w-0 flex-1" />
                {a.dismissed && (
                  <Badge variant="outline" className="shrink-0">
                    {t('home.dismissed')}
                  </Badge>
                )}
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
