import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { CircleAlert, TriangleAlert } from 'lucide-react';
import { useId, useState } from 'react';
import { Controller, useForm, useWatch } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { conflictReason } from '@/api/conflict';
import { addLessonParticipantMutation, refusedAt, type StaffPersonMatch } from '@/api/queries';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { SwitchField, TextareaField } from '@/features/admin/crud/fields';
import { useAdminErrorText } from '@/features/admin/crud/form';
import { TextField } from '@/features/auth/TextField';
import { useParticipantErrorText } from './participantErrors';
import {
  addParticipantData,
  addParticipantLocations,
  addParticipantSchema,
  type AddParticipantValues,
  emptyParticipant,
} from './participantForms';
import { StaffPersonField } from './StaffPersonField';

/**
 * Adds a person of the school (found by the staff's person search) or a
 * guest (a name, an optional email) to a lesson, optionally as a trial with
 * a note. A full lesson asks whether to book beyond its capacity and, if so,
 * sends the form as it is with `confirmOverCapacity`. Any change to the form
 * clears the answer of the last attempt.
 */
export function AddParticipantDialog({
  open,
  lessonId,
  label,
  onClose,
}: {
  open: boolean;
  lessonId: string;
  /** The lesson in words, e.g. "Ballet, Monday, October 26, 18:00–19:30". */
  label: string;
  onClose: () => void;
}) {
  const { t } = useTranslation('schedule');
  const errorText = useAdminErrorText();
  const participantError = useParticipantErrorText();
  const queryClient = useQueryClient();
  const add = useMutation(addLessonParticipantMutation(queryClient));
  const formId = useId();
  const [person, setPerson] = useState<StaffPersonMatch>();
  // Who the last attempt was for, to name them in its refusal.
  const [sentName, setSentName] = useState('');
  const form = useForm<AddParticipantValues>({
    resolver: zodResolver(addParticipantSchema),
    defaultValues: emptyParticipant,
  });
  const { errors } = form.formState;
  const trial = useWatch({ control: form.control, name: 'trial' });

  const nameOf = (v: AddParticipantValues) =>
    v.mode === 'person' ? (person?.name ?? '') : v.guestName;

  /** The form changed: the answer of the last attempt no longer applies. */
  const changed = () => {
    if (add.error) add.reset();
  };

  const send = (values: AddParticipantValues, confirmOverCapacity: boolean) => {
    const name = nameOf(values);
    setSentName(name);
    add.mutate(
      { lessonId, body: addParticipantData(values, confirmOverCapacity) },
      {
        onSuccess: () => {
          toast.success(t('participants.add.added', { name }));
          onClose();
        },
        onError: (error) => {
          for (const [field, location] of Object.entries(addParticipantLocations)) {
            if (refusedAt(error, location)) {
              form.setError(field as keyof typeof addParticipantLocations, {
                type: 'server',
                // An unknown person is one that is gone since the search.
                message: field === 'personId' ? 'unknownPerson' : 'invalid',
              });
            }
          }
        },
      },
    );
  };

  const full = conflictReason(add.error) === 'full';
  const personError =
    errors.personId?.message === 'unknownPerson'
      ? t('participants.errors.unknownPerson')
      : errorText(errors.personId);
  const fieldRefused = Object.values(addParticipantLocations).some((l) => refusedAt(add.error, l));

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next && !add.isPending) onClose();
      }}
    >
      <DialogContent showCloseButton={false} className="max-h-[90dvh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{t('participants.add.title')}</DialogTitle>
          <DialogDescription>
            {t('participants.add.description', { lesson: label })}
          </DialogDescription>
        </DialogHeader>
        <form
          id={formId}
          noValidate
          className="grid gap-4"
          onSubmit={form.handleSubmit((values) => send(values, false))}
        >
          <Controller
            control={form.control}
            name="mode"
            render={({ field }) => (
              <Tabs
                value={field.value}
                onValueChange={(next) => {
                  field.onChange(next);
                  changed();
                }}
              >
                <TabsList className="w-full">
                  <TabsTrigger value="person">{t('participants.add.person')}</TabsTrigger>
                  <TabsTrigger value="guest">{t('participants.add.guest')}</TabsTrigger>
                </TabsList>
                <TabsContent value="person" className="pt-2">
                  <Controller
                    control={form.control}
                    name="personId"
                    render={({ field: personField }) => (
                      <StaffPersonField
                        label={t('participants.add.personLabel')}
                        person={person}
                        onChange={(p) => {
                          setPerson(p);
                          personField.onChange(p.id);
                          changed();
                        }}
                        onBlur={personField.onBlur}
                        error={personError}
                      />
                    )}
                  />
                </TabsContent>
                <TabsContent value="guest" className="grid gap-4 pt-2">
                  <TextField
                    label={t('participants.add.guestName')}
                    autoComplete="off"
                    error={errorText(errors.guestName)}
                    {...form.register('guestName', { onChange: changed })}
                  />
                  <TextField
                    label={t('participants.add.guestEmail')}
                    type="email"
                    autoComplete="off"
                    hint={t('participants.add.guestEmailHint')}
                    error={errorText(errors.guestEmail)}
                    {...form.register('guestEmail', { onChange: changed })}
                  />
                </TabsContent>
              </Tabs>
            )}
          />
          <Controller
            control={form.control}
            name="trial"
            render={({ field }) => (
              <SwitchField
                label={t('participants.trialLabel')}
                checked={field.value}
                onChange={(checked) => {
                  field.onChange(checked);
                  changed();
                }}
                onBlur={field.onBlur}
              />
            )}
          />
          {trial && (
            <TextareaField
              label={t('participants.trialNoteLabel')}
              rows={3}
              error={errorText(errors.trialNote)}
              {...form.register('trialNote', { onChange: changed })}
            />
          )}
        </form>

        {full ? (
          <Alert>
            <TriangleAlert aria-hidden="true" />
            <AlertTitle>{t('participants.add.full')}</AlertTitle>
            <AlertDescription>
              <p>{t('participants.add.fullHint')}</p>
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="mt-2"
                disabled={add.isPending}
                onClick={() => void form.handleSubmit((values) => send(values, true))()}
              >
                {t('participants.add.override')}
              </Button>
            </AlertDescription>
          </Alert>
        ) : (
          add.error &&
          !fieldRefused && (
            <Alert variant="destructive">
              <CircleAlert aria-hidden="true" />
              <AlertTitle>{participantError(add.error, { name: sentName })}</AlertTitle>
            </Alert>
          )
        )}

        <DialogFooter>
          <Button type="button" variant="outline" disabled={add.isPending} onClick={onClose}>
            {t('participants.cancel')}
          </Button>
          <Button type="submit" form={formId} disabled={add.isPending}>
            {add.isPending ? t('participants.add.adding') : t('participants.add.submit')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
