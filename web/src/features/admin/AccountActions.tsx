import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Ban, CircleCheck, EllipsisVertical, ShieldCheck, Trash2 } from 'lucide-react';
import { type ReactNode, useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';
import type { AccountEntry } from '@/api/client';
import {
  type AccountRole,
  deleteAccountMutation,
  setAccountDisabledMutation,
  setAccountRoleMutation,
} from '@/api/queries';
import { ProblemAlert } from '@/components/ProblemAlert';
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog';
import { Button } from '@/components/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Label } from '@/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';

const roles: readonly AccountRole[] = ['member', 'instructor', 'office', 'admin'];

const fullName = (a: { givenName: string; familyName: string }) => `${a.givenName} ${a.familyName}`;

type Dialog = 'disable' | 'role' | 'delete';

/**
 * The admin's actions on an account (ADR 0016): disable or enable it,
 * change its role, delete it. Disabling, the role and deleting ask first.
 * The admin's own account offers none of them: another admin changes it,
 * and the API refuses it as well (409 `own-account`), like the last enabled
 * admin (409 `last-admin`).
 */
export function AccountActions({ account, own }: { account: AccountEntry; own: boolean }) {
  const { t } = useTranslation('auth');
  const queryClient = useQueryClient();
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const enable = useMutation(setAccountDisabledMutation(queryClient));
  const name = fullName(account);

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t('accounts.manage.label', { name })}
            disabled={own}
          >
            <EllipsisVertical aria-hidden="true" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="min-w-44">
          {account.disabled ? (
            <DropdownMenuItem
              onSelect={() =>
                enable.mutate(
                  { accountId: account.id, disabled: false },
                  {
                    onSuccess: () => toast.success(t('accounts.manage.enabled', { name })),
                    onError: () => toast.error(t('accounts.manage.failed', { name })),
                  },
                )
              }
            >
              <CircleCheck aria-hidden="true" />
              {t('accounts.manage.enable')}
            </DropdownMenuItem>
          ) : (
            <DropdownMenuItem onSelect={() => setDialog('disable')}>
              <Ban aria-hidden="true" />
              {t('accounts.manage.disable')}
            </DropdownMenuItem>
          )}
          <DropdownMenuItem onSelect={() => setDialog('role')}>
            <ShieldCheck aria-hidden="true" />
            {t('accounts.manage.changeRole')}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem variant="destructive" onSelect={() => setDialog('delete')}>
            <Trash2 aria-hidden="true" />
            {t('accounts.manage.delete')}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      {dialog === 'disable' && <DisableDialog account={account} onClose={() => setDialog(null)} />}
      {dialog === 'role' && <RoleDialog account={account} onClose={() => setDialog(null)} />}
      {dialog === 'delete' && <DeleteDialog account={account} onClose={() => setDialog(null)} />}
    </>
  );
}

/** Why the API refused a change of the account, in words. */
function useConflicts(name: string) {
  const { t } = useTranslation('auth');
  return {
    conflicts: {
      'last-admin': t('accounts.manage.lastAdmin'),
      'own-account': t('accounts.manage.ownAccount'),
      'in-use': t('accounts.manage.inUse', { name }),
    },
    messages: { 404: t('accounts.manage.notFound') },
  };
}

/** A confirmation of one change; the caller runs it and closes the dialog. */
function Confirm({
  title,
  description,
  error,
  pending,
  confirm,
  pendingLabel,
  destructive,
  onConfirm,
  onClose,
  children,
  name,
}: {
  title: string;
  description: string;
  error: unknown;
  pending: boolean;
  confirm: string;
  pendingLabel: string;
  destructive?: boolean;
  onConfirm: () => void;
  onClose: () => void;
  children?: ReactNode;
  name: string;
}) {
  const { t } = useTranslation('auth');
  const { conflicts, messages } = useConflicts(name);
  return (
    <AlertDialog
      open
      onOpenChange={(open) => {
        if (!open && !pending) onClose();
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>{description}</AlertDialogDescription>
        </AlertDialogHeader>
        {children}
        <ProblemAlert error={error} conflicts={conflicts} messages={messages} />
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>{t('accounts.manage.cancel')}</AlertDialogCancel>
          {/* No AlertDialogAction: it would close the dialog before the answer. */}
          <Button
            variant={destructive ? 'destructive' : 'default'}
            disabled={pending}
            onClick={onConfirm}
          >
            {pending ? pendingLabel : confirm}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

function DisableDialog({ account, onClose }: { account: AccountEntry; onClose: () => void }) {
  const { t } = useTranslation('auth');
  const queryClient = useQueryClient();
  const disable = useMutation(setAccountDisabledMutation(queryClient));
  const name = fullName(account);
  return (
    <Confirm
      name={name}
      title={t('accounts.manage.disableTitle', { name })}
      description={t('accounts.manage.disableDescription')}
      error={disable.error}
      pending={disable.isPending}
      confirm={t('accounts.manage.disable')}
      pendingLabel={t('accounts.manage.saving')}
      destructive
      onClose={onClose}
      onConfirm={() =>
        disable.mutate(
          { accountId: account.id, disabled: true },
          {
            onSuccess: () => {
              toast.success(t('accounts.manage.disabled', { name }));
              onClose();
            },
          },
        )
      }
    />
  );
}

function RoleDialog({ account, onClose }: { account: AccountEntry; onClose: () => void }) {
  const { t } = useTranslation('auth');
  const queryClient = useQueryClient();
  const change = useMutation(setAccountRoleMutation(queryClient));
  const [role, setRole] = useState<AccountRole>(account.role);
  const selectId = useId();
  const name = fullName(account);
  return (
    <Confirm
      name={name}
      title={t('accounts.manage.roleTitle', { name })}
      description={t('accounts.manage.roleDescription')}
      error={change.error}
      pending={change.isPending}
      confirm={t('accounts.manage.saveRole')}
      pendingLabel={t('accounts.manage.saving')}
      onClose={onClose}
      onConfirm={() =>
        role === account.role
          ? onClose()
          : change.mutate(
              { accountId: account.id, role },
              {
                onSuccess: () => {
                  toast.success(
                    t('accounts.manage.roleChanged', { name, role: t(`roles.${role}`) }),
                  );
                  onClose();
                },
              },
            )
      }
    >
      <div className="grid gap-1.5">
        <Label htmlFor={selectId}>{t('accounts.role')}</Label>
        <Select value={role} onValueChange={(value) => setRole(value as AccountRole)}>
          <SelectTrigger id={selectId} className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {roles.map((r) => (
              <SelectItem key={r} value={r}>
                {t(`roles.${r}`)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
    </Confirm>
  );
}

function DeleteDialog({ account, onClose }: { account: AccountEntry; onClose: () => void }) {
  const { t } = useTranslation('auth');
  const queryClient = useQueryClient();
  const remove = useMutation(deleteAccountMutation(queryClient));
  const name = fullName(account);
  // Once it succeeds, the account's row, and this dialog with it, may
  // unmount before mutate()'s own callbacks run; the promise of mutateAsync
  // settles anyway. A failure shows in the dialog.
  const run = async () => {
    try {
      await remove.mutateAsync(account.id);
    } catch {
      return;
    }
    toast.success(t('accounts.manage.deleted', { name }));
    onClose();
  };
  return (
    <Confirm
      name={name}
      title={t('accounts.manage.deleteTitle', { name })}
      description={t('accounts.manage.deleteDescription')}
      error={remove.error}
      pending={remove.isPending}
      confirm={t('accounts.manage.delete')}
      pendingLabel={t('accounts.manage.deleting')}
      destructive
      onClose={onClose}
      onConfirm={() => void run()}
    />
  );
}
