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
use StudioSchedule\Domain\PublishedLesson;
use StudioSchedule\Domain\ScheduleRules;
use StudioSchedule\Service\CatalogFailure;
use StudioSchedule\Service\ClassData;
use StudioSchedule\Service\LessonGenerationPersistence;
use StudioSchedule\Service\OneOffLessonData;
use StudioSchedule\Service\OneOffLessonPersistence;
use StudioSchedule\Service\TimetableAdminPersistence;
use Symfony\Component\Uid\Uuid;

/**
 * @phpstan-import-type TermRow from LessonGenerationPersistence
 * @phpstan-import-type ClassRow from LessonGenerationPersistence
 * @phpstan-import-type MemberRow from LessonGenerationPersistence
 * @phpstan-import-type ClassLesson from TimetableAdminPersistence
 * @phpstan-import-type ScheduledClassLesson from TimetableAdminPersistence
 */
final readonly class TimetableStore implements TimetableAdminPersistence, OneOffLessonPersistence
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
        $active = $includeRetired ? '' : ' AND retired = FALSE';

        return $this->classRows("term_id = ?$active ORDER BY weekday, start_time, id", $term);
    }

    public function classTerm(string $classId): string
    {
        $term = $this->db->fetchOne('SELECT term_id FROM classes WHERE id = ?', [$this->encodeId($classId)]);
        if (!is_string($term)) {
            throw new CatalogFailure(404, 'resource not found');
        }

        return $this->decodeId($term);
    }

    public function lockClass(string $classId): array
    {
        return $this->classRows('id = ?', $this->encodeId($classId))[0] ?? throw new CatalogFailure(404, 'resource not found');
    }

    /** @return list<ClassRow> */
    private function classRows(string $filter, string $id): array
    {
        $duration = $this->isPostgres() ? Database::intervalMicros('duration') : 'duration';
        /** @var list<array{id: string, course_type_id: string, room_id: string, instructor_id: string, accompanist_id: ?string,
         *     weekday: int|numeric-string, start_time: string, duration_micros: int|numeric-string, capacity: int|numeric-string, retired: int|string|bool}> $rows */
        $rows = $this->db->fetchAllAssociative(
            'SELECT id, course_type_id, room_id, instructor_id, accompanist_id, weekday, start_time, capacity, retired, '
            . "$duration AS duration_micros FROM classes WHERE $filter FOR UPDATE",
            [$id],
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
        return $this->lockLessons($class, $now, [$startsOn, $endsOn]);
    }

    public function writeOneOff(Closure $operation): mixed
    {
        return Database::retryWrite($this->db, $operation, readCommitted: true);
    }

    public function lockLessonReferences(OneOffLessonData $data): void
    {
        if ($data->courseTypeId !== null) {
            $this->requireReference('course_types', $data->courseTypeId, 'courseTypeId', 'course type');
        }
        $this->requireReference('rooms', $data->roomId, 'roomId', 'room');
        if ($data->instructorId !== null) {
            $this->requireReference('staff', $data->instructorId, 'instructorId', 'instructor');
        }
        if ($data->accompanistId !== null) {
            $this->requireReference('staff', $data->accompanistId, 'accompanistId', 'accompanist');
        }
    }

    public function roomOverlap(string $roomId, DateTimeImmutable $start, DateTimeImmutable $end, ?string $excluding = null): ?PublishedLesson
    {
        return (new ScheduleStore($this->db))->roomOverlap($roomId, $start, $end, $excluding);
    }

    public function createOneOffLesson(OneOffLessonData $data, DateTimeImmutable $start, DateTimeImmutable $end): PublishedLesson
    {
        $id = Uuid::v7()->toRfc4122();
        $this->db->insert('lessons', [
            'id' => $this->encodeId($id),
            'course_type_id' => $data->courseTypeId === null ? null : $this->encodeId($data->courseTypeId),
            'room_id' => $this->encodeId($data->roomId),
            'instructor_id' => $data->instructorId === null ? null : $this->encodeId($data->instructorId),
            'accompanist_id' => $data->accompanistId === null ? null : $this->encodeId($data->accompanistId),
            'starts_at' => Database::sqlInstant($this->db, $start), 'ends_at' => Database::sqlInstant($this->db, $end),
            'kind' => $data->kind, 'visibility' => $data->visibility, 'capacity' => $data->capacity,
            'status' => 'scheduled', 'renter' => $data->renter === '' ? null : $data->renter,
            'event_id' => $data->eventId === null ? null : $this->encodeId($data->eventId),
        ]);

        return (new ScheduleStore($this->db))->lesson($id) ?? throw new \RuntimeException('The new lesson was not stored');
    }

    public function saveClass(string $term, ClassData $data, ?string $storedId = null): array
    {
        $course = $this->requireReference('course_types', $data->courseTypeId, 'courseTypeId', 'course type');
        $room = $this->requireReference('rooms', $data->roomId, 'roomId', 'room');
        $instructor = $this->requireReference('staff', $data->instructorId, 'instructorId', 'instructor');
        $accompanist = $data->accompanistId === null ? null : $this->requireReference('staff', $data->accompanistId, 'accompanistId', 'accompanist');
        [$from, $to] = $this->lockClassLevels($data->levelFromId, $data->levelToId);
        $duration = $data->durationMinutes * 60_000_000;
        $class = [
            'id' => $storedId ?? $this->newId(), 'course_type_id' => $course, 'room_id' => $room,
            'instructor_id' => $instructor, 'accompanist_id' => $accompanist,
            'weekday' => $data->weekday, 'start_time' => (string) $data->startTime,
            'capacity' => $data->capacity, 'retired' => $data->retired,
        ];
        $fields = $class + [
            'term_id' => $term, 'level_from_id' => $from, 'level_to_id' => $to, 'all_levels' => $data->allLevels,
            'stage' => $data->stage, 'age_from_months' => $data->ageFromMonths, 'age_to_months' => $data->ageToMonths,
            'duration' => $this->isPostgres() ? "{$data->durationMinutes} minutes" : $duration,
        ];
        $types = ['all_levels' => ParameterType::BOOLEAN, 'retired' => ParameterType::BOOLEAN];
        if ($storedId === null) {
            $this->db->insert('classes', $fields, $types);
        } else {
            unset($fields['id'], $fields['term_id']);
            $this->db->update('classes', $fields, ['id' => $storedId], $types);
        }

        return $class + ['duration_micros' => $duration];
    }

    public function classDocument(string $id): array
    {
        return (new CatalogStore($this->db))->classByStoredId($id);
    }

    public function lockClassId(string $id): string
    {
        $stored = $this->encodeId($id);
        if ($this->db->fetchOne('SELECT id FROM classes WHERE id = ? FOR UPDATE', [$stored]) === false) {
            throw new CatalogFailure(404, 'resource not found');
        }

        return $stored;
    }

    public function deleteClass(string $id): void
    {
        try {
            $this->db->delete('classes', ['id' => $id]);
        } catch (ForeignKeyConstraintViolationException) {
            throw new CatalogFailure(409, 'conflict');
        }
    }

    public function lockClassLessons(string $class, DateTimeImmutable $now): array
    {
        return $this->lockLessons($class, $now, null);
    }

    public function lockClassEditLessons(string $class, Date $from): array
    {
        // Use point locks in lesson ID order, the same order used by pass voiding.
        /** @var list<string> $ids */
        $ids = $this->db->fetchFirstColumn(
            'SELECT id FROM lessons WHERE class_id = ? AND slot_date >= ? ORDER BY id',
            [$class, (string) $from],
        );
        $lessons = [];
        foreach ($ids as $id) {
            /** @var array{id:string,slot_date:string,starts_at:string,ends_at:string,status:string,visibility:string,
             *     kind:string,detached:bool|int|string,course_type_id:?string,room_id:string,instructor_id:?string,
             *     accompanist_id:?string,capacity:int|numeric-string}|false $row */
            $row = $this->db->fetchAssociative(
                'SELECT id, slot_date, starts_at, ends_at, status, visibility, kind, detached, course_type_id, room_id, '
                . 'instructor_id, accompanist_id, capacity FROM lessons WHERE id = ? FOR UPDATE',
                [$id],
            );
            if ($row === false) {
                continue;
            }
            $row['detached'] = in_array($row['detached'], [true, 1, '1', 't'], true);
            $row['lessonId'] = $this->decodeId($row['id']);
            $lessons[] = $row;
        }
        usort($lessons, static fn(array $a, array $b): int => [$a['slot_date'], $a['id']] <=> [$b['slot_date'], $b['id']]);

        return $lessons;
    }

    public function moveClassLesson(string $lesson, array $class, Date $slot, DateTimeImmutable $start, DateTimeImmutable $end): void
    {
        $this->db->update('lessons', [
            'slot_date' => (string) $slot, 'starts_at' => Database::sqlInstant($this->db, $start),
            'ends_at' => Database::sqlInstant($this->db, $end), 'course_type_id' => $class['course_type_id'],
            'room_id' => $class['room_id'], 'instructor_id' => $class['instructor_id'],
            'accompanist_id' => $class['accompanist_id'], 'capacity' => $class['capacity'],
        ], ['id' => $lesson]);
    }

    private function requireReference(string $table, string $id, string $field, string $label): string
    {
        $stored = $this->encodeId($id);
        if ($this->db->fetchOne("SELECT 1 FROM $table WHERE id = ? FOR UPDATE", [$stored]) === false) {
            throw CatalogFailure::field($field, "$label does not exist");
        }

        return $stored;
    }

    /** @return array{?string, ?string} */
    private function lockClassLevels(?string $from, ?string $to): array
    {
        if ($from === null || $to === null) {
            return [null, null];
        }
        $fields = ['levelFromId' => $this->encodeId($from), 'levelToId' => $this->encodeId($to)];
        $ids = array_unique(array_values($fields));
        // Catalogue reorders also lock levels by ID.
        sort($ids, SORT_STRING);
        $found = [];
        foreach ($ids as $id) {
            if ($this->db->fetchOne('SELECT 1 FROM levels WHERE id = ? FOR UPDATE', [$id]) !== false) {
                $found[] = $id;
            }
        }
        foreach ($fields as $field => $id) {
            if (!in_array($id, $found, true)) {
                throw CatalogFailure::field($field, 'level does not exist');
            }
        }

        return [$fields['levelFromId'], $fields['levelToId']];
    }

    /**
     * @param array{Date, Date}|null $outside
     * @return list<ClassLesson>
     */
    private function lockLessons(string $class, DateTimeImmutable $now, ?array $outside): array
    {
        $params = [Database::sqlInstant($this->db, $now), $class];
        $filter = '';
        if ($outside !== null) {
            $filter = ' AND (slot_date < ? OR slot_date > ?)';
            $params[] = (string) $outside[0];
            $params[] = (string) $outside[1];
        }
        /** @var list<array{id: string, slot_date: string, started: int|numeric-string}> $rows */
        $rows = $this->db->fetchAllAssociative(
            'SELECT id, slot_date, CASE WHEN starts_at <= ? THEN 1 ELSE 0 END AS started '
            . "FROM lessons WHERE class_id = ?$filter ORDER BY starts_at, id FOR UPDATE",
            $params,
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

    private function decodeId(string $id): string
    {
        return $this->isPostgres() ? $id : Uuid::fromBinary($id)->toRfc4122();
    }

    private function isPostgres(): bool
    {
        return $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }
}
