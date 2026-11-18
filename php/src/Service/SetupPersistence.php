<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;
use Symfony\Component\Uid\UuidV7;

interface SetupPersistence
{
    /** Store the school, initial admin and reset-token digest in one transaction. */
    public function initialize(
        SetupInput $input,
        UuidV7 $personId,
        UuidV7 $accountId,
        #[\SensitiveParameter]
        string $tokenHash,
        DateTimeImmutable $createdAt,
        DateTimeImmutable $expiresAt,
    ): void;
}
