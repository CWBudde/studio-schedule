import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { UserMinus, UserPlus } from 'lucide-react';
import { useEffect, useId, useRef, useState } from 'react';
import { Controller, useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import {
  ApiError,
  addClassMemberMutation,
  type ClassEntry,
  type ClassMember,
  classMembersQuery,
  removeClassMemberMutation,
} from '@/api/queries';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { invalidFields } from '@/features/auth/form';
import { FormError } from '@/features/auth/FormError';
import { TextField } from '@/features/auth/TextField';
import { formatDate } from '@/lib/schoolTime';
import {
  classMemberData,
  classMemberFields,
  classMemberSchema,
  type ClassMemberValues,
  emptyClassMember,
} from './classMemberForms';
import { conflictReason, showServerErrors, useAdminErrorText } from './crud/form';
import { FormSheet, FormSheetHeader } from './crud/FormSheet';
import { LoadError } from './crud/LoadError';
import { personName } from './crud/person';
import { PersonField } from './crud/PersonField';
import { ProblemAlert } from './crud/ProblemAlert';

/**
 * A sheet with the members of a class (`target`; closed while it is null):
 * the office adds persons, with the date they joined if known, and removes
 * them. `name` names a class, as its card does.
 */
export function ClassMembersSheet({
  target,
  name,
  onClose,
}: {
  target: ClassEntry | null;
  name: (entry: ClassEntry) => string;
  onClose: () => void;
}) {
  return (
    <FormSheet target={target} onClose={onClose}>
      {(shown) => <ClassMembers key={shown.id} classId={shown.id} name={name(shown)} />}
    </FormSheet>
  );
}

function ClassMembers({ classId, name }: { classId: string; name: string }) {
  const { t } = useTranslation('admin');
  const members = useQuery(classMembersQuery(classId));
  const headingId = useId();
  return (
    <>
      <FormSheetHeader
        title={t('members.title', { name })}
        description={t('members.description')}
      />
      <div className="flex flex-col gap-6 px-4 pb-4">
        <AddMember classId={classId} members={members.data} />
        <section aria-labelledby={headingId} className="space-y-3">
          <h3 id={headingId} className="font-heading text-base font-medium">
            {members.data
              ? t('members.count', { count: members.data.length })
              : t('members.heading')}
          </h3>
          {members.data === undefined ? (
            members.isError ? (
              <LoadError onRetry={() => void members.refetch()} />
            ) : (
              <div className="space-y-2" aria-label={t('loading')} role="status">
                <Skeleton className="h-12 w-full" />
                <Skeleton className="h-12 w-full" />
              </div>
            )
          ) : members.data.length === 0 ? (
            <p className="rounded-lg border border-dashed px-4 py-6 text-center text-sm text-muted-foreground">
              {t('members.empty')}
            </p>
          ) : (
            <ul aria-label={t('members.list')} className="divide-y rounded-lg border">
              {members.data.map((member) => (
                <MemberItem
                  key={member.person.id}
                  classId={classId}
                  classLabel={name}
                  member={member}
                />
              ))}
            </ul>
          )}
        </section>
      </div>
    </>
  );
}

/** The form that adds a person to the class. */
function AddMember({ classId, members }: { classId: string; members: ClassMember[] | undefined }) {
  const { t } = useTranslation('admin');
  const { t: tAuth } = useTranslation('auth');
  const errorText = useAdminErrorText();
  const queryClient = useQueryClient();
  const add = useMutation(addClassMemberMutation(queryClient));
  const form = useForm<ClassMemberValues>({
    resolver: zodResolver(classMemberSchema),
    defaultValues: emptyClassMember,
  });
  const { errors } = form.formState;
  // Every pick of a person starts with an empty field; a new key resets it.
  const [round, setRound] = useState(0);

  /** The member a 409 refers to: the person tried, who is in the list already. */
  const tried = members?.find((m) => m.person.id === add.variables?.body.personId);

  const onSubmit = (values: ClassMemberValues) => {
    add.mutate(
      { classId, body: classMemberData(values) },
      {
        onSuccess: (added) => {
          toast.success(t('members.added', { name: personName(added.person) }));
          form.reset(emptyClassMember);
          setRound((r) => r + 1);
        },
        onError: (error) => {
          showServerErrors(error, form, classMemberFields);
          if (invalidFields(error, ['personId'] as const).length > 0) {
            form.setError('personId', { type: 'server', message: 'person' });
          }
          if (conflictReason(error) === 'duplicate') {
            form.setError('personId', { type: 'server', message: 'alreadyMember' });
          }
        },
      },
    );
  };

  return (
    <form
      noValidate
      aria-label={t('members.addLabel')}
      className="flex flex-col gap-4 rounded-lg border bg-muted/30 p-4"
      onSubmit={form.handleSubmit(onSubmit)}
    >
      <ProblemAlert
        error={add.error}
        conflicts={{
          duplicate: tried
            ? t('members.duplicate', { name: personName(tried.person) })
            : t('members.duplicateUnknown'),
        }}
        messages={{ 404: t('classes.notFound'), 422: tAuth('form.checkEntries') }}
      />
      <Controller
        control={form.control}
        name="personId"
        render={({ field }) => (
          <PersonField
            key={round}
            label={t('members.person')}
            value={field.value}
            onChange={field.onChange}
            onBlur={field.onBlur}
            error={errorText(errors.personId)}
          />
        )}
      />
      <TextField
        label={t('form.optional', { label: t('members.since') })}
        type="date"
        hint={t('members.sinceHint')}
        error={errorText(errors.since)}
        {...form.register('since')}
      />
      <div>
        <Button type="submit" disabled={add.isPending}>
          <UserPlus aria-hidden="true" />
          {add.isPending ? t('form.saving') : t('members.add')}
        </Button>
      </div>
    </form>
  );
}

/** A member of the class, with a button that asks before removing them. */
function MemberItem({
  classId,
  classLabel,
  member,
}: {
  classId: string;
  classLabel: string;
  member: ClassMember;
}) {
  const { t, i18n } = useTranslation('admin');
  const lang = i18n.resolvedLanguage ?? 'en';
  const [asking, setAsking] = useState(false);
  const confirmId = useId();
  const name = personName(member.person);
  return (
    <li className="space-y-1 px-3 py-2.5">
      <div className="flex items-center gap-3">
        <div className="min-w-0 flex-1">
          <p className="truncate font-medium">{name}</p>
          {member.person.email && (
            <p className="truncate text-xs text-muted-foreground">{member.person.email}</p>
          )}
          {member.since && (
            <p className="text-xs text-muted-foreground tabular-nums">
              {t('members.sinceDate', {
                date: formatDate(member.since, lang, {
                  day: 'numeric',
                  month: 'short',
                  year: 'numeric',
                }),
              })}
            </p>
          )}
        </div>
        <Button
          variant="outline"
          size="sm"
          aria-label={t('members.removeLabel', { name })}
          aria-expanded={asking}
          aria-controls={asking ? confirmId : undefined}
          onClick={() => setAsking((a) => !a)}
        >
          <UserMinus aria-hidden="true" />
          <span className="hidden sm:inline">{t('members.remove')}</span>
        </Button>
      </div>
      {asking && (
        <ConfirmRemove
          id={confirmId}
          classId={classId}
          classLabel={classLabel}
          member={member}
          onCancel={() => setAsking(false)}
        />
      )}
    </li>
  );
}

/** Asks before ending a membership; the safe choice has the focus first. */
function ConfirmRemove({
  id,
  classId,
  classLabel,
  member,
  onCancel,
}: {
  id: string;
  classId: string;
  classLabel: string;
  member: ClassMember;
  onCancel: () => void;
}) {
  const { t } = useTranslation('admin');
  const questionId = useId();
  const keepButton = useRef<HTMLButtonElement>(null);
  const queryClient = useQueryClient();
  const remove = useMutation(removeClassMemberMutation(queryClient));
  const name = personName(member.person);

  useEffect(() => keepButton.current?.focus(), []);

  return (
    <div
      id={id}
      role="group"
      aria-labelledby={questionId}
      className="mt-2 space-y-3 rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm"
    >
      <p id={questionId} className="font-medium">
        {t('members.confirm', { name, classLabel })}
      </p>
      <FormError error={remove.error} />
      <div className="flex flex-wrap gap-2">
        <Button
          variant="destructive"
          size="sm"
          disabled={remove.isPending}
          onClick={() =>
            remove.mutate(
              { classId, personId: member.person.id },
              {
                onSuccess: () => toast.success(t('members.removed', { name })),
                // The refreshed list no longer shows the row, so say it here.
                onError: (error) => {
                  if (error instanceof ApiError && error.status === 404) {
                    toast.info(t('members.notFound', { name }));
                  }
                },
              },
            )
          }
        >
          <UserMinus aria-hidden="true" />
          {remove.isPending ? t('members.removing') : t('members.remove')}
        </Button>
        <Button ref={keepButton} variant="outline" size="sm" onClick={onCancel}>
          {t('members.keep')}
        </Button>
      </div>
    </div>
  );
}
