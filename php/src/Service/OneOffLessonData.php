<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\TimeOfDay;

final readonly class OneOffLessonData
{
    public function __construct(
        public Date $date,
        public TimeOfDay $startTime,
        public int $durationMinutes,
        public string $roomId,
        public ?string $courseTypeId,
        public ?string $instructorId,
        public ?string $accompanistId,
        public int $capacity,
        public string $visibility,
        public string $kind,
        public string $renter,
        public bool $confirmRoomOverlap,
        public bool $confirmPast,
    ) {}

    public function validate(): void
    {
        if ($this->durationMinutes > 1440 - $this->startTime->minutesSinceMidnight()) {
            throw CatalogFailure::field('durationMinutes', 'the lesson must not run past midnight');
        }
        if ($this->capacity > 1000) {
            throw CatalogFailure::field('capacity', 'lesson capacity must not exceed 1000');
        }
        if ($this->kind === 'rental') {
            $this->validateRental();
        } else {
            $this->validateBookable();
        }
        if ($this->accompanistId !== null && $this->accompanistId === $this->instructorId) {
            throw CatalogFailure::field('accompanistId', 'accompanist must differ from the instructor');
        }
    }

    private function validateRental(): void
    {
        $failure = match (true) {
            $this->instructorId !== null => ['instructorId', 'a room rental has no instructor'],
            $this->accompanistId !== null => ['accompanistId', 'a room rental has no accompanist'],
            $this->capacity !== 0 => ['capacity', 'a room rental has no capacity'],
            $this->visibility !== 'hidden' => ['visibility', 'a room rental is visible to staff only'],
            $this->renter === '' => ['renter', 'the renter must be set'],
            default => null,
        };
        if ($failure !== null) {
            throw CatalogFailure::field(...$failure);
        }
    }

    private function validateBookable(): void
    {
        $failure = match (true) {
            $this->courseTypeId === null => ['courseTypeId', 'lesson course type must be set'],
            $this->instructorId === null => ['instructorId', 'lesson instructor must be set'],
            $this->capacity <= 0 => ['capacity', 'lesson capacity must be positive'],
            $this->renter !== '' => ['renter', 'only a room rental has a renter'],
            default => null,
        };
        if ($failure !== null) {
            throw CatalogFailure::field(...$failure);
        }
    }
}
