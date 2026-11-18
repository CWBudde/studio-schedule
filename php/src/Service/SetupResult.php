<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateTimeImmutable;

final readonly class SetupResult
{
    public function __construct(
        public string $accountId,
        #[\SensitiveParameter]
        public string $token,
        public DateTimeImmutable $expiresAt,
    ) {}

    /** The app URL ends in '/' (or '#/' for the demo), as with Go setup. */
    public function passwordLink(string $appUrl): string
    {
        return $appUrl . 'reset-password?token=' . rawurlencode($this->token);
    }
}
