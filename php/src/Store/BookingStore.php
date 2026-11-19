<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Closure;
use DateTimeImmutable;
use Doctrine\DBAL\ArrayParameterType;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use StudioSchedule\Domain\Date;
use StudioSchedule\Domain\Instant;
use StudioSchedule\Service\BookingPersistence;
use Symfony\Component\Uid\Uuid;

/**
 * @phpstan-import-type Booking from BookingPersistence
 * @phpstan-type BookingRow array{id:string,lesson_id:string,person_id:?string,guest_name:?string,guest_email:?string,
 *     status:string,source:string,trial:bool|int|string,trial_note:?string,booked_at:string,canceled_at:?string,late:bool|int|string}
 */
final readonly class BookingStore implements BookingPersistence
{
    private const string COLUMNS = 'id, lesson_id, person_id, guest_name, guest_email, status, source, trial, trial_note, booked_at, canceled_at, late';

    public function __construct(
        private Connection $db,
        private EventStore $transactions,
        private LessonWriteStore $lessons,
        private ParticipantReadStore $participants,
    ) {}

    public function write(Closure $write): mixed
    {
        return $this->transactions->write($write);
    }

    public function bookingLesson(string $booking): ?string
    {
        /** @var string|false $lesson */
        $lesson = $this->db->fetchOne('SELECT lesson_id FROM bookings WHERE id = ?', [$this->encodeId($booking)], [$this->idType()]);

        return $lesson === false ? null : $this->decodeId($lesson);
    }

    public function lock(string $lesson): bool
    {
        return $this->lessons->lock($lesson);
    }

    public function lockClass(string $class): void
    {
        (new TimetableStore($this->db))->lockClassId($class);
    }

    public function classSlot(string $lesson): ?array
    {
        /** @var array{slot_date:string,ends_on:string}|false $row */
        $row = $this->db->fetchAssociative(
            'SELECT l.slot_date, t.ends_on FROM lessons l JOIN classes c ON c.id = l.class_id '
            . 'JOIN terms t ON t.id = c.term_id WHERE l.id = ?',
            [$this->encodeId($lesson)],
            [$this->idType()],
        );

        return $row === false ? null : ['slot' => Date::parse($row['slot_date']), 'termEnd' => Date::parse($row['ends_on'])];
    }

    public function followingLessons(string $class, Date $from, Date $until, DateTimeImmutable $now): array
    {
        /** @var list<string> $ids */
        $ids = $this->db->fetchFirstColumn(
            'SELECT id FROM lessons WHERE class_id = ? AND slot_date >= ? AND slot_date <= ? AND starts_at > ? ORDER BY id',
            [$this->encodeId($class), (string) $from, (string) $until, Database::sqlInstant($this->db, $now)],
            [$this->idType(), ParameterType::STRING, ParameterType::STRING, ParameterType::STRING],
        );

        return array_map($this->decodeId(...), $ids);
    }

    public function activeFor(string $person, array $lessons): array
    {
        $bookings = [];
        $idsType = $this->isPostgres() ? ArrayParameterType::STRING : ArrayParameterType::BINARY;
        foreach (array_chunk($lessons, 1000) as $ids) {
            /** @var list<BookingRow> $rows */
            $rows = $this->db->fetchAllAssociative(
                'SELECT ' . self::COLUMNS . " FROM bookings WHERE person_id = ? AND lesson_id IN (?) AND status <> 'canceled' ORDER BY booked_at, id",
                [$this->encodeId($person), array_map($this->encodeId(...), $ids)],
                [$this->idType(), $idsType],
            );
            foreach ($rows as $row) {
                $bookings[] = $this->bookingRow($row);
            }
        }

        return $bookings;
    }

    public function membershipPause(string $class, string $person): ?array
    {
        /** @var array{paused_until:?string}|false $row */
        $row = $this->db->fetchAssociative(
            'SELECT paused_until FROM class_members WHERE class_id = ? AND person_id = ?',
            [$this->encodeId($class), $this->encodeId($person)],
            [$this->idType(), $this->idType()],
        );

        return $row === false ? null : ['pausedUntil' => $row['paused_until'] === null ? null : Date::parse($row['paused_until'])];
    }

    public function pauseMembership(string $class, string $person, Date $until): void
    {
        $this->db->executeStatement(
            'UPDATE class_members SET paused_until = ? WHERE class_id = ? AND person_id = ?',
            [(string) $until, $this->encodeId($class), $this->encodeId($person)],
            [ParameterType::STRING, $this->idType(), $this->idType()],
        );
    }

    public function redemptionOnBooking(): bool
    {
        return $this->lessons->redemptionOnBooking();
    }

    public function recipient(string $account): ?array
    {
        return (new SessionStore($this->db))->profile($account);
    }

    public function lockLesson(string $lesson, ?string $staffPerson): ?array
    {
        return $this->lock($lesson) ? $this->participants->lesson($lesson, $staffPerson) : null;
    }

    public function booking(string $id): ?array
    {
        /** @var BookingRow|false $row */
        $row = $this->db->fetchAssociative(
            'SELECT ' . self::COLUMNS . ' FROM bookings WHERE id = ?',
            [$this->encodeId($id)],
            [$this->idType()],
        );

        return $row === false ? null : $this->bookingRow($row);
    }

    public function unknownAttendance(string $lesson): array
    {
        /** @var list<BookingRow> $rows */
        $rows = $this->db->fetchAllAssociative(
            'SELECT ' . self::COLUMNS . " FROM bookings WHERE lesson_id = ? AND status = 'booked' ORDER BY booked_at, id",
            [$this->encodeId($lesson)],
            [$this->idType()],
        );

        return array_map($this->bookingRow(...), $rows);
    }

    public function setStatus(string $booking, string $status): void
    {
        $this->db->executeStatement('UPDATE bookings SET status = ? WHERE id = ?', [$status, $this->encodeId($booking)], [ParameterType::STRING, $this->idType()]);
    }

    public function cancelLocked(string $booking, DateTimeImmutable $now, bool $late, bool $released): void
    {
        $this->db->executeStatement(
            "UPDATE bookings SET status = 'canceled', canceled_at = ?, late = ?, released = ? WHERE id = ?",
            [Database::sqlInstant($this->db, $now), $late, $released, $this->encodeId($booking)],
            [ParameterType::STRING, ParameterType::BOOLEAN, ParameterType::BOOLEAN, $this->idType()],
        );
    }

    public function settle(array $charges, array $refunds, string $schoolDate, DateTimeImmutable $now): void
    {
        (new LessonPaymentStore($this->db))->settle(
            array_map(fn(array $booking): array => [
                'id' => $this->encodeId($booking['id']), 'person_id' => $this->encodeId($booking['personId']),
            ], $charges),
            array_map($this->encodeId(...), $refunds),
            $schoolDate,
            $now,
        );
    }

    public function record(string $booking, string $actor, string $status, DateTimeImmutable $now, bool $late = false): void
    {
        $type = $this->idType();
        $this->db->executeStatement(
            'INSERT INTO booking_events (id, booking_id, actor_account_id, at, status, late) VALUES (?, ?, ?, ?, ?, ?)',
            [$this->encodeId(Uuid::v7()->toRfc4122()), $this->encodeId($booking), $this->encodeId($actor), Database::sqlInstant($this->db, $now), $status, $late],
            [$type, $type, $type, ParameterType::STRING, ParameterType::STRING, ParameterType::BOOLEAN],
        );
    }

    /**
     * @param BookingRow $row
     * @return Booking
     */
    private function bookingRow(array $row): array
    {
        return [
            'id' => $this->decodeId($row['id']), 'lessonId' => $this->decodeId($row['lesson_id']),
            'personId' => $row['person_id'] === null ? null : $this->decodeId($row['person_id']),
            'guestName' => $row['guest_name'], 'guestEmail' => $row['guest_email'],
            'status' => $row['status'], 'source' => $row['source'], 'trial' => self::boolean($row['trial']), 'trialNote' => $row['trial_note'],
            'bookedAt' => Instant::format(Database::instant($row['booked_at'])),
            'canceledAt' => $row['canceled_at'] === null ? null : Instant::format(Database::instant($row['canceled_at'])),
            'late' => self::boolean($row['late']),
        ];
    }

    private static function boolean(bool|int|string $value): bool
    {
        return in_array($value, [true, 1, '1', 't'], true);
    }

    private function idType(): ParameterType
    {
        return $this->isPostgres() ? ParameterType::STRING : ParameterType::BINARY;
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
