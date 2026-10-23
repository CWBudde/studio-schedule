import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { UserRoundCog, Undo2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { useBackendStatus } from '@/api/backendStatus';
import { meQuery, stopImpersonationMutation } from '@/api/queries';
import { Button } from '@/components/ui/button';

/**
 * Shown on every page while an admin is signed in as someone else ("login
 * as"): whose account it is, and a way back to the admin's own account.
 */
export function ImpersonationBanner() {
  const { t } = useTranslation('auth');
  const queryClient = useQueryClient();
  const backend = useBackendStatus();
  const me = useQuery({ ...meQuery, enabled: backend.state === 'ready' });
  const stop = useMutation(stopImpersonationMutation(queryClient));

  const impersonator = me.data?.impersonator;
  if (!me.data || !impersonator) return null;

  return (
    <section
      aria-label={t('impersonation.label')}
      className="border-b border-foreground bg-foreground text-background"
    >
      <div className="mx-auto flex max-w-5xl flex-wrap items-center gap-x-3 gap-y-2 px-4 py-2.5 text-sm">
        <UserRoundCog className="size-4 shrink-0" aria-hidden="true" />
        <div className="min-w-0 flex-1 basis-56">
          <p className="leading-snug break-words">
            {t('impersonation.banner', {
              name: `${me.data.givenName} ${me.data.familyName}`,
              email: me.data.email,
              admin: `${impersonator.givenName} ${impersonator.familyName}`,
            })}
          </p>
          {stop.isError && (
            <p role="alert" className="mt-1 font-medium">
              {t('impersonation.stopFailed')}
            </p>
          )}
        </div>
        <Button
          size="sm"
          variant="secondary"
          disabled={stop.isPending}
          onClick={() =>
            stop.mutate(undefined, {
              onSuccess: (admin) =>
                toast.success(t('impersonation.stopped', { name: admin.givenName })),
            })
          }
        >
          <Undo2 aria-hidden="true" />
          {stop.isPending ? t('impersonation.stopping') : t('impersonation.stop')}
        </Button>
      </div>
    </section>
  );
}
