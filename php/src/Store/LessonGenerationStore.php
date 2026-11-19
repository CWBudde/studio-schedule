<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Closure;
use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\ScheduleRules;
use StudioSchedule\Service\LessonGenerationPersistence;
use Symfony\Component\Uid\Uuid;

/**
 * @phpstan-import-type TermRow from LessonGenerationPersistence
 * @phpstan-import-type ClassRow from LessonGenerationPersistence
 * @phpstan-import-type MemberRow from LessonGenerationPersistence
 */
final readonly class LessonGenerationStore implements LessonGenerationPersistence
{
    public function __construct(private Connection $db) {}

    public function generateInTransaction(Closure $generate): int
    {
        return Database::retryWrite($this->db, $generate);
    }

    public function lockRules(): ScheduleRules
    {
        return (new ScheduleStore($this->db))->lockRules();
    }

    public function longestVisibleDays(): int
    {
        /** @var int|numeric-string $days */
        $days = $this->db->fetchOne('SELECT COALESCE(MAX(visible_days), 0) FROM accounts');

        return (int) $days;
    }

    public function lockTerms(Date $from, Date $until): array
    {
        /** @var list<TermRow> $rows */
        $rows = $this->db->fetchAllAssociative(
            'SELECT id, starts_on, ends_on FROM terms WHERE ends_on >= ? AND starts_on < ? ORDER BY starts_on, id FOR UPDATE',
            [(string) $from, (string) $until],
        );

        return $rows;
    }

    public function lockClasses(string $term): array
    {
        $duration = $this->isPostgres() ? Database::intervalMicros('duration') : 'duration';
        /** @var list<ClassRow> $rows */
        $rows = $this->db->fetchAllAssociative(
            'SELECT id, course_type_id, room_id, instructor_id, accompanist_id, weekday, start_time, capacity, '
            . "$duration AS duration_micros FROM classes WHERE term_id = ? AND retired = FALSE ORDER BY weekday, start_time, id FOR UPDATE",
            [$term],
        );

        return $rows;
    }

    public function lockSlotDates(string $class, Date $from): array
    {
        // On InnoDB a locking read sees rows committed by the preceding class writer.
        /** @var list<string> $dates */
        $dates = $this->db->fetchFirstColumn(
            'SELECT slot_date FROM lessons WHERE class_id = ? AND slot_date >= ? ORDER BY slot_date FOR UPDATE',
            [$class, (string) $from],
        );

        return $dates;
    }

    public function lockMembers(string $class): array
    {
        /** @var list<MemberRow> $members */
        $members = $this->db->fetchAllAssociative(
            'SELECT person_id, since, paused_until FROM class_members WHERE class_id = ? ORDER BY person_id FOR UPDATE',
            [$class],
        );

        return $members;
    }

    public function createLesson(array $class, Date $slot, DateTimeImmutable $start, DateTimeImmutable $end): string
    {
        $id = $this->newId();
        $this->db->executeStatement(
            'INSERT INTO lessons (id, class_id, slot_date, course_type_id, room_id, instructor_id, accompanist_id, '
            . 'starts_at, ends_at, status, visibility, kind, capacity) '
            . "VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'scheduled', 'public', 'regular', ?)",
            [$id, $class['id'], (string) $slot, $class['course_type_id'], $class['room_id'], $class['instructor_id'],
                $class['accompanist_id'], Database::sqlInstant($this->db, $start), Database::sqlInstant($this->db, $end), $class['capacity']],
        );

        return $id;
    }

    public function createMemberBooking(string $lesson, string $person, DateTimeImmutable $now, bool $override): void
    {
        $id = $this->newId();
        $at = Database::sqlInstant($this->db, $now);
        $this->db->executeStatement(
            "INSERT INTO bookings (id, lesson_id, person_id, status, trial, booked_at, source) VALUES (?, ?, ?, 'booked', FALSE, ?, 'member')",
            [$id, $lesson, $person, $at],
        );
        $this->db->executeStatement(
            "INSERT INTO booking_events (id, booking_id, at, status, override) VALUES (?, ?, ?, 'booked', ?)",
            [$this->newId(), $id, $at, $override],
            [ParameterType::STRING, ParameterType::STRING, ParameterType::STRING, ParameterType::BOOLEAN],
        );
    }

    private function newId(): string
    {
        $uuid = Uuid::v7();

        return $this->isPostgres() ? $uuid->toRfc4122() : $uuid->toBinary();
    }

    private function isPostgres(): bool
    {
        return $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }
}
