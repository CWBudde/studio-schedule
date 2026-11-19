<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeImmutable;
use StudioSchedule\Mail\Rendered;

interface PasswordResetPersistence
{
    public function findAccount(#[\SensitiveParameter] string $tokenHash, ?string $lockedAccountId = null): ?PasswordResetAccount;

    /**
     * @template Result
     * @param Closure(): Result $write
     * @return Result
     */
    public function transactional(Closure $write): mixed;

    /** @return array{id: string, email: string}|null */
    public function lockResetRecipient(#[\SensitiveParameter] string $email): ?array;

    public function replaceResetLink(
        string $accountId,
        #[\SensitiveParameter]
        string $email,
        #[\SensitiveParameter]
        string $tokenHash,
        DateTimeImmutable $createdAt,
        DateTimeImmutable $expiresAt,
        #[\SensitiveParameter]
        Rendered $rendered,
    ): void;

    public function lockAccount(string $accountId): void;

    public function setPassword(PasswordResetAccount $account, #[\SensitiveParameter] string $passwordHash, DateTimeImmutable $now): void;

    /** @return array{locale: string, schoolName: string, givenName: string} */
    public function passwordChangedData(string $accountId, string $fallbackLocale = ''): array;

    public function queuePasswordChanged(string $email, Rendered $rendered, DateTimeImmutable $now): void;
}
