import { CircleAlert } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { retryAfterOf, statusOf } from './form';

/**
 * The answer of a failed form submission: "too many attempts" (or the 429
 * message of `messages`) with the wait for a 429, the message of `messages`
 * for the status, or a generic error.
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
        <AlertTitle>{messages[429] ?? t('form.tooMany.title')}</AlertTitle>
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

/**
 * How long to wait: seconds up to 90, whole minutes (rounded up) below two
 * hours, whole hours (rounded up) above.
 */
function WaitText({ seconds }: { seconds: number | undefined }) {
  const { t } = useTranslation('auth');
  if (seconds === undefined) return <p>{t('form.tooMany.later')}</p>;
  if (seconds <= 90) return <p>{t('form.tooMany.seconds', { count: seconds })}</p>;
  const minutes = Math.ceil(seconds / 60);
  if (minutes < 120) return <p>{t('form.tooMany.minutes', { count: minutes })}</p>;
  return <p>{t('form.tooMany.hours', { count: Math.ceil(minutes / 60) })}</p>;
}
