import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  eventRegistrationMutation,
  type EventRegistrationAction,
  type MyEvent,
} from '@/api/queries';
import { PendingIcon } from '@/components/PendingIcon';
import { Button } from '@/components/ui/button';
import { formatDateTime } from '@/lib/schoolTime';
import { CancelRegistrationDialog } from './CancelRegistrationDialog';
import { RegistrationProblem } from './RegistrationProblem';

export function RegistrationControls({ item, timeZone }: { item: MyEvent; timeZone: string }) {
  const { t, i18n } = useTranslation('events');
  const client = useQueryClient();
  const write = useMutation(eventRegistrationMutation(client));
  const [canceling, setCanceling] = useState(false);
  const { registration: r, event } = item;
  const perform = (action: EventRegistrationAction) =>
    write.mutate({ registrationId: r.id, action });
  const lang = i18n.resolvedLanguage ?? 'en';
  const writeButton = (action: 'leave' | 'accept' | 'decline') => (
    <Button
      requiresOnline
      size="sm"
      variant={action === 'accept' ? 'default' : 'outline'}
      disabled={write.isPending}
      onClick={() => perform(action)}
    >
      <PendingIcon pending={write.isPending && write.variables?.action === action} />
      {t(action)}
    </Button>
  );

  return (
    <div className="space-y-2">
      {r.status === 'invited' && r.expiresAt && (
        <p className="text-sm text-muted-foreground">
          {t('reservedUntil', { at: formatDateTime(r.expiresAt, timeZone, lang) })}
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        {r.status === 'waiting' && writeButton('leave')}
        {r.status === 'invited' && (
          <>
            {event.signupOpen && writeButton('accept')}
            {writeButton('decline')}
          </>
        )}
        {r.cancelable && (
          <Button requiresOnline size="sm" variant="outline" onClick={() => setCanceling(true)}>
            {t('cancel')}
          </Button>
        )}
      </div>
      {r.status === 'registered' && !r.cancelable && (
        <p className="text-sm text-muted-foreground">{t('tooLate')}</p>
      )}
      {write.error && <RegistrationProblem error={write.error} />}
      {canceling && <CancelRegistrationDialog item={item} onClose={() => setCanceling(false)} />}
    </div>
  );
}
