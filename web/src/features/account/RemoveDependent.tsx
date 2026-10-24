import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Trash2 } from 'lucide-react';
import { useEffect, useId, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { type FamilyDependent, removeDependentMutation } from '@/api/queries';
import { Button } from '@/components/ui/button';
import { ProblemAlert } from '@/components/ProblemAlert';
import { fullName } from './personForm';

/**
 * Asks before removing a dependent from the signed-in person's family and
 * says what happens: a managed dependent (own account, staff link, or a
 * guardian of someone itself) and a child with another guardian
 * (`guardians`, their names as a list) stay registered; one without is
 * deleted, which the API refuses while it has bookings or class memberships
 * (409 `in-use`).
 */
export function RemoveDependent({
  id,
  dependent,
  guardians,
  onCancel,
}: {
  id: string;
  dependent: FamilyDependent;
  guardians: string;
  onCancel: () => void;
}) {
  const { t } = useTranslation('account');
  const questionId = useId();
  const keepButton = useRef<HTMLButtonElement>(null);
  const queryClient = useQueryClient();
  const remove = useMutation(removeDependentMutation(queryClient));
  const name = fullName(dependent);

  // The safe choice has the focus first.
  useEffect(() => keepButton.current?.focus(), []);

  return (
    <div
      id={id}
      role="group"
      aria-labelledby={questionId}
      className="mt-3 space-y-3 rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm"
    >
      <p id={questionId} className="font-medium">
        {t('dependents.confirm.question', { name })}
      </p>
      <p>
        {dependent.managed
          ? t('dependents.confirm.managed', { givenName: dependent.givenName })
          : dependent.guardians.length > 0
            ? t('dependents.confirm.unlink', { givenName: dependent.givenName, guardians })
            : t('dependents.confirm.delete', { givenName: dependent.givenName })}
      </p>
      <ProblemAlert
        error={remove.error}
        conflicts={{ 'in-use': t('dependents.confirm.inUse', { name }) }}
        messages={{ 404: t('dependents.notFound') }}
      />
      <div className="flex flex-wrap gap-2">
        <Button
          variant="destructive"
          size="sm"
          disabled={remove.isPending}
          onClick={() =>
            remove.mutate(dependent.id, {
              onSuccess: () => toast.success(t('dependents.removed', { name })),
            })
          }
        >
          <Trash2 aria-hidden="true" />
          {remove.isPending ? t('dependents.confirm.submitting') : t('dependents.confirm.submit')}
        </Button>
        <Button ref={keepButton} variant="outline" size="sm" onClick={onCancel}>
          {t('dependents.confirm.cancel')}
        </Button>
      </div>
    </div>
  );
}
