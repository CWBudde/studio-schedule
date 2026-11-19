<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateInterval;
use DateTimeImmutable;
use DateTimeZone;

final readonly class SessionRecord
{
    public const string IDLE_TIMEOUT = 'PT336H';
    public const string MAX_AGE = 'PT720H';

    public function __construct(
        public SessionPrincipal $principal,
        public DateTimeImmutable $createdAt,
        public DateTimeImmutable $lastSeenAt,
        public DateTimeImmutable $expiresAt,
    ) {}

    public function endsAt(): DateTimeImmutable
    {
        return min($this->expiresAt, $this->maximumEnd());
    }

    public function maximumEnd(): DateTimeImmutable
    {
        return $this->createdAt->setTimezone(new DateTimeZone('UTC'))->add(new DateInterval(self::MAX_AGE));
    }
}
