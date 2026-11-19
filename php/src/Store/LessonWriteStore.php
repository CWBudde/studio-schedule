<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use DateTimeImmutable;
use Doctrine\DBAL\ArrayParameterType;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Exception\ForeignKeyConstraintViolationException;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use StudioSchedule\Service\CatalogFailure;
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
        $this->returnVisits($lesson, $onBooking);
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

    private function returnVisits(string $lesson, bool $onBooking): void
    {
        $type = $this->idType();
        $listType = $this->isPostgres() ? ArrayParameterType::STRING : ArrayParameterType::BINARY;
        $bookings = $this->connection->fetchFirstColumn(
            "SELECT b.id FROM bookings b WHERE b.lesson_id = ? AND b.person_id IS NOT NULL AND b.source <> 'member' "
            . 'AND b.trial = FALSE AND ('
            . " (b.status = 'canceled' AND b.late = TRUE)"
            . " OR (b.status <> 'canceled' AND (? = TRUE OR b.status IN ('attended', 'no_show'))))",
            [$lesson, $onBooking],
            [$type, ParameterType::BOOLEAN],
        );
        if ($bookings === []) {
            return;
        }

        // Follow the lesson-to-passes order used by Go. Read redemptions under the pass locks.
        $this->connection->fetchFirstColumn(
            'SELECT id FROM passes WHERE person_id IN (SELECT person_id FROM bookings WHERE id IN (?)) ORDER BY id FOR UPDATE',
            [$bookings],
            [$listType],
        );
        /** @var list<array{pass_id:string,booking_id:string}> $redemptions */
        $redemptions = $this->connection->fetchAllAssociative(
            'SELECT pass_id, booking_id FROM pass_redemptions WHERE booking_id IN (?) ORDER BY booking_id',
            [$bookings],
            [$listType],
        );
        foreach ($redemptions as $redemption) {
            $removed = $this->connection->executeStatement(
                'DELETE FROM pass_redemptions WHERE booking_id = ? AND pass_id = ?',
                [$redemption['booking_id'], $redemption['pass_id']],
                [$type, $type],
            );
            if ($removed === 1) {
                $this->connection->executeStatement(
                    "UPDATE passes SET visits_remaining = visits_remaining + 1 WHERE id = ? AND kind = 'visits' AND visits_remaining < visits_total",
                    [$redemption['pass_id']],
                    [$type],
                );
            }
        }
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
