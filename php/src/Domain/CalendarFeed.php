<?php

declare(strict_types=1);

namespace StudioSchedule\Domain;

use DateTimeImmutable;

final readonly class CalendarFeed
{
    public function __construct(
        public string $id,
        public CalendarFeedScope $scope,
        public DateTimeImmutable $createdAt,
        public ?string $locationId = null,
        public ?string $locationName = null,
        public ?DateTimeImmutable $lastUsedAt = null,
    ) {}
}
