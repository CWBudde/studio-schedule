import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Unlink } from 'lucide-react';
import { useEffect, useId, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { revokeCalendarFeedMutation } from '@/api/queries';
import { ProblemAlert } from '@/components/ProblemAlert';
import { Button } from '@/components/ui/button';
import { statusOf } from '@/features/auth/form';

/**
 * Asks before revoking a calendar feed and says what happens: calendars
 * subscribed to it stop receiving updates. `onRevoked` runs once it is gone,
 * also when it had been revoked or replaced meanwhile (404, which is no
 * error here). The list of feeds is asked again either way.
 */
export function RevokeFeed({
  id,
  feedId,
  onCancel,
  onRevoked,
}: {
  id: string;
  feedId: string;
  onCancel: () => void;
  onRevoked?: () => void;
}) {
  const { t } = useTranslation('feeds');
  const questionId = useId();
  const keepButton = useRef<HTMLButtonElement>(null);
  const queryClient = useQueryClient();
  const revoke = useMutation(revokeCalendarFeedMutation(queryClient));

  // The safe choice has the focus first.
  useEffect(() => keepButton.current?.focus(), []);

  return (
    <div
      id={id}
      role="group"
      aria-labelledby={questionId}
      className="space-y-3 rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm"
    >
      <p id={questionId} className="font-medium">
        {t('revoke.question')}
      </p>
      <p>{t('revoke.explain')}</p>
      {/* A 404 is no failure: the link is gone either way. */}
      <ProblemAlert error={statusOf(revoke.error) === 404 ? null : revoke.error} />
      <div className="flex flex-wrap gap-2">
        <Button
          variant="destructive"
          size="sm"
          disabled={revoke.isPending}
          onClick={() =>
            revoke.mutate(feedId, {
              onSuccess: () => {
                toast.success(t('revoke.done'));
                onRevoked?.();
              },
              onError: (error) => {
                if (statusOf(error) !== 404) return;
                toast.info(t('revoke.notFound'));
                onRevoked?.();
              },
            })
          }
        >
          <Unlink aria-hidden="true" />
          {revoke.isPending ? t('revoke.submitting') : t('revoke.submit')}
        </Button>
        <Button ref={keepButton} variant="outline" size="sm" onClick={onCancel}>
          {t('revoke.cancel')}
        </Button>
      </div>
    </div>
  );
}
