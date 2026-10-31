import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { logoutMutation } from '@/api/queries';

/** Signing out, with a notice of how it went: the account menu's, and a theme's own button. */
export function useSignOut() {
  const { t } = useTranslation('auth');
  const queryClient = useQueryClient();
  const logout = useMutation(logoutMutation(queryClient));
  return {
    signOut: () =>
      logout.mutate(undefined, {
        onSuccess: () => toast.success(t('account.signedOut')),
        onError: () => toast.error(t('account.signOutFailed')),
      }),
    pending: logout.isPending,
  };
}
