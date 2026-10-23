import { CircleAlert } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { retryAfterOf, statusOf } from './form';

/**
 * The answer of a failed form submission: "too many attempts" with the wait
 * for a 429, the message of `messages` for the status, or a generic error.
 */
export function FormError({
  error,
  messages = {},
}: {
  error: unknown;
  /** Titles by HTTP status, e.g. `{ 401: 'The password is incorrect.' }`. */
  messages?: Partial<Record<number, string>>;
}) {
  const { t } = useTranslation('auth');
  if (!error) return null;
  const status = statusOf(error);

  if (status === 429) {
    return (
      <Alert variant="destructive">
        <CircleAlert aria-hidden="true" />
        <AlertTitle>{t('form.tooMany.title')}</AlertTitle>
        <AlertDescription>
          <WaitText seconds={retryAfterOf(error)} />
        </AlertDescription>
      </Alert>
    );
  }

  return (
    <Alert variant="destructive">
      <CircleAlert aria-hidden="true" />
      <AlertTitle>{(status !== undefined && messages[status]) || t('form.failed')}</AlertTitle>
    </Alert>
  );
}

/** How long to wait: seconds up to 90, whole minutes (rounded up) above. */
function WaitText({ seconds }: { seconds: number | undefined }) {
  const { t } = useTranslation('auth');
  if (seconds === undefined) return <p>{t('form.tooMany.later')}</p>;
  if (seconds <= 90) return <p>{t('form.tooMany.seconds', { count: seconds })}</p>;
  return <p>{t('form.tooMany.minutes', { count: Math.ceil(seconds / 60) })}</p>;
}
