<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Closure;
use DateTimeImmutable;
use DateTimeZone;
use Doctrine\DBAL\ArrayParameterType;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\PublishedLesson;
use StudioSchedule\Domain\ScheduleRules;
use StudioSchedule\Domain\TimeOfDay;
use StudioSchedule\Service\SchedulePersistence;
use StudioSchedule\Service\SettingsNotFound;
use Symfony\Component\Uid\Uuid;

/** @phpstan-import-type LevelRef from PublishedLesson */
final readonly class ScheduleStore implements SchedulePersistence
{
    private const string LESSONS = <<<'SQL'
        SELECT l.id, l.class_id, l.starts_at, l.ends_at, l.status, l.kind, l.visibility, l.renter, l.event_id, l.cancel_note,
               l.capacity AS lesson_capacity, COALESCE(e.capacity, l.capacity) AS capacity,
               e.slug AS event_slug, e.status AS event_status,
               ct.id AS course_type_id, ct.name AS course_type_name, ct.description AS course_type_description,
               r.id AS room_id, r.name AS room_name, r.position AS room_position,
               lc.id AS location_id, lc.name AS location_name, lc.address AS location_address,
               i.id AS instructor_id, i.display_name AS instructor_name,
               a.id AS accompanist_id, a.display_name AS accompanist_name,
               c.all_levels, c.stage, c.age_from_months, c.age_to_months,
               lf.id AS low_id, lf.name AS low_name, lf.code AS low_code, lf.position AS low_position,
               lt.id AS high_id, lt.name AS high_name, lt.code AS high_code, lt.position AS high_position
        FROM lessons l
        JOIN rooms r ON r.id = l.room_id
        JOIN locations lc ON lc.id = r.location_id
        LEFT JOIN course_types ct ON ct.id = l.course_type_id
        LEFT JOIN staff i ON i.id = l.instructor_id
        LEFT JOIN staff a ON a.id = l.accompanist_id
        LEFT JOIN classes c ON c.id = l.class_id
        LEFT JOIN levels lf ON lf.id = c.level_from_id
        LEFT JOIN levels lt ON lt.id = c.level_to_id
        LEFT JOIN events e ON e.id = l.event_id
        SQL;

    public function __construct(private Connection $connection) {}

    public function inReadSnapshot(Closure $read): mixed
    {
        return Database::inReadSnapshot($this->connection, $read);
    }

    public function rules(): ScheduleRules
    {
        $book = 'booking_cutoff';
        $cancel = 'cancellation_cutoff';
        $invite = 'waitlist_invite_expiry';
        if ($this->isPostgres()) {
            $book = Database::intervalMicros($book);
            $cancel = Database::intervalMicros($cancel);
            $invite = Database::intervalMicros($invite);
        }
        $row = $this->connection->fetchAssociative(
            'SELECT timezone, release_weekday, release_time, release_weeks_ahead, released_through, '
            . "default_visible_days, $book AS booking_micros, $cancel AS cancellation_micros, $invite AS invite_micros FROM settings WHERE id = TRUE",
        );
        if ($row === false) {
            throw new SettingsNotFound();
        }
        $manual = self::optionalString($row['released_through']);

        return new ScheduleRules(
            new DateTimeZone(self::string($row['timezone'])),
            self::integer($row['release_weekday']),
            TimeOfDay::parse(substr(self::string($row['release_time']), 0, 5)),
            self::integer($row['release_weeks_ahead']),
            $manual === null ? null : Date::parse($manual),
            self::integer($row['default_visible_days']),
            self::integer($row['booking_micros']),
            self::integer($row['cancellation_micros']),
            self::integer($row['invite_micros']),
        );
    }

    /** @param Closure(ScheduleRules):ScheduleRules $change */
    public function changeBookingRules(Closure $change): ScheduleRules
    {
        return Database::retryWrite($this->connection, function () use ($change): ScheduleRules {
            $rules = $change($this->lockRules());
            $duration = $this->isPostgres() ? "CAST(? AS BIGINT) * INTERVAL '1 microsecond'" : '?';
            $this->connection->executeStatement(
                'UPDATE settings SET release_weekday = ?, release_time = ?, release_weeks_ahead = ?, released_through = ?, '
                . "default_visible_days = ?, booking_cutoff = $duration, cancellation_cutoff = $duration, waitlist_invite_expiry = $duration",
                [$rules->releaseWeekday, (string) $rules->releaseTime, $rules->releaseWeeksAhead,
                    $rules->releasedThrough === null ? null : (string) $rules->releasedThrough, $rules->defaultVisibleDays,
                    $rules->bookingCutoffMicros, $rules->cancellationCutoffMicros, $rules->waitlistInviteExpiryMicros],
            );

            return $rules;
        });
    }

    public function lockRules(): ScheduleRules
    {
        if ($this->isPostgres()) {
            $this->connection->executeStatement("SET LOCAL DateStyle TO 'ISO, YMD'");
        }
        if ($this->connection->fetchOne('SELECT 1 FROM settings WHERE id = TRUE FOR UPDATE') === false) {
            throw new SettingsNotFound();
        }

        return $this->rules();
    }

    public function lessons(DateTimeImmutable $from, DateTimeImmutable $to, array $filters): array
    {
        $conditions = ['l.starts_at >= ?', 'l.starts_at <= ?'];
        $params = Database::sqlRange($this->connection, $from, $to);
        $types = [ParameterType::STRING, ParameterType::STRING];
        $clauses = [
            'location' => 'r.location_id = ?',
            'instructor' => '(l.instructor_id = ? OR l.accompanist_id = ?)',
            'courseType' => 'l.course_type_id = ?',
            'level' => 'EXISTS (SELECT 1 FROM levels f WHERE f.id = ? AND '
                . '(c.all_levels = TRUE OR f.position BETWEEN LEAST(lf.position, lt.position) AND GREATEST(lf.position, lt.position)))',
        ];
        foreach ($filters as $key => $id) {
            $conditions[] = $clauses[$key];
            $params[] = $this->encodeId($id);
            $types[] = $this->idType();
            if ($key === 'instructor') {
                $params[] = $this->encodeId($id);
                $types[] = $this->idType();
            }
        }
        $rows = $this->connection->fetchAllAssociative(
            self::LESSONS . ' WHERE ' . implode(' AND ', $conditions) . ' ORDER BY l.starts_at, l.id',
            $params,
            $types,
        );

        return array_map($this->publishedLesson(...), $rows);
    }

    public function roomOverlap(string $roomId, DateTimeImmutable $start, DateTimeImmutable $end): ?PublishedLesson
    {
        $row = $this->connection->fetchAssociative(
            self::LESSONS . " WHERE l.room_id = ? AND l.status <> 'canceled' AND l.starts_at < ? AND l.ends_at > ?"
            . ' ORDER BY l.starts_at, l.id LIMIT 1',
            [$this->encodeId($roomId), $this->sqlInstant($end), $this->sqlInstant($start)],
            [$this->idType(), ParameterType::STRING, ParameterType::STRING],
        );

        return $row === false ? null : $this->publishedLesson($row);
    }

    public function lesson(string $id): ?PublishedLesson
    {
        $row = $this->connection->fetchAssociative(
            self::LESSONS . ' WHERE l.id = ?',
            [$this->encodeId($id)],
            [$this->idType()],
        );

        return $row === false ? null : $this->publishedLesson($row);
    }

    /**
     * @param list<string> $lessonIds
     * @return list<PublishedLesson>
     */
    public function lessonsById(array $lessonIds): array
    {
        $lessons = [];
        foreach (array_chunk($lessonIds, 1000) as $ids) {
            $rows = $this->connection->fetchAllAssociative(
                self::LESSONS . ' WHERE l.id IN (?)',
                [$this->encodeIds($ids)],
                [$this->idListType()],
            );
            foreach ($rows as $row) {
                $lessons[] = $this->publishedLesson(['capacity' => $row['lesson_capacity']] + $row);
            }
        }
        usort($lessons, static fn(PublishedLesson $first, PublishedLesson $second): int
            => [$first->startsAt, $first->id] <=> [$second->startsAt, $second->id]);

        return $lessons;
    }

    /**
     * @param list<string>|null $eventIds Null selects the sessions of all events.
     * @return list<PublishedLesson>
     */
    public function eventSessions(?array $eventIds): array
    {
        $where = $eventIds === null ? ' WHERE l.event_id IS NOT NULL' : ' WHERE l.event_id IN (?)';
        $rows = $this->connection->fetchAllAssociative(
            self::LESSONS . $where . ' ORDER BY l.starts_at, l.id',
            $eventIds === null ? [] : [$this->encodeIds($eventIds)],
            $eventIds === null ? [] : [$this->idListType()],
        );

        return array_map(fn(array $row): PublishedLesson => $this->publishedLesson(['capacity' => $row['lesson_capacity']] + $row), $rows);
    }

    /** @return array<string,true> Active staff IDs linked to the account's person. */
    public function activeStaff(string $personId): array
    {
        $ids = $this->connection->fetchFirstColumn(
            'SELECT id FROM staff WHERE person_id = ? AND active = TRUE',
            [$this->encodeId($personId)],
            [$this->idType()],
        );
        $staff = [];
        foreach ($ids as $id) {
            $staff[$this->id($id)] = true;
        }

        return $staff;
    }

    /**
     * @param non-empty-list<string> $staffIds
     * @return list<PublishedLesson>
     */
    public function teachingLessons(DateTimeImmutable $from, DateTimeImmutable $until, array $staffIds): array
    {
        $ids = $this->encodeIds($staffIds);
        $rows = $this->connection->fetchAllAssociative(
            self::LESSONS . ' WHERE l.starts_at >= ? AND l.starts_at < ? AND (l.instructor_id IN (?) OR l.accompanist_id IN (?)) '
            . 'ORDER BY l.starts_at, l.id',
            [$this->sqlInstant($from), $this->sqlInstant($until), $ids, $ids],
            [ParameterType::STRING, ParameterType::STRING, $this->idListType(), $this->idListType()],
        );

        return array_map($this->publishedLesson(...), $rows);
    }

    /**
     * @param non-empty-list<string> $personIds
     * @return array<string,array<string,true>> Lesson ID to actively booked or registered person IDs.
     */
    public function bookedPeople(DateTimeImmutable $from, array $personIds): array
    {
        $ids = $this->encodeIds($personIds);
        $start = $this->sqlInstant($from);
        $rows = $this->connection->fetchAllAssociative(
            'SELECT b.lesson_id, b.person_id FROM bookings b JOIN lessons l ON l.id = b.lesson_id '
            . "WHERE l.starts_at >= ? AND b.person_id IN (?) AND b.status <> 'canceled' "
            . 'UNION '
            . 'SELECT l.id AS lesson_id, r.person_id FROM event_registrations r JOIN events e ON e.id = r.event_id '
            . 'JOIN lessons l ON l.event_id = e.id WHERE l.starts_at >= ? AND r.person_id IN (?) '
            . "AND r.status = 'registered' AND e.status <> 'draft'",
            [$start, $ids, $start, $ids],
            [ParameterType::STRING, $this->idListType(), ParameterType::STRING, $this->idListType()],
        );
        $people = [];
        foreach ($rows as $row) {
            $people[$this->id($row['lesson_id'])][$this->id($row['person_id'])] = true;
        }

        return $people;
    }

    public function activeBookings(array $lessonIds): array
    {
        $counts = $this->connection->fetchAllAssociative(
            "SELECT lesson_id AS id, COUNT(*) AS occupied FROM bookings WHERE lesson_id IN (?) AND status <> 'canceled' GROUP BY lesson_id",
            [$this->encodeIds($lessonIds)],
            [$this->idListType()],
        );

        return $this->counts($counts);
    }

    public function occupiedEventPlaces(array $eventIds, DateTimeImmutable $now): array
    {
        $counts = $this->connection->fetchAllAssociative(
            'SELECT event_id AS id, COUNT(*) AS occupied FROM event_registrations WHERE event_id IN (?) '
            . "AND (status = 'registered' OR (status = 'invited' AND expires_at > ?)) GROUP BY event_id",
            [$this->encodeIds($eventIds), $this->sqlInstant($now)],
            [$this->idListType(), ParameterType::STRING],
        );

        return $this->counts($counts);
    }

    public function family(string $personId): array
    {
        $id = $this->encodeId($personId);
        $name = $this->connection->fetchOne("SELECT CONCAT(given_name, ' ', family_name) FROM persons WHERE id = ?", [$id], [$this->idType()]);
        $family = [$personId => self::string($name)];
        $familyOrder = $this->isPostgres()
            ? 'p.family_name, p.given_name, p.id'
            : 'CAST(p.family_name AS BINARY), CAST(p.given_name AS BINARY), p.id';
        $rows = $this->connection->fetchAllAssociative(
            "SELECT p.id, CONCAT(p.given_name, ' ', p.family_name) AS name FROM person_guardians g "
            . "JOIN persons p ON p.id = g.dependent_id WHERE g.guardian_id = ? ORDER BY $familyOrder",
            [$id],
            [$this->idType()],
        );
        foreach ($rows as $row) {
            $family[$this->id($row['id'])] = self::string($row['name']);
        }

        return $family;
    }

    public function familyBookings(array $lessonIds, array $personIds): array
    {
        $rows = $this->connection->fetchAllAssociative(
            'SELECT lesson_id, person_id, id FROM bookings WHERE lesson_id IN (?) AND person_id IN (?) '
            . "AND status <> 'canceled' ORDER BY booked_at, id",
            [$this->encodeIds($lessonIds), $this->encodeIds($personIds)],
            [$this->idListType(), $this->idListType()],
        );
        $bookings = [];
        foreach ($rows as $r) {
            $bookings[$this->id($r['lesson_id'])][$this->id($r['person_id'])][] = $this->id($r['id']);
        }

        return $bookings;
    }

    public function waitlist(array $lessonIds): array
    {
        $rows = $this->connection->fetchAllAssociative(
            'SELECT lesson_id, id, person_id, status, stay_on_list, expires_at FROM waitlist_entries '
            . "WHERE lesson_id IN (?) AND status IN ('waiting', 'invited') ORDER BY joined_at, id",
            [$this->encodeIds($lessonIds)],
            [$this->idListType()],
        );
        $waitlist = [];
        foreach ($rows as $r) {
            $waitlist[$this->id($r['lesson_id'])][] = [
                'id' => $this->id($r['id']),
                'personId' => $this->id($r['person_id']),
                'status' => self::string($r['status']),
                'stayOnList' => self::boolean($r['stay_on_list']),
                'expiresAt' => $r['expires_at'] === null ? null : self::instant($r['expires_at']),
            ];
        }

        return $waitlist;
    }

    public function lessonAdmin(string $id): array
    {
        $row = $this->connection->fetchAssociative(
            'SELECT l.detached, l.cancel_note, l.canceled_at, a.id AS canceled_by, '
            . "CONCAT(p.given_name, ' ', p.family_name) AS canceled_by_name FROM lessons l "
            . 'LEFT JOIN accounts a ON a.id = l.canceled_by LEFT JOIN persons p ON p.id = a.person_id WHERE l.id = ?',
            [$this->encodeId($id)],
            [$this->idType()],
        );
        if ($row === false) {
            throw new RuntimeException('Lesson is missing inside the read snapshot');
        }

        return [
            'detached' => self::boolean($row['detached']),
            'cancelNote' => self::optionalString($row['cancel_note']),
            'canceledAt' => $row['canceled_at'] === null ? null : self::instant($row['canceled_at']),
            'canceledBy' => $row['canceled_by'] === null ? null : [
                'id' => $this->id($row['canceled_by']),
                'name' => self::string($row['canceled_by_name']),
            ],
        ];
    }

    /** @param array<string, mixed> $row */
    private function publishedLesson(array $row): PublishedLesson
    {
        $low = $this->level($row, 'low');
        $high = $this->level($row, 'high');
        if ($low !== null && $high !== null && [$low['position'], $low['id']] > [$high['position'], $high['id']]) {
            [$low, $high] = [$high, $low];
        }

        return new PublishedLesson(
            id: $this->id($row['id']),
            startsAt: self::instant($row['starts_at']),
            endsAt: self::instant($row['ends_at']),
            status: self::string($row['status']),
            kind: self::string($row['kind']),
            visibility: self::string($row['visibility']),
            capacity: self::integer($row['capacity']),
            room: ['id' => $this->id($row['room_id']), 'name' => self::string($row['room_name']), 'position' => self::integer($row['room_position'])],
            location: ['id' => $this->id($row['location_id']), 'name' => self::string($row['location_name'])],
            classId: $this->optionalId($row['class_id']),
            courseType: $row['course_type_id'] === null ? null : [
                'id' => $this->id($row['course_type_id']), 'name' => self::string($row['course_type_name']),
            ],
            courseTypeDescription: self::optionalString($row['course_type_description']),
            locationAddress: self::optionalString($row['location_address']),
            instructor: $row['instructor_id'] === null ? null : [
                'id' => $this->id($row['instructor_id']), 'displayName' => self::string($row['instructor_name']),
            ],
            accompanist: $row['accompanist_id'] === null ? null : [
                'id' => $this->id($row['accompanist_id']), 'displayName' => self::string($row['accompanist_name']),
            ],
            levelFrom: $low,
            levelTo: $high,
            allLevels: self::boolean($row['all_levels']),
            stage: $row['stage'] === null ? null : self::integer($row['stage']),
            ageFromMonths: $row['age_from_months'] === null ? null : self::integer($row['age_from_months']),
            ageToMonths: $row['age_to_months'] === null ? null : self::integer($row['age_to_months']),
            renter: self::optionalString($row['renter']),
            eventId: $this->optionalId($row['event_id']),
            eventSlug: self::optionalString($row['event_slug']),
            eventStatus: self::optionalString($row['event_status']),
            cancelNote: self::optionalString($row['cancel_note']),
        );
    }

    /**
     * @param array<string, mixed> $row
     * @return LevelRef|null
     */
    private function level(array $row, string $prefix): ?array
    {
        if ($row[$prefix . '_id'] === null) {
            return null;
        }

        return [
            'id' => $this->id($row[$prefix . '_id']),
            'name' => self::string($row[$prefix . '_name']),
            'code' => self::optionalString($row[$prefix . '_code']),
            'position' => self::integer($row[$prefix . '_position']),
        ];
    }

    /**
     * @param list<array<string, mixed>> $rows
     * @return array<string, int>
     */
    private function counts(array $rows): array
    {
        $out = [];
        foreach ($rows as $row) {
            $out[$this->id($row['id'])] = self::integer($row['occupied']);
        }

        return $out;
    }

    private function isPostgres(): bool
    {
        return $this->connection->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }

    private function idType(): ParameterType
    {
        return $this->isPostgres() ? ParameterType::STRING : ParameterType::BINARY;
    }

    private function idListType(): ArrayParameterType
    {
        return $this->isPostgres() ? ArrayParameterType::STRING : ArrayParameterType::BINARY;
    }

    private function encodeId(string $id): string
    {
        return $this->isPostgres() ? $id : Uuid::fromString($id)->toBinary();
    }

    /**
     * @param list<string> $ids
     * @return list<string>
     */
    private function encodeIds(array $ids): array
    {
        return array_map($this->encodeId(...), $ids);
    }

    private function id(mixed $value): string
    {
        $value = self::string($value);

        return $this->isPostgres() ? $value : Uuid::fromBinary($value)->toRfc4122();
    }

    private function optionalId(mixed $value): ?string
    {
        return $value === null ? null : $this->id($value);
    }

    private function sqlInstant(DateTimeImmutable $instant): string
    {
        return Database::sqlInstant($this->connection, $instant);
    }

    private static function instant(mixed $value): DateTimeImmutable
    {
        return Database::instant(self::string($value));
    }

    private static function string(mixed $value): string
    {
        return is_string($value) ? $value : throw new RuntimeException('Expected a schedule text column');
    }

    private static function optionalString(mixed $value): ?string
    {
        return $value === null ? null : self::string($value);
    }

    private static function integer(mixed $value): int
    {
        return is_numeric($value) ? (int) $value : throw new RuntimeException('Expected a schedule integer column');
    }

    private static function boolean(mixed $value): bool
    {
        return in_array($value, [true, 1, '1', 't'], true);
    }
}
