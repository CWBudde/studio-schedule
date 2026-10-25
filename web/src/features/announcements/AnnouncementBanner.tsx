import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ChevronDown, ChevronUp, Megaphone, X } from 'lucide-react';
import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import {
  dismissAnnouncementMutation,
  type MyAnnouncement,
  myAnnouncementsQuery,
  publicSettingsQuery,
} from '@/api/queries';
import { useMe } from '@/api/useMe';
import { Button } from '@/components/ui/button';
import { formatTimestamp } from '@/lib/schoolTime';

/**
 * The announcements the signed-in account has not dismissed (ADR 0014),
 * under the header of every page: the newest one's title, and on request
 * all of them with their text, each to dismiss. A dismissed one stays in
 * the list on the home page. While an admin is signed in as someone else,
 * the API stores no dismissal; the banner hides it for the visit.
 */
export function AnnouncementBanner() {
  const me = useMe();
  const signedIn = Boolean(me.data);
  const announcements = useQuery({ ...myAnnouncementsQuery, enabled: signedIn });
  const [hidden, setHidden] = useState<ReadonlySet<string>>(new Set());

  const shown = (announcements.data ?? []).filter((a) => !a.dismissed && !hidden.has(a.id));
  if (!signedIn || shown.length === 0) return null;

  return (
    <Banner announcements={shown} onDismissed={(id) => setHidden((ids) => new Set(ids).add(id))} />
  );
}

function Banner({
  announcements,
  onDismissed,
}: {
  announcements: MyAnnouncement[];
  onDismissed: (id: string) => void;
}) {
  const { t } = useTranslation('announcements');
  const queryClient = useQueryClient();
  const dismiss = useMutation(dismissAnnouncementMutation(queryClient));
  const [open, setOpen] = useState(false);
  const listId = useId();
  const [newest] = announcements;
  if (!newest) return null;
  const more = announcements.length - 1;

  const onDismiss = (a: MyAnnouncement) =>
    dismiss.mutate(a.id, {
      onSuccess: () => onDismissed(a.id),
      onError: () => toast.error(t('banner.dismissFailed')),
    });

  return (
    <section
      aria-label={t('banner.label')}
      className="border-b border-primary/15 bg-primary/5 text-foreground"
    >
      <div className="mx-auto max-w-5xl px-4 py-2.5 text-sm">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <Megaphone className="size-4 shrink-0 text-primary" aria-hidden="true" />
          <p className="min-w-0 flex-1 basis-48 leading-snug break-words">
            {open ? (
              <span className="font-medium">{t('banner.label')}</span>
            ) : (
              <>
                <span className="font-medium">{newest.title}</span>
                {more > 0 && (
                  <span className="text-muted-foreground">
                    {' '}
                    {t('banner.more', { count: more })}
                  </span>
                )}
              </>
            )}
          </p>
          <div className="flex shrink-0 gap-1">
            <Button
              size="sm"
              variant="ghost"
              aria-expanded={open}
              aria-controls={listId}
              aria-label={open ? undefined : t('banner.showLabel', { count: announcements.length })}
              onClick={() => setOpen((o) => !o)}
            >
              {open ? <ChevronUp aria-hidden="true" /> : <ChevronDown aria-hidden="true" />}
              {open ? t('banner.hide') : t('banner.show')}
            </Button>
            {!open && (
              <DismissButton
                announcement={newest}
                pending={dismiss.isPending}
                onDismiss={onDismiss}
              />
            )}
          </div>
        </div>
        {open && (
          <ul id={listId} className="mt-2 space-y-3 border-t border-primary/10 pt-3">
            {announcements.map((a) => (
              <li key={a.id} className="flex items-start gap-3">
                <AnnouncementText announcement={a} className="min-w-0 flex-1" />
                <DismissButton announcement={a} pending={dismiss.isPending} onDismiss={onDismiss} />
              </li>
            ))}
          </ul>
        )}
      </div>
    </section>
  );
}

function DismissButton({
  announcement,
  pending,
  onDismiss,
}: {
  announcement: MyAnnouncement;
  pending: boolean;
  onDismiss: (a: MyAnnouncement) => void;
}) {
  const { t } = useTranslation('announcements');
  return (
    <Button
      size="sm"
      variant="ghost"
      disabled={pending}
      aria-label={t('banner.dismissLabel', { title: announcement.title })}
      onClick={() => onDismiss(announcement)}
    >
      <X aria-hidden="true" />
      <span className="max-sm:sr-only">{t('banner.dismiss')}</span>
    </Button>
  );
}

/** An announcement's title, date and text, with its line breaks. */
export function AnnouncementText({
  announcement,
  className,
}: {
  announcement: MyAnnouncement;
  className?: string;
}) {
  const { t, i18n } = useTranslation('announcements');
  const settings = useQuery(publicSettingsQuery);
  const timeZone = settings.data?.timezone;
  return (
    <div className={className}>
      <p className="font-medium break-words">{announcement.title}</p>
      {timeZone && (
        <p className="text-xs text-muted-foreground">
          <time dateTime={announcement.publishedAt}>
            {t('banner.published', {
              date: formatTimestamp(
                announcement.publishedAt,
                timeZone,
                i18n.resolvedLanguage ?? 'en',
              ),
            })}
          </time>
        </p>
      )}
      <p className="mt-1 break-words whitespace-pre-line text-muted-foreground">
        {announcement.body}
      </p>
    </div>
  );
}
