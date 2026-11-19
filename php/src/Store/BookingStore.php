<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Closure;
use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
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

    public function cancelLocked(string $booking, DateTimeImmutable $now, bool $late): void
    {
        $this->db->executeStatement(
            "UPDATE bookings SET status = 'canceled', canceled_at = ?, late = ?, released = TRUE WHERE id = ?",
            [Database::sqlInstant($this->db, $now), $late, $this->encodeId($booking)],
            [ParameterType::STRING, ParameterType::BOOLEAN, $this->idType()],
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
