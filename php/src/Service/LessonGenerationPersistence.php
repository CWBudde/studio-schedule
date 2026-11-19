<?php

declare(strict_types=1);

namespace StudioSchedule\Service;

use Closure;
use DateTimeImmutable;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\ScheduleRules;

/**
 * @phpstan-type TermRow array{id: string, starts_on: string, ends_on: string}
 * @phpstan-type ClassRow array{id: string, course_type_id: string, room_id: string, instructor_id: string, accompanist_id: ?string,
 *     weekday: int|numeric-string, start_time: string, duration_micros: int|numeric-string, capacity: int|numeric-string}
 * @phpstan-type MemberRow array{person_id: string, since: ?string, paused_until: ?string}
 */
interface LessonGenerationPersistence
{
    /** @param Closure():int $generate */
    public function generateInTransaction(Closure $generate): int;

    public function lockRules(): ScheduleRules;

    public function longestVisibleDays(): int;

    /** @return list<TermRow> */
    public function lockTerms(Date $from, Date $until): array;

    /** @return list<ClassRow> */
    public function lockClasses(string $term): array;

    /** @return list<string> */
    public function lockSlotDates(string $class, Date $from): array;

    /** @return list<MemberRow> */
    public function lockMembers(string $class): array;

    /** @param ClassRow $class */
    public function createLesson(array $class, Date $slot, DateTimeImmutable $start, DateTimeImmutable $end): string;

    public function createMemberBooking(string $lesson, string $person, DateTimeImmutable $now, bool $override): void;
}
