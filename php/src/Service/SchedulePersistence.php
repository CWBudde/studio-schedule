<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeImmutable;
use StudioSchedule\Domain\PublishedLesson;
use StudioSchedule\Domain\ScheduleRules;

interface SchedulePersistence
{
    /**
     * @template T
     * @param Closure(): T $read
     * @return T
     */
    public function inReadSnapshot(Closure $read): mixed;

    public function rules(): ScheduleRules;

    /** @param Closure(ScheduleRules):ScheduleRules $change */
    public function changeBookingRules(Closure $change): ScheduleRules;

    /**
     * @param array{location?: string, instructor?: string, courseType?: string, level?: string} $filters
     * @return list<PublishedLesson>
     */
    public function lessons(DateTimeImmutable $from, DateTimeImmutable $to, array $filters): array;

    public function lesson(string $id): ?PublishedLesson;

    /**
     * @param list<string> $lessonIds
     * @return array<string, int>
     */
    public function activeBookings(array $lessonIds): array;

    /**
     * @param list<string> $eventIds
     * @return array<string, int>
     */
    public function occupiedEventPlaces(array $eventIds, DateTimeImmutable $now): array;

    /** @return array<string, string> By person ID in family order. */
    public function family(string $personId): array;

    /**
     * @param list<string> $lessonIds
     * @param list<string> $personIds
     * @return array<string, array<string, list<string>>> By lesson ID, then person ID.
     */
    public function familyBookings(array $lessonIds, array $personIds): array;

    /**
     * @param list<string> $lessonIds
     * @return array<string, list<array{id: string, personId: string, status: string, stayOnList: bool, expiresAt: ?DateTimeImmutable}>>
     */
    public function waitlist(array $lessonIds): array;

    /**
     * @return array{detached: bool, cancelNote: ?string, canceledAt: ?DateTimeImmutable, canceledBy: ?array{id: string, name: string}}
     */
    public function lessonAdmin(string $id): array;
}
