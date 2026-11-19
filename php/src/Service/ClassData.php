<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use StudioSchedule\Domain\TimeOfDay;

final readonly class ClassData
{
    public function __construct(
        public string $courseTypeId,
        public string $roomId,
        public string $instructorId,
        public int $weekday,
        public TimeOfDay $startTime,
        public int $durationMinutes,
        public int $capacity,
        public ?string $accompanistId,
        public ?string $levelFromId,
        public ?string $levelToId,
        public bool $allLevels,
        public ?int $stage,
        public ?int $ageFromMonths,
        public ?int $ageToMonths,
        public bool $retired,
    ) {}

    public function validate(): void
    {
        if ($this->accompanistId === $this->instructorId) {
            throw CatalogFailure::field('accompanistId', 'accompanist must differ from the instructor');
        }
        if (($this->levelFromId === null) !== ($this->levelToId === null)) {
            throw CatalogFailure::field('levelToId', 'a level range needs both ends');
        }
        if ($this->allLevels && $this->levelFromId !== null) {
            throw CatalogFailure::field('allLevels', 'all levels excludes a level range');
        }
        if ($this->ageToMonths !== null && $this->ageFromMonths !== null && $this->ageToMonths < $this->ageFromMonths) {
            throw CatalogFailure::field('ageToMonths', 'the age range ends before it starts');
        }
        if ($this->durationMinutes > 1440 - $this->startTime->minutesSinceMidnight()) {
            throw CatalogFailure::field('durationMinutes', 'class must not run past midnight');
        }
    }
}
