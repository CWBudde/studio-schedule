<?php

declare(strict_types=1);

namespace StudioSchedule\Domain;

use DateTimeImmutable;

/**
 * @phpstan-type NameRef array{id: string, name: string}
 * @phpstan-type RoomRef array{id: string, name: string, position: int}
 * @phpstan-type StaffRef array{id: string, displayName: string}
 * @phpstan-type LevelRef array{id: string, name: string, code: ?string, position: int}
 */
final readonly class PublishedLesson
{
    /**
     * @param RoomRef $room
     * @param NameRef $location
     * @param NameRef|null $courseType
     * @param StaffRef|null $instructor
     * @param StaffRef|null $accompanist
     * @param LevelRef|null $levelFrom
     * @param LevelRef|null $levelTo
     */
    public function __construct(
        public string $id,
        public DateTimeImmutable $startsAt,
        public DateTimeImmutable $endsAt,
        public string $status,
        public string $kind,
        public string $visibility,
        public int $capacity,
        public array $room,
        public array $location,
        public ?string $classId,
        public ?array $courseType,
        public ?string $courseTypeDescription,
        public ?string $locationAddress,
        public ?array $instructor,
        public ?array $accompanist,
        public ?array $levelFrom,
        public ?array $levelTo,
        public bool $allLevels,
        public ?int $stage,
        public ?int $ageFromMonths,
        public ?int $ageToMonths,
        public ?string $renter,
        public ?string $eventId,
        public ?string $eventSlug,
        public ?string $eventStatus,
    ) {}
}
