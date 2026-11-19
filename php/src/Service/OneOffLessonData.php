<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use DateInterval;
use DateTimeImmutable;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\PublishedLesson;
use StudioSchedule\Domain\ScheduleRules;
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
        public ?string $eventId = null,
    ) {}

    public function validate(?PublishedLesson $existing = null): void
    {
        if ($this->durationMinutes > 1440 - $this->startTime->minutesSinceMidnight()) {
            throw CatalogFailure::field('durationMinutes', 'the lesson must not run past midnight');
        }
        if ($this->eventId === null && $existing?->eventId === null && $this->capacity > 1000) {
            throw CatalogFailure::field('capacity', 'lesson capacity must not exceed 1000');
        }
        if ($this->kind === 'rental') {
            if ($existing?->classId !== null) {
                throw CatalogFailure::field('kind', 'a lesson of a class cannot be a room rental');
            }
            $this->validateRental();
        } else {
            if ($existing !== null && $existing->courseType === null) {
                throw CatalogFailure::field('kind', 'a room rental without a course type cannot become another kind');
            }
            $this->validateBookable($existing->courseType['id'] ?? $this->courseTypeId);
        }
        if ($this->accompanistId !== null && $this->accompanistId === $this->instructorId) {
            throw CatalogFailure::field('accompanistId', 'accompanist must differ from the instructor');
        }
        if ($existing?->eventId !== null && $this->kind !== 'workshop') {
            throw CatalogFailure::field('kind', "an event's session is a workshop");
        }
    }

    /** @return array{DateTimeImmutable,DateTimeImmutable} */
    public function instants(ScheduleRules $rules): array
    {
        $start = $this->date->at($this->startTime, $rules->timezone);
        $end = $start->add(new DateInterval("PT{$this->durationMinutes}M"));
        if ((int) $start->format('Y') < 1 || $start->format('Y-m-d H:i:s') === '0001-01-01 00:00:00' || (int) $start->format('Y') > 9999) {
            throw CatalogFailure::field('date', 'the lesson start is outside the supported UTC date range');
        }
        if ((int) $end->format('Y') > 9999) {
            throw CatalogFailure::field('durationMinutes', 'the lesson end is outside the supported UTC date range');
        }

        return [$start, $end];
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

    private function validateBookable(?string $courseTypeId): void
    {
        $failure = match (true) {
            $courseTypeId === null => ['courseTypeId', 'lesson course type must be set'],
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
