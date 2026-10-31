import { Link } from '@tanstack/react-router';
import { LogIn, LogOut, Settings, ShieldCheck, UserRound, UsersRound } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useMe } from '@/api/useMe';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { canAdminister } from '@/features/admin/access';
import { useSignOut } from './useSignOut';

/** "Sign in" when signed out; the signed-in person's menu (with "Sign out") otherwise. */
export function AccountMenu() {
  const { t } = useTranslation('auth');
  const { t: tAdmin } = useTranslation('admin');
  const me = useMe();
  const logout = useSignOut();

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
        <DropdownMenuItem asChild>
          <Link to="/account">
            <Settings aria-hidden="true" />
            {t('account.settings')}
          </Link>
        </DropdownMenuItem>
        {canAdminister(me.data.role) && (
          <DropdownMenuItem asChild>
            <Link to="/admin">
              <ShieldCheck aria-hidden="true" />
              {tAdmin('nav.label')}
            </Link>
          </DropdownMenuItem>
        )}
        {me.data.role === 'admin' && (
          <DropdownMenuItem asChild>
            <Link to="/admin/accounts">
              <UsersRound aria-hidden="true" />
              {t('account.accounts')}
            </Link>
          </DropdownMenuItem>
        )}
        <DropdownMenuSeparator />
        <DropdownMenuItem disabled={logout.pending} onSelect={logout.signOut}>
          <LogOut aria-hidden="true" />
          {t('account.signOut')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
