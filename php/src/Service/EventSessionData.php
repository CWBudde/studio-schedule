<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\TimeOfDay;

final readonly class EventSessionData
{
    public function __construct(
        public Date $date,
        public TimeOfDay $startTime,
        public int $durationMinutes,
        public string $roomId,
        public string $instructorId,
        public ?string $accompanistId,
        public bool $confirmRoomOverlap,
        public bool $confirmPast,
    ) {}
}
