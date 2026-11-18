<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;

final readonly class PasswordResetAccount
{
    public function __construct(
        public string $id,
        public string $email,
        public string $tokenEmail,
        public DateTimeImmutable $tokenExpiresAt,
        public bool $confirmed,
        public bool $disabled,
        public bool $hasPassword,
    ) {}
}
