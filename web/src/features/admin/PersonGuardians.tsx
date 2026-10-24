import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { UserMinus, UserPlus } from 'lucide-react';
import { useEffect, useId, useMemo, useRef, useState } from 'react';
import { Controller, useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import {
  ApiError,
  addGuardianMutation,
  type PersonDetail,
  type PersonMatch,
  removeGuardianMutation,
} from '@/api/queries';
import { Button } from '@/components/ui/button';
import { invalidFields } from '@/features/auth/form';
import { FormError } from '@/features/auth/FormError';
import { conflictReason } from '@/api/conflict';
import { useAdminErrorText } from './crud/form';
import { personName } from './crud/person';
import { PersonField } from './crud/PersonField';
import { ProblemAlert } from '@/components/ProblemAlert';
import { Section } from './PersonSection';
import { guardianSchema, type GuardianValues } from './personForms';

/**
 * The guardians of a person: each a link to their page with a button that
 * asks before ending the guardianship, and a form that links another one.
 * Removing the last guardian keeps the person.
 */
export function PersonGuardians({ person }: { person: PersonDetail }) {
  const { t } = useTranslation('admin');
  const name = personName(person);
  return (
    <Section
      title={t('persons.guardians.title')}
      description={t('persons.guardians.description', { name })}
    >
      {person.guardians.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t('persons.guardians.empty', { name })}</p>
      ) : (
        <ul aria-label={t('persons.guardians.list')} className="divide-y rounded-lg border">
          {person.guardians.map((g) => (
            <GuardianItem key={g.id} person={person} guardian={g} />
          ))}
        </ul>
      )}
      <AddGuardian person={person} />
    </Section>
  );
}

/** The form that links a guardian to the person. */
function AddGuardian({ person }: { person: PersonDetail }) {
  const { t } = useTranslation('admin');
  const { t: tAuth } = useTranslation('auth');
  const errorText = useAdminErrorText();
  const queryClient = useQueryClient();
  const add = useMutation(addGuardianMutation(queryClient));
  const schema = useMemo(
    () =>
      guardianSchema(
        person.id,
        person.dependents.map((d) => d.id),
      ),
    [person.id, person.dependents],
  );
  const form = useForm<GuardianValues>({
    resolver: zodResolver(schema),
    defaultValues: { guardianId: '' },
  });
  const { errors } = form.formState;
  // Every pick of a person starts with an empty field; a new key resets it.
  const [round, setRound] = useState(0);
  const name = personName(person);

  /** The guardian a 409 refers to: the person tried, who is linked already. */
  const tried = person.guardians.find((g) => g.id === add.variables?.guardianId);

  const onSubmit = ({ guardianId }: GuardianValues) => {
    add.mutate(
      { personId: person.id, guardianId },
      {
        onSuccess: (guardian) => {
          toast.success(t('persons.guardians.added', { guardian: personName(guardian), name }));
          form.reset({ guardianId: '' });
          setRound((r) => r + 1);
        },
        onError: (error) => {
          // Gone meanwhile, or a dependent further down (a cycle).
          if (invalidFields(error, ['guardianId'] as const).length > 0) {
            form.setError('guardianId', { type: 'server', message: 'guardianInvalid' });
          }
          if (conflictReason(error) === 'duplicate') {
            form.setError('guardianId', { type: 'server', message: 'alreadyGuardian' });
          }
        },
      },
    );
  };

  return (
    <form
      noValidate
      aria-label={t('persons.guardians.addLabel')}
      className="flex flex-col gap-4 rounded-lg border bg-muted/30 p-4"
      onSubmit={form.handleSubmit(onSubmit)}
    >
      <ProblemAlert
        error={add.error}
        conflicts={{
          duplicate: tried
            ? t('persons.guardians.duplicate', { guardian: personName(tried), name })
            : t('persons.guardians.duplicateUnknown', { name }),
        }}
        messages={{ 404: t('persons.notFound'), 422: tAuth('form.checkEntries') }}
      />
      <Controller
        control={form.control}
        name="guardianId"
        render={({ field }) => (
          <PersonField
            key={round}
            label={t('persons.guardians.person')}
            value={field.value}
            onChange={field.onChange}
            onBlur={field.onBlur}
            error={errorText(errors.guardianId)}
          />
        )}
      />
      <div>
        <Button type="submit" variant="outline" disabled={add.isPending}>
          <UserPlus aria-hidden="true" />
          {add.isPending ? t('form.saving') : t('persons.guardians.add')}
        </Button>
      </div>
    </form>
  );
}

/** A guardian of the person, with a button that asks before removing the link. */
function GuardianItem({ person, guardian }: { person: PersonDetail; guardian: PersonMatch }) {
  const { t } = useTranslation('admin');
  const [asking, setAsking] = useState(false);
  const confirmId = useId();
  const removeButton = useRef<HTMLButtonElement>(null);
  const guardianName = personName(guardian);
  return (
    <li className="space-y-1 px-3 py-2.5">
      <div className="flex items-center gap-3">
        <div className="min-w-0 flex-1">
          <Link
            to="/admin/persons/$personId"
            params={{ personId: guardian.id }}
            className="font-medium break-words underline-offset-4 hover:underline focus-visible:underline"
          >
            {guardianName}
          </Link>
          {guardian.email && (
            <p className="truncate text-xs text-muted-foreground">{guardian.email}</p>
          )}
        </div>
        <Button
          ref={removeButton}
          variant="outline"
          size="sm"
          aria-label={t('persons.guardians.removeLabel', { guardian: guardianName })}
          aria-expanded={asking}
          aria-controls={asking ? confirmId : undefined}
          onClick={() => setAsking((a) => !a)}
        >
          <UserMinus aria-hidden="true" />
          <span className="hidden sm:inline">{t('persons.guardians.remove')}</span>
        </Button>
      </div>
      {asking && (
        <ConfirmRemove
          id={confirmId}
          person={person}
          guardian={guardian}
          onCancel={() => {
            setAsking(false);
            removeButton.current?.focus();
          }}
        />
      )}
    </li>
  );
}

/** Asks before ending a guardianship; the safe choice has the focus first. */
function ConfirmRemove({
  id,
  person,
  guardian,
  onCancel,
}: {
  id: string;
  person: PersonDetail;
  guardian: PersonMatch;
  onCancel: () => void;
}) {
  const { t } = useTranslation('admin');
  const questionId = useId();
  const keepButton = useRef<HTMLButtonElement>(null);
  const queryClient = useQueryClient();
  const remove = useMutation(removeGuardianMutation(queryClient));
  const names = { guardian: personName(guardian), name: personName(person) };

  useEffect(() => keepButton.current?.focus(), []);

  return (
    <div
      id={id}
      role="group"
      aria-labelledby={questionId}
      className="mt-2 space-y-3 rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm"
    >
      <p id={questionId} className="font-medium">
        {t('persons.guardians.confirm', names)}
      </p>
      {person.guardians.length === 1 && (
        <p>{t('persons.guardians.lastGuardian', { givenName: person.givenName })}</p>
      )}
      <FormError error={remove.error} />
      <div className="flex flex-wrap gap-2">
        <Button
          variant="destructive"
          size="sm"
          disabled={remove.isPending}
          onClick={() =>
            remove.mutate(
              { personId: person.id, guardianId: guardian.id },
              {
                onSuccess: () => toast.success(t('persons.guardians.removed', names)),
                // The refreshed list no longer shows the guardian, so say it here.
                onError: (error) => {
                  if (error instanceof ApiError && error.status === 404) {
                    toast.info(t('persons.guardians.notFound', names));
                  }
                },
              },
            )
          }
        >
          <UserMinus aria-hidden="true" />
          {remove.isPending ? t('persons.guardians.removing') : t('persons.guardians.remove')}
        </Button>
        <Button ref={keepButton} variant="outline" size="sm" onClick={onCancel}>
          {t('persons.guardians.keep')}
        </Button>
      </div>
    </div>
  );
}
