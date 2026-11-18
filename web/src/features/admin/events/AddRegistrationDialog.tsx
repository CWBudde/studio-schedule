import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useId } from 'react';
import { Controller, useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { conflictReason } from '@/api/conflict';
import {
  type AddEventRegistrationData,
  type AdminEvent,
  addEventRegistrationMutation,
} from '@/api/queries';
import { PendingIcon } from '@/components/PendingIcon';
import { ProblemAlert } from '@/components/ProblemAlert';
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
import { TextField } from '@/features/auth/TextField';
import { showServerErrors, useAdminErrorText } from '../crud/form';
import { PersonField } from '../crud/PersonField';
import { registrationSchema, type RegistrationValues } from './eventForms';

export function AddRegistrationDialog({
  event,
  onClose,
}: {
  event: AdminEvent;
  onClose: () => void;
}) {
  const { t } = useTranslation('adminEvents');
  const { t: tAdmin } = useTranslation('admin');
  const { t: tAuth } = useTranslation('auth');
  const errorText = useAdminErrorText();
  const queryClient = useQueryClient();
  const add = useMutation(addEventRegistrationMutation(queryClient, event.id));
  const formId = useId();
  const form = useForm<RegistrationValues, unknown, AddEventRegistrationData>({
    resolver: zodResolver(registrationSchema),
    defaultValues: { mode: 'person', personId: '', guest: { name: '', email: '' } },
  });
  const { errors } = form.formState;
  const full = conflictReason(add.error) === 'full';
  const changed = () => {
    if (add.error) add.reset();
  };
  const send = (body: AddEventRegistrationData, confirmOverCapacity = false) =>
    add.mutate(confirmOverCapacity ? { ...body, confirmOverCapacity } : body, {
      onSuccess: () => {
        toast.success(t('registrations.added'));
        onClose();
      },
      onError: (error) => showServerErrors(error, form, ['personId', 'guest.name', 'guest.email']),
    });

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !add.isPending) onClose();
      }}
    >
      <DialogContent showCloseButton={false} className="max-h-[90dvh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{t('registrations.add')}</DialogTitle>
          <DialogDescription>
            {t('registrations.addDescription', { title: event.title })}
          </DialogDescription>
        </DialogHeader>
        <ProblemAlert
          error={add.error}
          conflicts={{
            closed: t('conflicts.closed'),
            duplicate: t('registrations.duplicate'),
            full: t('conflicts.full'),
          }}
          messages={{ 404: t('notFound'), 422: tAuth('form.checkEntries') }}
        />
        {full && (
          <div className="space-y-2">
            <p className="text-sm text-muted-foreground">{t('registrations.overrideHint')}</p>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={add.isPending}
              onClick={() => void form.handleSubmit((body) => send(body, true))()}
            >
              {t('registrations.override')}
            </Button>
          </div>
        )}
        <form id={formId} noValidate onSubmit={form.handleSubmit((body) => send(body))}>
          <Controller
            control={form.control}
            name="mode"
            render={({ field }) => (
              <Tabs
                value={field.value}
                onValueChange={(value) => {
                  field.onChange(value);
                  form.clearErrors();
                  changed();
                }}
              >
                <TabsList className="w-full">
                  <TabsTrigger value="person">{t('registrations.person')}</TabsTrigger>
                  <TabsTrigger value="guest">{t('registrations.guest')}</TabsTrigger>
                </TabsList>
                <TabsContent
                  value="person"
                  forceMount
                  className="pt-2 data-[state=inactive]:hidden"
                >
                  <Controller
                    control={form.control}
                    name="personId"
                    render={({ field: person }) => (
                      <PersonField
                        label={t('registrations.person')}
                        value={person.value}
                        onChange={(id) => {
                          person.onChange(id);
                          changed();
                        }}
                        onBlur={person.onBlur}
                        error={errorText(errors.personId)}
                      />
                    )}
                  />
                </TabsContent>
                <TabsContent value="guest" className="grid gap-4 pt-2">
                  <TextField
                    label={t('registrations.guestName')}
                    maxLength={200}
                    autoComplete="off"
                    error={errorText(errors.guest?.name)}
                    {...form.register('guest.name', { onChange: changed })}
                  />
                  <TextField
                    label={t('registrations.guestEmail')}
                    type="email"
                    maxLength={254}
                    autoComplete="off"
                    error={errorText(errors.guest?.email)}
                    {...form.register('guest.email', { onChange: changed })}
                  />
                </TabsContent>
              </Tabs>
            )}
          />
        </form>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose} disabled={add.isPending}>
            {tAdmin('form.cancel')}
          </Button>
          <Button type="submit" form={formId} disabled={add.isPending}>
            <PendingIcon pending={add.isPending} />
            {t('registrations.add')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
