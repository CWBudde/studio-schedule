<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;
use StudioSchedule\Domain\EventRegistration;
use StudioSchedule\Service\EventRegistrationPersistence;
use Symfony\Component\Uid\Uuid;

final readonly class EventRegistrationStore implements EventRegistrationPersistence
{
    private const string REGISTRATIONS = <<<'SQL'
        SELECT r.id, r.event_id, r.person_id, r.guest_name, r.guest_email,
               r.status, r.payment, r.price, r.vat_rate_bp, r.receipt_id, r.cancellation_receipt_id,
               r.created_at, r.updated_at, r.expires_at, r.canceled_at,
               p.given_name, p.family_name, p.anonymized_at
          FROM event_registrations r LEFT JOIN persons p ON p.id = r.person_id
        SQL;

    public function __construct(private Connection $connection) {}

    public function forEvent(string $eventId): array
    {
        $rows = $this->connection->fetchAllAssociative(
            self::REGISTRATIONS . ' WHERE r.event_id = ? ORDER BY r.created_at, r.id',
            [$this->encodeId($eventId)],
            [$this->idType()],
        );

        return array_map($this->registration(...), $rows);
    }

    public function forFamily(string $personId): array
    {
        $person = $this->encodeId($personId);
        $type = $this->idType();
        $self = $this->isPostgres() ? 'CAST(? AS uuid)' : '?';
        $rows = $this->connection->fetchAllAssociative(
            self::REGISTRATIONS . " JOIN events e ON e.id = r.event_id AND e.status <> 'draft' "
            . "WHERE r.person_id IN (SELECT $self UNION ALL SELECT dependent_id FROM person_guardians WHERE guardian_id = ?)",
            [$person, $person],
            [$type, $type],
        );

        return array_map($this->registration(...), $rows);
    }

    public function activeInvite(string $tokenHash, DateTimeImmutable $now): ?array
    {
        $hash = $this->isPostgres() ? "decode(?, 'hex')" : 'UNHEX(?)';
        $row = $this->connection->fetchAssociative(
            'SELECT r.event_id, p.given_name, r.expires_at FROM event_registrations r JOIN persons p ON p.id = r.person_id '
            . "WHERE r.invite_token_hash = $hash AND r.status = 'invited' AND r.expires_at > ?",
            [$tokenHash, Database::sqlInstant($this->connection, $now)],
        );

        return $row === false ? null : [
            'eventId' => $this->id($row['event_id']),
            'givenName' => self::text($row['given_name']),
            'expiresAt' => Database::instant(self::text($row['expires_at'])),
        ];
    }

    /** @param array<string,mixed> $row */
    private function registration(array $row): EventRegistration
    {
        $person = null;
        $guest = null;
        if ($row['person_id'] !== null) {
            $person = [
                'id' => $this->id($row['person_id']),
                'name' => self::text($row['given_name']) . ' ' . self::text($row['family_name']),
            ];
            if ($row['anonymized_at'] !== null) {
                $person['anonymized'] = true;
            }
        } else {
            $guest = ['name' => self::text($row['guest_name'])];
            if ($row['guest_email'] !== null && $row['guest_email'] !== '') {
                $guest['email'] = self::text($row['guest_email']);
            }
        }

        return new EventRegistration(
            id: $this->id($row['id']),
            eventId: $this->id($row['event_id']),
            person: $person,
            guest: $guest,
            status: self::text($row['status']),
            payment: self::text($row['payment']),
            price: self::integer($row['price']),
            vatRate: self::integer($row['vat_rate_bp']),
            receiptId: $row['receipt_id'] === null ? null : $this->id($row['receipt_id']),
            cancellationReceiptId: $row['cancellation_receipt_id'] === null ? null : $this->id($row['cancellation_receipt_id']),
            createdAt: Database::instant(self::text($row['created_at'])),
            updatedAt: Database::instant(self::text($row['updated_at'])),
            expiresAt: $row['expires_at'] === null ? null : Database::instant(self::text($row['expires_at'])),
            canceledAt: $row['canceled_at'] === null ? null : Database::instant(self::text($row['canceled_at'])),
        );
    }

    private function encodeId(string $id): string
    {
        return $this->isPostgres() ? $id : Uuid::fromString($id)->toBinary();
    }

    private function idType(): ParameterType
    {
        return $this->isPostgres() ? ParameterType::STRING : ParameterType::BINARY;
    }

    private function id(mixed $value): string
    {
        $id = self::text($value);

        return $this->isPostgres() ? $id : Uuid::fromBinary($id)->toRfc4122();
    }

    private function isPostgres(): bool
    {
        return $this->connection->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }

    private static function text(mixed $value): string
    {
        return is_string($value) ? $value : throw new RuntimeException('Expected an event registration text column');
    }

    private static function integer(mixed $value): int
    {
        return is_numeric($value) ? (int) $value : throw new RuntimeException('Expected an event registration integer column');
    }
}
