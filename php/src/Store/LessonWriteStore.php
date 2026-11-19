<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Exception\ForeignKeyConstraintViolationException;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;
use SensitiveParameter;
use StudioSchedule\Service\CatalogFailure;
use StudioSchedule\Service\OneOffLessonData;
use StudioSchedule\Service\LessonWritePersistence;
use Symfony\Component\Uid\Uuid;

final readonly class LessonWriteStore implements LessonWritePersistence
{
    public function __construct(private Connection $connection) {}

    public function lock(string $id): bool
    {
        return $this->connection->fetchOne(
            'SELECT id FROM lessons WHERE id = ? FOR UPDATE',
            [$this->encodeId($id)],
            [$this->idType()],
        ) !== false;
    }

    public function hasBookings(string $id): bool
    {
        return $this->connection->fetchOne(
            'SELECT 1 FROM bookings WHERE lesson_id = ? LIMIT 1',
            [$this->encodeId($id)],
            [$this->idType()],
        ) !== false;
    }

    public function delete(string $id): void
    {
        try {
            $this->connection->executeStatement('DELETE FROM lessons WHERE id = ?', [$this->encodeId($id)], [$this->idType()]);
        } catch (ForeignKeyConstraintViolationException) {
            throw new CatalogFailure(409, 'the record is still in use', reason: 'in-use');
        }
    }

    public function redemptionOnBooking(): bool
    {
        return $this->connection->fetchOne('SELECT redemption_mode FROM settings WHERE id = TRUE') === 'booking';
    }

    public function cancelLocked(string $id, string $actor, string $note, DateTimeImmutable $now, bool $onBooking): void
    {
        $lesson = $this->encodeId($id);
        $by = $this->encodeId($actor);
        $instant = Database::sqlInstant($this->connection, $now);
        $type = $this->idType();
        $this->connection->executeStatement(
            "UPDATE lessons SET status = 'canceled', detached = (class_id IS NOT NULL), "
            . 'cancel_note = ?, canceled_by = ?, canceled_at = ? WHERE id = ?',
            [$note === '' ? null : $note, $by, $instant, $lesson],
            [$note === '' ? ParameterType::NULL : ParameterType::STRING, $type, ParameterType::STRING, $type],
        );
        // Old links still identify the requeued entry, but release their reserved place.
        $this->connection->executeStatement(
            "UPDATE waitlist_entries SET status = 'waiting', invited_at = NULL, expires_at = NULL "
            . "WHERE lesson_id = ? AND status = 'invited'",
            [$lesson],
            [$type],
        );
        (new LessonPaymentStore($this->connection))->cancel($lesson, $onBooking);
    }

    public function updateLocked(string $id, OneOffLessonData $data, DateTimeImmutable $start, DateTimeImmutable $end): void
    {
        $this->connection->executeStatement(
            'UPDATE lessons SET room_id = ?, instructor_id = ?, accompanist_id = ?, starts_at = ?, ends_at = ?, '
            . 'kind = ?, visibility = ?, capacity = ?, renter = ?, detached = (class_id IS NOT NULL) WHERE id = ?',
            [
                $this->encodeId($data->roomId), $data->instructorId === null ? null : $this->encodeId($data->instructorId),
                $data->accompanistId === null ? null : $this->encodeId($data->accompanistId),
                Database::sqlInstant($this->connection, $start), Database::sqlInstant($this->connection, $end),
                $data->kind, $data->visibility, $data->capacity, $data->renter === '' ? null : $data->renter,
                $this->encodeId($id),
            ],
            [
                $this->idType(), $data->instructorId === null ? ParameterType::NULL : $this->idType(),
                $data->accompanistId === null ? ParameterType::NULL : $this->idType(),
                ParameterType::STRING, ParameterType::STRING, ParameterType::STRING, ParameterType::STRING, ParameterType::INTEGER,
                $data->renter === '' ? ParameterType::NULL : ParameterType::STRING, $this->idType(),
            ],
        );
    }

    public function recheckPayments(string $id, string $day, DateTimeImmutable $now): void
    {
        (new LessonPaymentStore($this->connection))->move($this->encodeId($id), $day, $now);
    }

    public function restoreLocked(string $id, string $day, DateTimeImmutable $now, bool $onBooking): void
    {
        $lesson = $this->encodeId($id);
        $this->connection->executeStatement(
            "UPDATE lessons SET status = 'scheduled', detached = (class_id IS NOT NULL), "
            . 'cancel_note = NULL, canceled_by = NULL, canceled_at = NULL WHERE id = ?',
            [$lesson],
            [$this->idType()],
        );
        (new LessonPaymentStore($this->connection))->restore($lesson, $onBooking, $day, $now);
    }

    public function inviteLocked(string $entryId, #[SensitiveParameter] string $tokenHash, DateTimeImmutable $now, DateTimeImmutable $expires): void
    {
        $hash = $this->isPostgres() ? "decode(?, 'hex')" : 'UNHEX(?)';
        $changed = $this->connection->executeStatement(
            "UPDATE waitlist_entries SET status = 'invited', invited_at = ?, expires_at = ?, "
            . "invite_token_hash = $hash WHERE id = ? AND status = 'waiting'",
            [Database::sqlInstant($this->connection, $now), Database::sqlInstant($this->connection, $expires), $tokenHash, $this->encodeId($entryId)],
            [ParameterType::STRING, ParameterType::STRING, ParameterType::STRING, $this->idType()],
        );
        if ($changed !== 1) {
            throw new RuntimeException('The lesson waiting list changed before the place could be offered');
        }
    }

    public function bookedRecipients(string $id): array
    {
        /** @var list<array{person_id:?string,guest_name:?string,guest_email:?string}> $rows */
        $rows = $this->connection->fetchAllAssociative(
            "SELECT person_id, guest_name, guest_email FROM bookings WHERE lesson_id = ? AND status <> 'canceled' ORDER BY booked_at, id",
            [$this->encodeId($id)],
            [$this->idType()],
        );

        return array_map(fn(array $row): array => [
            'personId' => $row['person_id'] === null ? null
                : ($this->isPostgres() ? $row['person_id'] : Uuid::fromBinary($row['person_id'])->toRfc4122()),
            'guest' => $row['guest_name'] === null ? null : (
                ['name' => $row['guest_name']] + ($row['guest_email'] === null ? [] : ['email' => $row['guest_email']])
            ),
        ], $rows);
    }

    private function idType(): ParameterType
    {
        return $this->isPostgres() ? ParameterType::STRING : ParameterType::BINARY;
    }

    private function encodeId(string $id): string
    {
        return $this->isPostgres() ? $id : Uuid::fromString($id)->toBinary();
    }

    private function isPostgres(): bool
    {
        return $this->connection->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }
}
