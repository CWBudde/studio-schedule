import { useQuery } from '@tanstack/react-query';
import { Link, useNavigate, useSearch } from '@tanstack/react-router';
import { LogIn } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { meQuery } from '@/api/queries';
import { Button } from '@/components/ui/button';
import { LoadError } from '@/features/admin/crud/LoadError';
import { SubscribeButton } from '@/features/calendar-feeds/SubscribeDialog';
import { LessonDetailSheet } from '@/features/schedule/detail/LessonSheet';
import { HistorySection } from './HistorySection';
import { Loading } from './Loading';
import { UpcomingSections } from './UpcomingSections';
import { useNamesPersons } from './useNamesPersons';

/**
 * "My lessons" (/my-lessons): the coming lessons of the signed-in family,
 * its waiting-list entries and the lessons that are over, a page of three
 * months at a time. A lesson opens in the lesson sheet; "Subscribe" puts
 * the family's bookings in a calendar app. Visitors are asked to sign in.
 */
export function MyLessonsPage() {
  const { t } = useTranslation('myLessons');
  const me = useQuery(meQuery);
  const search = useSearch({ from: '/my-lessons' });
  const navigate = useNavigate({ from: '/my-lessons' });
  const open = (lesson: string | undefined) =>
    void navigate({ search: (prev) => ({ ...prev, lesson }), replace: lesson === undefined });

  return (
    <div className="mx-auto w-full max-w-3xl space-y-8">
      <div className="space-y-2">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h1 className="font-heading text-4xl font-medium tracking-tight">{t('title')}</h1>
          {me.data && <SubscribeButton subject={{ scope: 'bookings' }} />}
        </div>
        {me.data && <Intro />}
      </div>
      {me.isPending ? (
        <Loading />
      ) : me.isError ? (
        <LoadError message={t('loadError')} onRetry={() => void me.refetch()} />
      ) : !me.data ? (
        <SignedOut />
      ) : (
        <>
          <UpcomingSections me={me.data} onOpen={open} />
          <HistorySection onOpen={open} />
          <LessonDetailSheet lessonId={search.lesson} onClose={() => open(undefined)} />
        </>
      )}
    </div>
  );
}

function Intro() {
  const { t } = useTranslation('myLessons');
  return (
    <p className="max-w-prose text-muted-foreground">
      {useNamesPersons() ? t('introFamily') : t('intro')}
    </p>
  );
}

function SignedOut() {
  const { t } = useTranslation('myLessons');
  return (
    <div className="flex flex-col items-center gap-3 rounded-xl border border-dashed px-4 py-10 text-center">
      <LogIn className="size-8 text-muted-foreground" aria-hidden="true" />
      <p className="font-heading text-lg font-semibold">{t('signedOutTitle')}</p>
      <p className="max-w-prose text-muted-foreground">{t('signedOut')}</p>
      <Button asChild>
        <Link to="/login">{t('signIn')}</Link>
      </Button>
    </div>
  );
}
