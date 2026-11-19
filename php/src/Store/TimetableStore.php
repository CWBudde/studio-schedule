<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Closure;
use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Exception\ForeignKeyConstraintViolationException;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\ScheduleRules;
use StudioSchedule\Service\CatalogFailure;
use StudioSchedule\Service\LessonGenerationPersistence;
use StudioSchedule\Service\TermPersistence;
use Symfony\Component\Uid\Uuid;

/**
 * @phpstan-import-type TermRow from LessonGenerationPersistence
 * @phpstan-import-type ClassRow from LessonGenerationPersistence
 * @phpstan-import-type MemberRow from LessonGenerationPersistence
 */
final readonly class TimetableStore implements TermPersistence
{
    public function __construct(private Connection $db) {}

    public function write(Closure $operation): mixed
    {
        return Database::retryWrite($this->db, $operation);
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

    public function lockClasses(string $term, bool $includeRetired = false): array
    {
        $duration = $this->isPostgres() ? Database::intervalMicros('duration') : 'duration';
        $active = $includeRetired ? '' : ' AND retired = FALSE';
        /** @var list<array{id: string, course_type_id: string, room_id: string, instructor_id: string, accompanist_id: ?string,
         *     weekday: int|numeric-string, start_time: string, duration_micros: int|numeric-string, capacity: int|numeric-string, retired: int|string|bool}> $rows */
        $rows = $this->db->fetchAllAssociative(
            'SELECT id, course_type_id, room_id, instructor_id, accompanist_id, weekday, start_time, capacity, retired, '
            . "$duration AS duration_micros FROM classes WHERE term_id = ?$active ORDER BY weekday, start_time, id FOR UPDATE",
            [$term],
        );

        return array_map(static function (array $row): array {
            $row['retired'] = in_array($row['retired'], [true, 1, '1', 't'], true);

            return $row;
        }, $rows);
    }

    public function lockTerm(string $id): array
    {
        /** @var TermRow|false $row */
        $row = $this->db->fetchAssociative('SELECT id, starts_on, ends_on FROM terms WHERE id = ? FOR UPDATE', [$this->encodeId($id)]);

        return $row === false ? throw new CatalogFailure(404, 'resource not found') : $row;
    }

    public function saveTerm(?string $id, string $name, Date $startsOn, Date $endsOn): string
    {
        $fields = ['name' => $name, 'starts_on' => (string) $startsOn, 'ends_on' => (string) $endsOn];
        if ($id === null) {
            $id = Uuid::v7()->toRfc4122();
            $this->db->insert('terms', ['id' => $this->encodeId($id)] + $fields);
        } else {
            $this->db->update('terms', $fields, ['id' => $this->encodeId($id)]);
        }

        return $id;
    }

    public function deleteTerm(string $id): void
    {
        try {
            if ($this->db->delete('terms', ['id' => $this->encodeId($id)]) === 0) {
                throw new CatalogFailure(404, 'resource not found');
            }
        } catch (ForeignKeyConstraintViolationException) {
            throw new CatalogFailure(409, 'the record is still in use', reason: 'in-use');
        }
    }

    public function lockExcludedLessons(string $class, Date $startsOn, Date $endsOn, DateTimeImmutable $now): array
    {
        /** @var list<array{id: string, slot_date: string, started: int|numeric-string}> $rows */
        $rows = $this->db->fetchAllAssociative(
            'SELECT id, slot_date, CASE WHEN starts_at <= ? THEN 1 ELSE 0 END AS started '
            . 'FROM lessons WHERE class_id = ? AND (slot_date < ? OR slot_date > ?) ORDER BY starts_at, id FOR UPDATE',
            [Database::sqlInstant($this->db, $now), $class, (string) $startsOn, (string) $endsOn],
        );

        return array_map(static function (array $row): array {
            $row['started'] = (bool) $row['started'];

            return $row;
        }, $rows);
    }

    public function hasOwnBookings(string $lesson): bool
    {
        return $this->db->fetchOne("SELECT id FROM bookings WHERE lesson_id = ? AND source <> 'member' LIMIT 1 FOR UPDATE", [$lesson]) !== false;
    }

    public function deleteClassLesson(string $lesson): void
    {
        try {
            $this->db->executeStatement("DELETE FROM booking_events WHERE booking_id IN (SELECT id FROM bookings WHERE lesson_id = ? AND source = 'member')", [$lesson]);
            $this->db->executeStatement("DELETE FROM bookings WHERE lesson_id = ? AND source = 'member'", [$lesson]);
            $this->db->delete('lessons', ['id' => $lesson]);
        } catch (ForeignKeyConstraintViolationException) {
            throw new CatalogFailure(409, 'conflict');
        }
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
        return $this->encodeId(Uuid::v7()->toRfc4122());
    }

    private function encodeId(string $id): string
    {
        return $this->isPostgres() ? $id : Uuid::fromString($id)->toBinary();
    }

    private function isPostgres(): bool
    {
        return $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }
}
