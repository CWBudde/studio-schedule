<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use Psr\Clock\ClockInterface;
use SensitiveParameter;
use StudioSchedule\Mail\Renderer;
use StudioSchedule\Store\PasswordResetStore;

final readonly class PasswordChange
{
    /** @param (Closure():void)|null $wakeMail */
    public function __construct(
        private PasswordResetStore $store,
        private PasswordHash $hasher,
        private CommonPasswords $commonPasswords,
        private ClockInterface $clock,
        private Renderer $mail,
        private ?Closure $wakeMail = null,
    ) {}

    /** Change the password after the request has passed length validation. */
    public function change(
        SessionPrincipal $principal,
        #[SensitiveParameter]
        string $presentedToken,
        #[SensitiveParameter]
        string $currentPassword,
        #[SensitiveParameter]
        string $newPassword,
    ): ?PasswordChangeFailure {
        if ($principal->impersonatorId !== null) {
            return PasswordChangeFailure::Impersonating;
        }
        if ($this->commonPasswords->isCommon($newPassword)) {
            return PasswordChangeFailure::CommonPassword;
        }

        $accountId = $principal->accountId;
        $verifiedHash = $this->store->passwordHashForAccount($accountId);
        if ($verifiedHash === null || !$this->hasher->verify($verifiedHash, $currentPassword)) {
            return PasswordChangeFailure::WrongPassword;
        }
        $newHash = $this->hasher->hash($newPassword);
        $tokenHash = hash('sha256', $presentedToken);

        $failure = $this->store->transactional(function () use ($accountId, $tokenHash, $verifiedHash, $newHash): ?PasswordChangeFailure {
            $session = $this->store->lockPasswordChange($accountId, $tokenHash, $this->clock);
            if ($session === null) {
                return PasswordChangeFailure::NoSession;
            }
            if ($session['hash'] === null || !hash_equals($verifiedHash, $session['hash'])) {
                return PasswordChangeFailure::WrongPassword;
            }

            $this->store->replacePassword($session['accountId'], $newHash, $session['now'], $session['sessionId']);
            $data = $this->store->passwordChangedData($session['accountId']);
            $mail = $this->mail->render($data['locale'], 'password-changed', [
                'SchoolName' => $data['schoolName'],
                'GivenName' => $data['givenName'],
            ]);
            $this->store->queuePasswordChanged($session['email'], $mail, $session['now']);

            return null;
        });
        if ($failure === null) {
            $this->wakeMail?->__invoke();
        }

        return $failure;
    }
}
