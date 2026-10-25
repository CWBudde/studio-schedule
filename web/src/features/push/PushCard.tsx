import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { BellOff, BellRing } from 'lucide-react';
import { useId } from 'react';
import { useTranslation } from 'react-i18next';
import {
  disablePushMutation,
  enablePushMutation,
  NotificationsBlockedError,
  pushDeviceQuery,
  pushStatusQuery,
} from '@/api/queries';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Label } from '@/components/ui/label';
import { Switch } from '@/components/ui/switch';
import { isDemo } from '@/lib/env';
import { useOnline } from '@/lib/online';
import { pushDevice } from './device';

/**
 * "Notifications on this device" on the account page (ADR 0017): a switch
 * that subscribes this browser to the account's notifications (waiting-list
 * invites, besides the mail) or ends that. Hidden while the installation
 * sends no push; a browser that cannot show notifications, or blocks them
 * for the site, is told so.
 */
export function PushCard({ accountId }: { accountId: string }) {
  const { t } = useTranslation('account');
  const titleId = useId();
  const switchId = useId();
  const queryClient = useQueryClient();
  const online = useOnline();
  const status = useQuery(pushStatusQuery);
  const device = useQuery({ ...pushDeviceQuery(accountId), enabled: pushDevice.supported });
  const enable = useMutation(enablePushMutation(queryClient, accountId));
  const disable = useMutation(disablePushMutation(queryClient));

  // Push off (no VAPID keys): nothing to offer. A real installation also
  // needs the key to subscribe with; the demo shows notifications itself.
  if (!status.data?.enabled || (!isDemo && !status.data.publicKey)) return null;

  const on = device.data?.on ?? false;
  const blocked = (device.data?.permission ?? pushDevice.permission()) === 'denied';
  const busy = enable.isPending || disable.isPending || device.isPending;
  const failed = enable.error ?? disable.error;

  const toggle = (checked: boolean) => {
    enable.reset();
    disable.reset();
    if (checked) enable.mutate(status.data?.publicKey);
    else disable.mutate();
  };

  return (
    <Card role="region" aria-labelledby={titleId}>
      <CardHeader>
        <CardTitle>
          <h2 id={titleId} className="font-heading text-lg font-semibold">
            {t('push.title')}
          </h2>
        </CardTitle>
        <CardDescription>{t('push.description')}</CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {!pushDevice.supported ? (
          <p className="flex items-start gap-2 text-sm text-muted-foreground">
            <BellOff className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
            {t('push.unsupported')}
          </p>
        ) : (
          <>
            <div className="flex items-center justify-between gap-4">
              <Label htmlFor={switchId} className="flex items-center gap-2">
                <BellRing className="size-4 text-muted-foreground" aria-hidden="true" />
                {t('push.switch')}
              </Label>
              <Switch
                id={switchId}
                checked={on}
                disabled={busy || !online || (blocked && !on)}
                title={!online ? t('push.offline') : undefined}
                onCheckedChange={toggle}
              />
            </div>
            {blocked && !on && (
              <p role="status" className="text-sm text-muted-foreground">
                {t('push.blocked')}
              </p>
            )}
            {on && <p className="text-sm text-muted-foreground">{t('push.on')}</p>}
            {failed && (
              <Alert variant="destructive">
                <AlertDescription>
                  {failed instanceof NotificationsBlockedError
                    ? t('push.blocked')
                    : enable.error
                      ? t('push.enableFailed')
                      : t('push.disableFailed')}
                </AlertDescription>
              </Alert>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}
