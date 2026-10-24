import { type UseQueryResult, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useParams } from '@tanstack/react-router';
import { ArrowLeft, CalendarPlus, RotateCcw, Users } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import {
  allClassesQuery,
  allStaffQuery,
  type ClassEntry,
  courseTypesQuery,
  deleteClassMutation,
  locationsQuery,
  reactivateClassMutation,
  retireClassMutation,
  roomsQuery,
  termsQuery,
} from '@/api/queries';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { type Catalogue, WeekList } from '@/features/timetable/WeekList';
import { termRange } from '@/features/timetable/timetable';
import {
  TimetableEmpty,
  TimetableError,
  TimetableLoading,
} from '@/features/timetable/TimetableStates';
import { ClassMembersSheet } from './ClassMembersSheet';
import { type ClassChoices, ClassSheet, type ClassTarget } from './ClassSheet';
import { DeleteDialog } from './crud/DeleteDialog';
import { RowActions } from './crud/RowActions';
import { useClassLabel } from './useClassLabel';

/** Refetches the queries that failed. */
function retryFailed(queries: UseQueryResult[]) {
  for (const q of queries) if (q.isError) void q.refetch();
}

/** The page of the route: the classes of the term in the URL, afresh for every term. */
export function ClassesRoute() {
  const { termId } = useParams({ from: '/admin/terms/$termId/classes' });
  return <ClassesPage key={termId} termId={termId} />;
}

/**
 * The weekly classes of one term for the office
 * (/admin/terms/$termId/classes), laid out like the public /classes: add
 * classes, edit them, delete those without history and retire (or
 * reactivate) the others, and maintain their members.
 */
export function ClassesPage({ termId }: { termId: string }) {
  const { t, i18n } = useTranslation('admin');
  const lang = i18n.resolvedLanguage ?? 'en';
  const terms = useQuery(termsQuery);
  const classes = useQuery(allClassesQuery(termId));
  const courseTypes = useQuery(courseTypesQuery);
  const rooms = useQuery(roomsQuery);
  const locations = useQuery(locationsQuery);
  const staff = useQuery(allStaffQuery);
  const [sheet, setSheet] = useState<ClassTarget | null>(null);
  const [deleting, setDeleting] = useState<ClassEntry | null>(null);
  const [members, setMembers] = useState<ClassEntry | null>(null);
  const queries = [terms, classes, courseTypes, rooms, locations, staff];

  const term = terms.data?.find((x) => x.id === termId);
  const back = (
    <Link
      to="/admin/terms"
      className="inline-flex items-center gap-1 text-sm font-medium text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
    >
      <ArrowLeft className="size-4" aria-hidden="true" />
      {t('classes.allTerms')}
    </Link>
  );

  if (terms.data && !term) {
    return (
      <div className="space-y-4">
        {back}
        <TimetableEmpty title={t('classes.noTerm')} description={t('classes.noTermDescription')} />
      </div>
    );
  }

  const ready =
    term && classes.data && courseTypes.data && rooms.data && locations.data && staff.data;

  return (
    <div className="space-y-6">
      <div className="space-y-3">
        {back}
        <div className="flex flex-wrap items-end justify-between gap-4">
          <div className="space-y-2">
            <h1 className="font-heading text-3xl font-semibold tracking-tight">
              {term ? t('classes.title', { term: term.name }) : t('classes.titleLoading')}
            </h1>
            {term && (
              <p className="text-sm text-muted-foreground tabular-nums">
                {t('classes.range', termRange(term, lang))}
              </p>
            )}
            <p className="max-w-prose text-muted-foreground">{t('classes.description')}</p>
          </div>
          {ready && (
            <Button size="sm" onClick={() => setSheet({ mode: 'add' })}>
              <CalendarPlus aria-hidden="true" />
              {t('classes.add')}
            </Button>
          )}
        </div>
      </div>

      {queries.some((q) => q.isError) ? (
        <TimetableError onRetry={() => retryFailed(queries)} />
      ) : !ready ? (
        <TimetableLoading />
      ) : (
        <TermClasses
          termId={termId}
          classes={classes.data}
          choices={{
            courseTypes: courseTypes.data,
            rooms: rooms.data,
            locations: locations.data,
            staff: staff.data,
          }}
          sheet={sheet}
          deleting={deleting}
          members={members}
          onSheet={setSheet}
          onDelete={setDeleting}
          onMembers={setMembers}
        />
      )}
    </div>
  );
}

function TermClasses({
  termId,
  classes,
  choices,
  sheet,
  deleting,
  members,
  onSheet,
  onDelete,
  onMembers,
}: {
  termId: string;
  classes: ClassEntry[];
  choices: ClassChoices;
  sheet: ClassTarget | null;
  deleting: ClassEntry | null;
  /** The class whose members are shown. */
  members: ClassEntry | null;
  onSheet: (target: ClassTarget | null) => void;
  onDelete: (entry: ClassEntry | null) => void;
  onMembers: (entry: ClassEntry | null) => void;
}) {
  const { t } = useTranslation('admin');
  const classLabel = useClassLabel(choices.courseTypes);
  const byId = useMemo(() => new Map(classes.map((c) => [c.id, c])), [classes]);
  const catalogue: Catalogue = useMemo(
    () => ({
      courseTypes: new Map(choices.courseTypes.map((x) => [x.id, x.name])),
      rooms: new Map(choices.rooms.map((x) => [x.id, x])),
      locations: new Map(choices.locations.map((x) => [x.id, x.name])),
    }),
    [choices],
  );

  return (
    <>
      {classes.length === 0 ? (
        <TimetableEmpty
          title={t('classes.empty.title')}
          description={t('classes.empty.description')}
        />
      ) : (
        <WeekList
          classes={classes}
          catalogue={catalogue}
          badges={(c) =>
            byId.get(c.id)?.retired && <Badge variant="outline">{t('classes.retired')}</Badge>
          }
          actions={(c) => {
            const entry = byId.get(c.id);
            if (!entry) return null;
            const name = classLabel(entry);
            return (
              <>
                {entry.retired && <ReactivateButton entry={entry} name={name} />}
                <Button
                  variant="outline"
                  size="sm"
                  aria-label={t('members.label', { name })}
                  onClick={() => onMembers(entry)}
                >
                  <Users aria-hidden="true" />
                  <span className="hidden md:inline">{t('members.open')}</span>
                </Button>
                <RowActions
                  editLabel={t('classes.editLabel', { name })}
                  deleteLabel={t('classes.deleteLabel', { name })}
                  onEdit={() => onSheet({ mode: 'edit', entry })}
                  onDelete={() => onDelete(entry)}
                />
              </>
            );
          }}
        />
      )}

      <ClassSheet termId={termId} target={sheet} choices={choices} onClose={() => onSheet(null)} />
      <ClassMembersSheet target={members} name={classLabel} onClose={() => onMembers(null)} />
      <DeleteDialog
        target={deleting}
        id={(c) => c.id}
        mutation={deleteClassMutation}
        texts={(c) => {
          const name = classLabel(c);
          return {
            title: t('classes.deleteTitle', { name }),
            description: c.retired
              ? t('classes.deleteDescriptionRetired')
              : t('classes.deleteDescription'),
            inUse: c.retired ? t('classes.inUseRetired', { name }) : t('classes.inUse', { name }),
            notFound: t('classes.notFound'),
            deleted: t('classes.deleted', { name }),
          };
        }}
        inUseAction={(c) =>
          c.retired
            ? undefined
            : {
                label: t('classes.retire'),
                pending: t('classes.retiring'),
                done: t('classes.retiredDone', { name: classLabel(c) }),
                mutation: retireClassMutation,
              }
        }
        onClose={() => onDelete(null)}
      />
    </>
  );
}

/** Makes a retired class generate lessons again and shows it in the public timetable. */
function ReactivateButton({ entry, name }: { entry: ClassEntry; name: string }) {
  const { t } = useTranslation('admin');
  const queryClient = useQueryClient();
  const reactivate = useMutation(reactivateClassMutation(queryClient));
  return (
    <Button
      variant="outline"
      size="sm"
      aria-label={t('classes.reactivateLabel', { name })}
      disabled={reactivate.isPending}
      onClick={() =>
        reactivate.mutate(entry, {
          onSuccess: () => toast.success(t('classes.reactivated', { name })),
          onError: () => toast.error(t('classes.reactivateFailed', { name })),
        })
      }
    >
      <RotateCcw aria-hidden="true" />
      <span className="hidden md:inline">{t('classes.reactivate')}</span>
    </Button>
  );
}
