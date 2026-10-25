import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from '@tanstack/react-router';
import { CircleAlert, LogIn, RotateCcw } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import type { AccountEntry } from '@/api/client';
import { impersonateMutation, listAccountsQuery, meQuery } from '@/api/queries';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { AccountActions } from './AccountActions';
import { NoAccess } from './NoAccess';

const fullName = (a: { givenName: string; familyName: string }) => `${a.givenName} ${a.familyName}`;

/** Whether an admin may sign in as the account (the API refuses the others). */
const canImpersonate = (a: AccountEntry) => a.role !== 'admin' && a.confirmed && !a.disabled;

/**
 * The accounts (/admin/accounts), for admins only: who can sign in, a way
 * to sign in as one of them ("login as"), and the account administration of
 * ADR 0016 (disable, enable, change the role, delete). Everyone else sees
 * that the page is not for them; the API refuses them the list as well.
 */
export function AccountsPage() {
  const { t } = useTranslation('auth');
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const me = useQuery(meQuery);
  const isAdmin = me.data?.role === 'admin';
  const accounts = useQuery({ ...listAccountsQuery, enabled: isAdmin });
  const impersonate = useMutation(impersonateMutation(queryClient));
  // Once it succeeds, the signed-in account is no admin any more: the admin
  // layout may unmount this page before mutate()'s own callbacks run, and
  // TanStack Query skips those of an unmounted observer. The promise of
  // mutateAsync settles anyway, so the toast and the navigation follow it.
  // A failure shows through `impersonate.isError`.
  const signInAs = async (account: AccountEntry) => {
    let target;
    try {
      target = await impersonate.mutateAsync(account.id);
    } catch {
      return;
    }
    toast.success(t('accounts.signedInAs', { name: fullName(target) }));
    void navigate({ to: '/' });
  };

  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <h1 className="font-heading text-3xl font-semibold tracking-tight">
          {t('accounts.title')}
        </h1>
        {isAdmin && (
          <p className="max-w-prose text-muted-foreground">{t('accounts.description')}</p>
        )}
      </div>
      {me.isPending || impersonate.isSuccess ? (
        <AccountsLoading />
      ) : !isAdmin ? (
        <NoAccess message={t('accounts.noAccess')} />
      ) : accounts.data ? (
        <AccountsTable
          accounts={accounts.data}
          ownId={me.data?.accountId ?? ''}
          onSignInAs={(account) => void signInAs(account)}
          pending={impersonate.isPending}
          failed={
            impersonate.isError
              ? accounts.data.find((a) => a.id === impersonate.variables)
              : undefined
          }
        />
      ) : accounts.isError ? (
        <AccountsError onRetry={() => void accounts.refetch()} />
      ) : (
        <AccountsLoading />
      )}
    </div>
  );
}

function AccountsTable({
  accounts,
  ownId,
  onSignInAs,
  pending,
  failed,
}: {
  accounts: AccountEntry[];
  /** The signed-in admin's own account, which the admin does not change here. */
  ownId: string;
  onSignInAs: (account: AccountEntry) => void;
  /** Whether a sign-in as an account is under way. */
  pending: boolean;
  /** The account a sign-in as failed for, if any. */
  failed?: AccountEntry;
}) {
  const { t } = useTranslation('auth');

  return (
    <div className="space-y-4">
      {failed && (
        <Alert variant="destructive">
          <CircleAlert aria-hidden="true" />
          <AlertTitle>{t('accounts.signInAsFailed', { name: fullName(failed) })}</AlertTitle>
        </Alert>
      )}
      <div className="rounded-xl ring-1 ring-foreground/10">
        <Table aria-label={t('accounts.title')}>
          <TableHeader>
            <TableRow>
              <TableHead className="pl-4">{t('accounts.name')}</TableHead>
              <TableHead className="hidden sm:table-cell">{t('accounts.email')}</TableHead>
              <TableHead>{t('accounts.role')}</TableHead>
              <TableHead>{t('accounts.status')}</TableHead>
              <TableHead className="pr-4 text-right">{t('accounts.actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {accounts.map((account) => (
              <TableRow key={account.id}>
                <TableCell className="pl-4 whitespace-normal">
                  <span className="block font-medium">{fullName(account)}</span>
                  {/* On phones the email goes under the name, not in a column. */}
                  <span className="block text-xs break-all text-muted-foreground sm:hidden">
                    {account.email}
                  </span>
                </TableCell>
                <TableCell className="hidden text-muted-foreground sm:table-cell">
                  {account.email}
                </TableCell>
                <TableCell>{t(`roles.${account.role}`)}</TableCell>
                <TableCell>
                  <AccountStatus account={account} />
                </TableCell>
                <TableCell className="pr-4 text-right">
                  <div className="flex items-center justify-end gap-1">
                    <Button
                      variant="outline"
                      size="sm"
                      aria-label={t('accounts.signInAsLabel', { name: fullName(account) })}
                      disabled={!canImpersonate(account) || pending}
                      onClick={() => onSignInAs(account)}
                    >
                      <LogIn aria-hidden="true" />
                      <span className="hidden sm:inline">{t('accounts.signInAs')}</span>
                    </Button>
                    <AccountActions account={account} own={account.id === ownId} />
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}

function AccountStatus({ account }: { account: AccountEntry }) {
  const { t } = useTranslation('auth');
  if (account.disabled) return <Badge variant="destructive">{t('accounts.disabled')}</Badge>;
  if (!account.confirmed) return <Badge variant="outline">{t('accounts.unconfirmed')}</Badge>;
  return <Badge variant="secondary">{t('accounts.active')}</Badge>;
}

function AccountsLoading() {
  const { t } = useTranslation('auth');
  return (
    <div role="status" className="space-y-3">
      <span className="sr-only">{t('accounts.loading')}</span>
      {[0, 1, 2, 3].map((row) => (
        <Skeleton key={row} className="h-10 w-full" aria-hidden="true" />
      ))}
    </div>
  );
}

function AccountsError({ onRetry }: { onRetry: () => void }) {
  const { t } = useTranslation('auth');
  return (
    <Alert variant="destructive">
      <CircleAlert aria-hidden="true" />
      <AlertTitle>{t('accounts.loadError')}</AlertTitle>
      <AlertDescription>
        <Button variant="outline" size="sm" className="mt-2" onClick={onRetry}>
          <RotateCcw aria-hidden="true" />
          {t('accounts.retry')}
        </Button>
      </AlertDescription>
    </Alert>
  );
}
