<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeImmutable;
use StudioSchedule\Mail\Rendered;
use Symfony\Component\Uid\Uuid;

interface AccountInvitationPersistence
{
    /**
     * @template T
     * @param Closure():T $write
     * @return T
     */
    public function inTransaction(Closure $write): mixed;

    public function createPending(string $personId, string $email, string $role): Uuid;

    /** @return array{locale:string,schoolName:string,givenName:string} */
    public function mailData(Uuid $account): array;

    public function writeInvitation(
        Uuid $account,
        string $email,
        string $hash,
        DateTimeImmutable $now,
        DateTimeImmutable $expires,
        Rendered $mail,
    ): void;
}
