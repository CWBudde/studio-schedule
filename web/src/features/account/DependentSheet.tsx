import { zodResolver } from '@hookform/resolvers/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Save, UserRoundPlus, XIcon } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { addDependentMutation, type FamilyDependent, updateDependentMutation } from '@/api/queries';
import { Button } from '@/components/ui/button';
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet';
import { FormError } from '@/features/auth/FormError';
import { PersonFields } from './PersonFields';
import {
  fullName,
  personData,
  personSchema,
  type PersonValues,
  personValues,
  showPersonErrors,
  useSchoolToday,
} from './personForm';

/** What the sheet edits: a new child (starting with the guardian's family name) or one of them. */
export type DependentSheetTarget =
  { mode: 'add'; familyName: string } | { mode: 'edit'; dependent: FamilyDependent };

/** A sheet with the form of a new or an existing dependent; closed while `target` is null. */
export function DependentSheet({
  target,
  onClose,
}: {
  target: DependentSheetTarget | null;
  onClose: () => void;
}) {
  const { t } = useTranslation('account');

  // Keep showing the last form while the sheet animates out.
  const [shown, setShown] = useState(target);
  if (target !== null && target !== shown) setShown(target);

  return (
    <Sheet
      open={target !== null}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <SheetContent
        side="right"
        showCloseButton={false}
        className="w-full overflow-y-auto data-[side=right]:w-full data-[side=right]:sm:max-w-md"
      >
        {shown && (
          <DependentForm
            key={shown.mode === 'edit' ? shown.dependent.id : 'add'}
            target={shown}
            onDone={onClose}
          />
        )}
        <SheetClose asChild>
          <Button variant="ghost" size="icon-sm" className="absolute top-3 right-3">
            <XIcon aria-hidden="true" />
            <span className="sr-only">{t('dependents.sheet.close')}</span>
          </Button>
        </SheetClose>
      </SheetContent>
    </Sheet>
  );
}

function DependentForm({ target, onDone }: { target: DependentSheetTarget; onDone: () => void }) {
  const { t } = useTranslation('account');
  const { t: tAuth } = useTranslation('auth');
  const queryClient = useQueryClient();
  const add = useMutation(addDependentMutation(queryClient));
  const update = useMutation(updateDependentMutation(queryClient));
  const editing = target.mode === 'edit' ? target.dependent : undefined;
  const save = editing ? update : add;
  const today = useSchoolToday();
  const schema = useMemo(() => personSchema(today), [today]);
  const form = useForm<PersonValues>({
    resolver: zodResolver(schema),
    defaultValues: personValues(
      editing ?? { familyName: target.mode === 'add' ? target.familyName : '' },
    ),
  });

  const onSubmit = (values: PersonValues) => {
    const body = personData(values);
    const done = (message: 'dependents.added' | 'dependents.updated') => () => {
      toast.success(t(message, { name: fullName(values) }));
      onDone();
    };
    const onError = (error: Error) => showPersonErrors(error, form);
    if (editing) {
      update.mutate(
        { personId: editing.id, body },
        { onSuccess: done('dependents.updated'), onError },
      );
    } else {
      add.mutate(body, { onSuccess: done('dependents.added'), onError });
    }
  };

  return (
    <>
      <SheetHeader className="pr-12">
        <SheetTitle className="text-lg">
          {editing
            ? t('dependents.sheet.editTitle', { name: fullName(editing) })
            : t('dependents.sheet.addTitle')}
        </SheetTitle>
        <SheetDescription>{t('dependents.sheet.description')}</SheetDescription>
      </SheetHeader>
      <form
        noValidate
        className="flex flex-col gap-4 px-4 pb-4"
        onSubmit={form.handleSubmit(onSubmit)}
      >
        <FormError
          error={save.error}
          messages={{ 404: t('dependents.notFound'), 422: tAuth('form.checkEntries') }}
        />
        <PersonFields form={form} today={today} self={false} />
        <div className="flex flex-wrap gap-2 pt-2">
          <Button type="submit" disabled={save.isPending}>
            {editing ? <Save aria-hidden="true" /> : <UserRoundPlus aria-hidden="true" />}
            {save.isPending
              ? t('dependents.sheet.saving')
              : editing
                ? t('dependents.sheet.save')
                : t('dependents.sheet.add')}
          </Button>
          <SheetClose asChild>
            <Button type="button" variant="outline">
              {t('dependents.sheet.cancel')}
            </Button>
          </SheetClose>
        </div>
      </form>
    </>
  );
}
