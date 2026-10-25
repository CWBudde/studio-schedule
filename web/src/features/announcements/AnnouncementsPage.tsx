import { useQuery } from '@tanstack/react-query';
import { Plus } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  type Announcement,
  allAnnouncementsQuery,
  courseTypesQuery,
  deleteAnnouncementMutation,
  publicSettingsQuery,
} from '@/api/queries';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { DataTable } from '@/features/admin/crud/DataTable';
import { DeleteDialog } from '@/features/admin/crud/DeleteDialog';
import { RowActions } from '@/features/admin/crud/RowActions';
import { columnHelper } from '@/features/admin/crud/table';
import { useClassLabel } from '@/features/admin/useClassLabel';
import { formatDateTime, formatTimestamp } from '@/lib/schoolTime';
import { useNow } from '@/lib/useNow';
import { AnnouncementSheet, type AnnouncementTarget } from './AnnouncementSheet';
import { statusAt } from './announcementForms';

const columns = columnHelper<Announcement>();

const statusVariant = { scheduled: 'outline', shown: 'secondary', expired: 'outline' } as const;

/**
 * The school's announcements (/admin/announcements), for the office (ADR
 * 0014): every one with its audience, publication window, status and email,
 * to write, edit until it expires, and delete.
 */
export function AnnouncementsPage() {
  const { t, i18n } = useTranslation('announcements');
  const lang = i18n.resolvedLanguage ?? 'en';
  const announcements = useQuery(allAnnouncementsQuery);
  const settings = useQuery(publicSettingsQuery);
  const courseTypes = useQuery(courseTypesQuery);
  const timeZone = settings.data?.timezone;
  const classLabel = useClassLabel(courseTypes.data ?? []);
  const now = useNow();
  const [sheet, setSheet] = useState<AnnouncementTarget | null>(null);
  const [deleting, setDeleting] = useState<Announcement | null>(null);

  const tableColumns = useMemo(() => {
    if (timeZone === undefined) return [];
    const audienceText = (a: Announcement) => {
      switch (a.audience) {
        case 'role':
          return t('admin.audienceText.role', { role: a.role ? t(`admin.roles.${a.role}`) : '' });
        case 'class':
          return a.class
            ? t('admin.audienceText.class', {
                class: classLabel({
                  courseTypeId: a.class.courseType.id,
                  weekday: a.class.weekday,
                  startTime: a.class.startTime,
                }),
                term: a.class.termName,
              })
            : '';
        case 'course-type':
          return t('admin.audienceText.course-type', { name: a.courseType?.name ?? '' });
        case 'lesson':
          return a.lesson
            ? t('admin.audienceText.lesson', {
                name: a.lesson.courseType.name,
                date: formatDateTime(a.lesson.startsAt, timeZone, lang),
              })
            : '';
        default:
          return t('admin.audienceText.everyone');
      }
    };
    const emailText = (a: Announcement) => {
      if (!a.email) return t('admin.noEmail');
      // The announcements job sends it from its publication on, in batches.
      if (!a.emailedAt) {
        return statusAt(a, now) === 'scheduled'
          ? t('admin.emailScheduled')
          : t('admin.emailSending');
      }
      return a.emailRecipients === 0
        ? t('admin.emailedNobody')
        : t('admin.emailed', { count: a.emailRecipients });
    };
    return columns.columns([
      columns.accessor('title', {
        header: t('admin.titleColumn'),
        cell: (info) => (
          <>
            <span className="block font-medium break-words">{info.getValue()}</span>
            {info.row.original.author && (
              <span className="block text-xs text-muted-foreground">
                {t('admin.by', { name: info.row.original.author })}
              </span>
            )}
          </>
        ),
      }),
      columns.accessor(audienceText, { id: 'audience', header: t('admin.audience') }),
      columns.accessor((a) => formatTimestamp(a.publishAt ?? '', timeZone, lang), {
        id: 'published',
        header: t('admin.published'),
        sortFn: (a, b) =>
          Date.parse(a.original.publishAt ?? '') - Date.parse(b.original.publishAt ?? ''),
        cell: (info) => <span className="whitespace-nowrap">{info.getValue()}</span>,
      }),
      columns.accessor(
        (a) => (a.expiresAt ? formatTimestamp(a.expiresAt, timeZone, lang) : t('admin.never')),
        {
          id: 'expires',
          header: t('admin.expires'),
          cell: (info) => <span className="whitespace-nowrap">{info.getValue()}</span>,
        },
      ),
      columns.accessor((a) => t(`admin.statuses.${statusAt(a, now)}`), {
        id: 'status',
        header: t('admin.status'),
        cell: (info) => (
          <Badge variant={statusVariant[statusAt(info.row.original, now)]}>{info.getValue()}</Badge>
        ),
      }),
      columns.accessor(emailText, { id: 'email', header: t('admin.email') }),
    ]);
  }, [t, lang, timeZone, now, classLabel]);

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="space-y-2">
          <h1 className="font-heading text-3xl font-semibold tracking-tight">{t('admin.title')}</h1>
          <p className="max-w-prose text-muted-foreground">{t('admin.description')}</p>
        </div>
        <Button size="sm" disabled={!timeZone} onClick={() => setSheet({ mode: 'add' })}>
          <Plus aria-hidden="true" />
          {t('admin.add')}
        </Button>
      </div>

      <DataTable
        label={t('admin.table')}
        searchLabel={t('admin.search')}
        empty={t('admin.empty')}
        columns={tableColumns}
        data={timeZone === undefined ? undefined : announcements.data}
        getRowId={(a) => a.id}
        isError={announcements.isError || settings.isError}
        onRetry={() => {
          if (announcements.isError) void announcements.refetch();
          if (settings.isError) void settings.refetch();
        }}
        initialSorting={[{ id: 'published', desc: true }]}
        actions={(announcement) => (
          <RowActions
            editLabel={t('admin.editLabel', { title: announcement.title })}
            deleteLabel={t('admin.deleteLabel', { title: announcement.title })}
            onEdit={
              statusAt(announcement, now) === 'expired'
                ? undefined
                : () => setSheet({ mode: 'edit', announcement })
            }
            onDelete={() => setDeleting(announcement)}
          />
        )}
      />

      {timeZone && (
        <AnnouncementSheet target={sheet} timeZone={timeZone} onClose={() => setSheet(null)} />
      )}
      <DeleteDialog
        target={deleting}
        id={(a) => a.id}
        mutation={deleteAnnouncementMutation}
        texts={({ title }) => ({
          title: t('admin.deleteTitle', { title }),
          description: t('admin.deleteDescription'),
          inUse: t('admin.inUse'),
          notFound: t('admin.notFound'),
          deleted: t('admin.deleted', { title }),
        })}
        onClose={() => setDeleting(null)}
      />
    </div>
  );
}
