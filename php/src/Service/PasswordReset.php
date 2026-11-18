<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeImmutable;
use Psr\Clock\ClockInterface;
use StudioSchedule\Mail\Renderer;

final readonly class PasswordReset
{
    /** @param (Closure():void)|null $wakeMail */
    public function __construct(
        private PasswordResetPersistence $store,
        private PasswordHash $hasher,
        private CommonPasswords $commonPasswords,
        private ClockInterface $clock,
        private Renderer $mail,
        private ?Closure $wakeMail = null,
    ) {}

    /** Consume a reset link with a password that passed request length validation. */
    public function confirm(#[\SensitiveParameter] string $token, #[\SensitiveParameter] string $password): PasswordResetResult
    {
        if ($this->commonPasswords->isCommon($password)) {
            return PasswordResetResult::CommonPassword;
        }

        $tokenHash = hash('sha256', $token);
        $candidate = $this->store->findAccount($tokenHash);
        if (!self::canReset($candidate, $this->clock->now())) {
            return PasswordResetResult::InvalidToken;
        }

        $passwordHash = $this->hasher->hash($password);

        $queuedMail = false;
        $result = $this->store->transactional(function () use ($candidate, $tokenHash, $passwordHash, &$queuedMail): PasswordResetResult {
            $queuedMail = false;
            // Serialize all reset links for this account. Re-read the token after
            // locking because a competing reset may have consumed it while we hashed.
            $this->store->lockAccount($candidate->id);
            $account = $this->store->findAccount($tokenHash, $candidate->id);
            $now = $this->clock->now();
            if (!self::canReset($account, $now)) {
                return PasswordResetResult::InvalidToken;
            }

            $this->store->setPassword($account, $passwordHash, $now);
            if ($account->hasPassword) {
                $data = $this->store->passwordChangedData($account->id);
                $mail = $this->mail->render($data['locale'], 'password-changed', [
                    'SchoolName' => $data['schoolName'],
                    'GivenName' => $data['givenName'],
                ]);
                $this->store->queuePasswordChanged($account->email, $mail, $now);
                $queuedMail = true;
            }

            return PasswordResetResult::Completed;
        });
        if ($queuedMail && $this->wakeMail !== null) {
            ($this->wakeMail)();
        }

        return $result;
    }

    /** @phpstan-assert-if-true !null $account */
    private static function canReset(?PasswordResetAccount $account, DateTimeImmutable $now): bool
    {
        return $account !== null
            && $account->tokenExpiresAt > $now
            && $account->tokenEmail === $account->email
            && !$account->disabled
            && ($account->confirmed || !$account->hasPassword);
    }
}
