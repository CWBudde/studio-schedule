<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Exception\UniqueConstraintViolationException;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;
use StudioSchedule\Domain\Event;
use StudioSchedule\Domain\EventRegistration;
use StudioSchedule\Service\CatalogFailure;
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

    public function find(string $id): ?EventRegistration
    {
        $row = $this->connection->fetchAssociative(
            self::REGISTRATIONS . ' WHERE r.id = ?',
            [$this->encodeId($id)],
            [$this->idType()],
        );

        return $row === false ? null : $this->registration($row);
    }

    public function inFamily(string $guardianId, string $personId, bool $forUpdate = false): bool
    {
        return $guardianId === $personId || (new FamilyStore($this->connection))->isGuardian($guardianId, $personId, $forUpdate);
    }

    public function lockPerson(string $personId, bool $liveOnly): bool
    {
        if ($liveOnly) {
            return (new FamilyStore($this->connection))->lockLivePersons($personId);
        }

        return $this->connection->fetchOne(
            'SELECT id FROM persons WHERE id = ? FOR UPDATE',
            [$this->encodeId($personId)],
            [$this->idType()],
        ) !== false;
    }

    public function requireSchool(): void
    {
        (new FamilyStore($this->connection))->schoolTimezone();
    }

    public function create(
        Event $event,
        ?string $personId,
        ?array $guest,
        string $actorId,
        bool $waiting,
        DateTimeImmutable $now,
    ): EventRegistration {
        $uuid = Uuid::v7()->toRfc4122();
        $at = Database::sqlInstant($this->connection, $now);
        try {
            $this->connection->insert('event_registrations', [
                'id' => $this->encodeId($uuid), 'event_id' => $this->encodeId($event->id),
                'person_id' => $personId === null ? null : $this->encodeId($personId),
                'guest_name' => $guest['name'] ?? null,
                'guest_email' => ($guest['email'] ?? '') === '' ? null : $guest['email'],
                'status' => $waiting ? 'waiting' : 'registered', 'payment' => $event->price === 0 ? 'paid' : 'unpaid',
                'price' => $event->price, 'vat_rate_bp' => $event->vatRate,
                'registered_by' => $this->encodeId($actorId), 'created_at' => $at, 'updated_at' => $at,
            ]);
        } catch (UniqueConstraintViolationException) {
            throw new CatalogFailure(409, 'a record with these values exists already', reason: 'duplicate');
        }

        return $this->find($uuid) ?? throw new RuntimeException('The new event registration was not stored');
    }

    public function actingRecipient(string $accountId, string $participant): array
    {
        $row = $this->connection->fetchAssociative(
            'SELECT a.email, a.locale, p.given_name FROM accounts a JOIN persons p ON p.id = a.person_id WHERE a.id = ?',
            [$this->encodeId($accountId)],
            [$this->idType()],
        );
        if ($row === false) {
            throw new RuntimeException('The registering account no longer exists');
        }

        return [
            'email' => self::text($row['email']), 'locale' => $row['locale'] === null ? '' : self::text($row['locale']),
            'givenName' => self::text($row['given_name']), 'participant' => $participant, 'accountId' => $accountId,
        ];
    }

    public function audit(EventRegistration $registration, string $actorId, bool $added, DateTimeImmutable $now): void
    {
        $person = $registration->person['id'] ?? null;
        $target = $person === null ? null : $this->encodeId($person);
        (new AdminAccountsStore($this->connection))->audit(
            $actorId,
            $added ? 'event-registration-added' : 'event-registration-canceled',
            $target === null ? null : 'person',
            $target,
            $now,
            ['eventId' => $registration->eventId, 'registrationId' => $registration->id],
        );
    }

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
