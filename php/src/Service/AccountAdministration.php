<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeImmutable;
use Psr\Clock\ClockInterface;
use SensitiveParameter;
use StudioSchedule\Mail\Renderer;
use StudioSchedule\Store\AdminAccountsStore;
use StudioSchedule\Store\OutboxStore;
use StudioSchedule\Store\PasswordResetStore;
use StudioSchedule\Store\SessionStore;

/**
 * @phpstan-import-type AccountEntry from AdminAccountsStore
 * @phpstan-import-type LockedAccount from AdminAccountsStore
 */
final readonly class AccountAdministration
{
    /** @param (Closure():void)|null $wakeMail */
    public function __construct(
        private AdminAccountsStore $accounts,
        private SessionStore $sessions,
        private PasswordResetStore $credentials,
        private PasswordHash $passwords,
        private Renderer $mail,
        private OutboxStore $outbox,
        private ClockInterface $clock,
        private ?Closure $wakeMail = null,
    ) {}

    /** @return AccountEntry|AccountAdministrationFailure */
    public function setDisabled(SessionPrincipal $principal, string $id, bool $disabled): array|AccountAdministrationFailure
    {
        return $this->change($principal, $id, function (array $account) use ($principal, $disabled): array|AccountAdministrationFailure {
            $key = $account['id'];
            if ($disabled === $account['disabled']) {
                return $this->accounts->entry($key);
            }
            if ($disabled && $account['lastAdmin']) {
                return AccountAdministrationFailure::LastAdmin;
            }

            $now = $this->clock->now();
            $this->accounts->setDisabled($key, $disabled ? $now : null);
            if ($disabled) {
                $this->sessions->endForAccount($key, $now);
            }
            $this->accounts->audit($principal->accountId, $disabled ? 'account-disabled' : 'account-enabled', 'account', $key, $now);

            return $this->accounts->entry($key);
        });
    }

    /** @return AccountEntry|AccountAdministrationFailure */
    public function setRole(SessionPrincipal $principal, string $id, string $role): array|AccountAdministrationFailure
    {
        return $this->change($principal, $id, function (array $account) use ($principal, $role): array|AccountAdministrationFailure {
            $key = $account['id'];
            $previous = $account['role'];
            if ($previous === $role) {
                return $this->accounts->entry($key);
            }
            if ($role !== 'admin' && $account['lastAdmin']) {
                return AccountAdministrationFailure::LastAdmin;
            }

            $this->accounts->setRole($key, $role);
            $this->accounts->audit(
                $principal->accountId,
                'account-role-changed',
                'account',
                $key,
                $this->clock->now(),
                ['previousRole' => $previous, 'role' => $role],
            );

            return $this->accounts->entry($key);
        });
    }

    public function delete(SessionPrincipal $principal, string $id): ?AccountAdministrationFailure
    {
        return $this->change($principal, $id, function (array $account) use ($principal): ?AccountAdministrationFailure {
            if ($account['lastAdmin']) {
                return AccountAdministrationFailure::LastAdmin;
            }
            $this->remove($account, $principal->accountId, $this->clock->now());

            return null;
        });
    }

    public function deleteOwn(SessionPrincipal $principal, #[SensitiveParameter] string $password, string $locale): ?AccountAdministrationFailure
    {
        if ($principal->impersonatorId !== null) {
            return AccountAdministrationFailure::Impersonating;
        }
        $id = $principal->accountId;
        $verifiedHash = $this->credentials->passwordHashForAccount($id);
        if ($verifiedHash === null || !$this->passwords->verify($verifiedHash, $password)) {
            return AccountAdministrationFailure::WrongPassword;
        }

        $failure = $this->accounts->write(function () use ($id, $verifiedHash, $locale): ?AccountAdministrationFailure {
            $account = $this->accounts->lock($id);
            if ($account === null) {
                return AccountAdministrationFailure::NotFound;
            }
            $currentHash = $this->credentials->passwordHashForAccount($id, locked: true);
            if ($currentHash === null || !hash_equals($verifiedHash, $currentHash)) {
                return AccountAdministrationFailure::WrongPassword;
            }
            if ($account['lastAdmin']) {
                return AccountAdministrationFailure::LastAdmin;
            }

            $now = $this->clock->now();
            $data = $this->credentials->passwordChangedData($account['id'], $locale);
            $rendered = $this->mail->render($data['locale'], 'account-deleted', [
                'SchoolName' => $data['schoolName'], 'GivenName' => $data['givenName'],
            ]);
            $this->outbox->enqueue($account['email'], 'account-deleted', $rendered, $now);
            $this->remove($account, null, $now);

            return null;
        });
        if ($failure === null) {
            $this->wakeMail?->__invoke();
        }

        return $failure;
    }

    /**
     * @template T
     * @param Closure(LockedAccount): T $apply
     * @return T|AccountAdministrationFailure
     */
    private function change(SessionPrincipal $principal, string $id, Closure $apply): mixed
    {
        if ($principal->accountId === $id) {
            return AccountAdministrationFailure::OwnAccount;
        }

        return $this->accounts->write(function () use ($id, $apply): mixed {
            $account = $this->accounts->lock($id);

            return $account === null ? AccountAdministrationFailure::NotFound : $apply($account);
        });
    }

    /** @param LockedAccount $account */
    private function remove(array $account, ?string $actor, DateTimeImmutable $now): void
    {
        $this->sessions->endForAccount($account['id'], $now);
        $this->accounts->delete($account['id'], $account['personId']);
        $this->accounts->audit($actor, 'account-deleted', 'person', $account['personId'], $now);
    }
}
