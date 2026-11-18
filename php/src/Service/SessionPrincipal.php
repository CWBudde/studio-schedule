<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

final readonly class SessionPrincipal
{
    public function __construct(
        public string $accountId,
        public string $personId,
        public string $role,
        public ?int $visibleDays = null,
        public ?string $impersonatorId = null,
    ) {}
}
