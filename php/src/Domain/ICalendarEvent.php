<?php

declare(strict_types=1);

namespace StudioSchedule\Domain;

use DateTimeImmutable;

/** The data for one VEVENT, with unescaped text and start and end instants. */
final readonly class ICalendarEvent
{
    public function __construct(
        public string $uid,
        public DateTimeImmutable $start,
        public DateTimeImmutable $end,
        public string $summary,
        public ICalendarStatus $status = ICalendarStatus::Confirmed,
        public string $location = '',
        public string $description = '',
        public string $url = '',
    ) {}
}
