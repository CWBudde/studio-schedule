import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Link } from '@tanstack/react-router';
import { LogIn, LogOut, UserRound } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import { useBackendStatus } from '@/api/backendStatus';
import { logoutMutation, meQuery } from '@/api/queries';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';

/** "Sign in" when signed out; the signed-in person's menu (with "Sign out") otherwise. */
export function AccountMenu() {
  const { t } = useTranslation('auth');
  const queryClient = useQueryClient();
  const backend = useBackendStatus();
  const me = useQuery({ ...meQuery, enabled: backend.state === 'ready' });
  const logout = useMutation(logoutMutation(queryClient));

  // Neither while unknown, so "Sign in" does not flash for a signed-in person.
  if (me.isPending) return null;

  if (!me.data) {
    return (
      <Button asChild variant="ghost" size="sm">
        <Link to="/login">
          <LogIn aria-hidden="true" />
          {t('account.signIn')}
        </Link>
      </Button>
    );
  }

  const name = `${me.data.givenName} ${me.data.familyName}`;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" className="max-w-40 gap-1.5">
          <UserRound aria-hidden="true" />
          <span className="truncate">{name}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-48">
        <DropdownMenuLabel className="font-normal">
          <span className="block text-xs text-muted-foreground">{t('account.signedInAs')}</span>
          <span className="block truncate font-medium text-foreground">{name}</span>
          <span className="block truncate text-xs text-muted-foreground">{me.data.email}</span>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          disabled={logout.isPending}
          onSelect={() =>
            logout.mutate(undefined, {
              onSuccess: () => toast.success(t('account.signedOut')),
              onError: () => toast.error(t('account.signOutFailed')),
            })
          }
        >
          <LogOut aria-hidden="true" />
          {t('account.signOut')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
