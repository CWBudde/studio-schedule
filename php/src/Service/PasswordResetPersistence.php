<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeImmutable;
use StudioSchedule\Mail\Rendered;

interface PasswordResetPersistence
{
    public function findAccount(#[\SensitiveParameter] string $tokenHash, ?string $lockedAccountId = null): ?PasswordResetAccount;

    /** @param Closure(): PasswordResetResult $reset */
    public function transactional(Closure $reset): PasswordResetResult;

    public function lockAccount(string $accountId): void;

    public function setPassword(PasswordResetAccount $account, #[\SensitiveParameter] string $passwordHash, DateTimeImmutable $now): void;

    /** @return array{locale: string, schoolName: string, givenName: string} */
    public function passwordChangedData(string $accountId): array;

    public function queuePasswordChanged(string $email, Rendered $rendered, DateTimeImmutable $now): void;
}
