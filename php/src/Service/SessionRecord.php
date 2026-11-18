<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;

final readonly class SessionRecord
{
    public function __construct(
        public SessionPrincipal $principal,
        public DateTimeImmutable $createdAt,
        public DateTimeImmutable $lastSeenAt,
        public DateTimeImmutable $expiresAt,
    ) {}
}
