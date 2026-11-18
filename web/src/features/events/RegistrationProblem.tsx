import { useTranslation } from 'react-i18next';
import { ProblemAlert } from '@/components/ProblemAlert';

export function RegistrationProblem({ error }: { error: unknown }) {
  const { t } = useTranslation('events');
  return (
    <ProblemAlert
      error={error}
      conflicts={{
        closed: t('signupClosed'),
        full: t('becameFull'),
        'not-full': t('becameAvailable'),
        duplicate: t('alreadyRegistered'),
        'too-late': t('tooLate'),
        'invite-expired': t('invite.unavailable'),
        'invite-used': t('invite.unavailable'),
        'not-invited': t('invite.unavailable'),
        'not-waiting': t('notWaiting'),
        'already-canceled': t('states.canceled'),
        'receipt-settings-incomplete': t('refundUnavailable'),
      }}
    />
  );
}
