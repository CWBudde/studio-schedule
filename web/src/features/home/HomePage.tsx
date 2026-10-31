import { useQuery } from '@tanstack/react-query';
import { healthQuery, publicSettingsQuery } from '@/api/queries';
import { useMe } from '@/api/useMe';
import { AnnouncementList } from '@/features/announcements/AnnouncementList';
import { useSlot } from '@/themes/useSlot';
import { NextLessons } from './NextLessons';
import { useMyLessons } from './useMyLessons';

/**
 * The home page: the school, its announcements, the next lessons, the
 * visitor's own lessons (for themes that show them) and the system status.
 */
export function HomePage() {
  const Home = useSlot('Home');
  const health = useQuery(healthQuery);
  const settings = useQuery(publicSettingsQuery);
  // Only the office and admins learn the version, for support (ASVS 14.3.3).
  const version = useMe().data?.version;
  const myLessons = useMyLessons();

  return (
    // The slot changes only with the theme, and then remounting what it renders is intended.
    // eslint-disable-next-line react-hooks/static-components
    <Home
      schoolName={settings.data?.schoolName}
      announcements={<AnnouncementList />}
      renderNextLessons={(className) => <NextLessons className={className} />}
      status={{
        health: health.isPending ? 'pending' : health.data?.status === 'ok' ? 'ok' : 'error',
        version,
        timezone: settings.data?.timezone,
        settingsPending: settings.isPending,
        failed: health.isError || settings.isError,
        onRetry: () => {
          void health.refetch();
          void settings.refetch();
        },
      }}
      myLessons={myLessons}
    />
  );
}
