<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use DateTimeImmutable;
use DateTimeZone;
use Doctrine\DBAL\ArrayParameterType;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;
use SensitiveParameter;
use StudioSchedule\Mail\Rendered;
use StudioSchedule\Service\EventWaitlistPersistence;
use StudioSchedule\Service\SettingsNotFound;
use Symfony\Component\Uid\Uuid;

/**
 * @phpstan-import-type Entry from EventWaitlistPersistence
 */
final readonly class EventWaitlistStore implements EventWaitlistPersistence
{
    private const string DUE = "status = 'invited' AND expires_at <= ?";

    public function __construct(private Connection $db) {}

    public function dueEvents(DateTimeImmutable $now): array
    {
        $rows = $this->db->fetchFirstColumn(
            'SELECT DISTINCT event_id FROM event_registrations WHERE ' . self::DUE . ' ORDER BY event_id',
            [Database::sqlInstant($this->db, $now)],
        );

        return array_map($this->id(...), $rows);
    }

    public function dueInvites(string $eventId, DateTimeImmutable $now): array
    {
        $rows = $this->db->fetchAllAssociative(
            'SELECT id, person_id FROM event_registrations WHERE event_id = ? AND ' . self::DUE . ' ORDER BY created_at, id',
            [$this->encodeId($eventId), Database::sqlInstant($this->db, $now)],
            [$this->idType(), ParameterType::STRING],
        );

        return array_map($this->entry(...), $rows);
    }

    public function waiting(string $eventId): array
    {
        $rows = $this->db->fetchAllAssociative(
            "SELECT id, person_id FROM event_registrations WHERE event_id = ? AND status = 'waiting' AND person_id IS NOT NULL ORDER BY created_at, id",
            [$this->encodeId($eventId)],
            [$this->idType()],
        );

        return array_map($this->entry(...), $rows);
    }

    public function settings(): array
    {
        $expiry = $this->isPostgres() ? Database::intervalMicros('waitlist_invite_expiry') : 'waitlist_invite_expiry';
        /** @var array{school_name:string,default_locale:string,timezone:string,currency:string,invite_micros:int|numeric-string}|false $row */
        $row = $this->db->fetchAssociative("SELECT school_name, default_locale, timezone, currency, $expiry AS invite_micros FROM settings WHERE id = TRUE");
        if ($row === false) {
            throw new SettingsNotFound();
        }

        return [
            'name' => $row['school_name'], 'locale' => $row['default_locale'],
            'timezone' => new DateTimeZone($row['timezone']), 'currency' => $row['currency'], 'inviteMicros' => (int) $row['invite_micros'],
        ];
    }

    public function recipients(array $personIds): array
    {
        $type = $this->isPostgres() ? ArrayParameterType::STRING : ArrayParameterType::BINARY;
        $order = $this->isPostgres() ? 'gp.family_name, gp.given_name' : 'CAST(gp.family_name AS BINARY), CAST(gp.given_name AS BINARY)';
        $query = <<<'SQL'
            SELECT p.id AS person_id,
                CASE WHEN a.confirmed_at IS NOT NULL THEN a.email ELSE COALESCE(NULLIF(p.email, ''), ga.email) END AS email,
                CASE WHEN a.confirmed_at IS NOT NULL THEN a.locale ELSE ga.locale END AS locale,
                CASE WHEN a.confirmed_at IS NOT NULL THEN a.id ELSE ga.id END AS account_id,
                COALESCE(gp.given_name, p.given_name) AS given_name,
                CONCAT(p.given_name, ' ', p.family_name) AS participant
            FROM persons p
            LEFT JOIN accounts a ON a.person_id = p.id
            LEFT JOIN person_guardians g ON g.dependent_id = p.id AND a.confirmed_at IS NULL AND NULLIF(p.email, '') IS NULL
            LEFT JOIN accounts ga ON ga.person_id = g.guardian_id AND ga.confirmed_at IS NOT NULL AND ga.disabled_at IS NULL
            LEFT JOIN persons gp ON gp.id = ga.person_id
            WHERE p.id IN (?) AND a.disabled_at IS NULL
            SQL;
        $recipients = [];
        foreach (array_chunk(array_values(array_unique($personIds)), 1000) as $batch) {
            /** @var list<array{person_id:string,email:?string,locale:?string,account_id:?string,given_name:string,participant:string}> $rows */
            $rows = $this->db->fetchAllAssociative($query . " ORDER BY p.id, $order, gp.id", [array_map($this->encodeId(...), $batch)], [$type]);
            foreach ($rows as $row) {
                if ($row['email'] === null || $row['email'] === '') {
                    continue;
                }
                $recipients[$this->id($row['person_id'])][] = [
                    'email' => $row['email'], 'locale' => $row['locale'] ?? '', 'givenName' => $row['given_name'],
                    'participant' => $row['participant'], 'accountId' => $row['account_id'] === null ? null : $this->id($row['account_id']),
                ];
            }
        }

        return $recipients;
    }

    public function byTokenHash(#[SensitiveParameter] string $tokenHash): ?array
    {
        $hash = $this->isPostgres() ? "decode(?, 'hex')" : 'UNHEX(?)';
        /** @var array{id:string,event_id:string,person_id:?string,expires_at:string,payment:string,price:int|numeric-string}|false $row */
        $row = $this->db->fetchAssociative(
            'SELECT id, event_id, person_id, expires_at, payment, price FROM event_registrations '
            . "WHERE status = 'invited' AND invite_token_hash = $hash",
            [$tokenHash],
        );
        if ($row === false) {
            return null;
        }

        return $this->entry($row) + [
            'eventId' => $this->id($row['event_id']),
            'expiresAt' => Database::instant($row['expires_at']),
            'payment' => $row['payment'],
            'price' => (int) $row['price'],
        ];
    }

    public function lockLivePerson(string $personId): bool
    {
        return $this->db->fetchOne(
            'SELECT id FROM persons WHERE id = ? AND merged_into IS NULL AND anonymized_at IS NULL FOR UPDATE',
            [$this->encodeId($personId)],
            [$this->idType()],
        ) !== false;
    }

    public function finishInvite(string $id, DateTimeImmutable $now, bool $accept): void
    {
        $time = Database::sqlInstant($this->db, $now);
        $eligible = $accept ? "status = 'invited'" : "status IN ('invited', 'waiting')";
        $changed = $this->db->executeStatement(
            'UPDATE event_registrations SET status = ?, updated_at = ?, canceled_at = ?, '
            . "invited_at = NULL, expires_at = NULL, invite_token_hash = NULL WHERE id = ? AND $eligible",
            [$accept ? 'registered' : 'canceled', $time, $accept ? null : $time, $this->encodeId($id)],
            [ParameterType::STRING, ParameterType::STRING, $accept ? ParameterType::NULL : ParameterType::STRING, $this->idType()],
        );
        if ($changed !== 1) {
            throw new RuntimeException('The event invitation changed before it could be settled');
        }
    }

    public function invite(string $id, #[SensitiveParameter] string $tokenHash, DateTimeImmutable $now, DateTimeImmutable $expires): void
    {
        $hash = $this->isPostgres() ? "decode(?, 'hex')" : 'UNHEX(?)';
        $time = Database::sqlInstant($this->db, $now);
        $changed = $this->db->executeStatement(
            "UPDATE event_registrations SET status = 'invited', updated_at = ?, invited_at = ?, expires_at = ?, "
            . "invite_token_hash = $hash WHERE id = ? AND status = 'waiting'",
            [$time, $time, Database::sqlInstant($this->db, $expires), $tokenHash, $this->encodeId($id)],
            [ParameterType::STRING, ParameterType::STRING, ParameterType::STRING, ParameterType::STRING, $this->idType()],
        );
        if ($changed !== 1) {
            throw new RuntimeException('The event waiting list changed before the place could be offered');
        }
    }

    public function enqueueMail(#[SensitiveParameter] string $email, string $template, #[SensitiveParameter] Rendered $mail, DateTimeImmutable $now): void
    {
        (new OutboxStore($this->db))->enqueue($email, $template, $mail, $now);
    }

    public function pushSubscriptions(string $accountId): array
    {
        // Ownership transfer deletes the old subscription and its queued messages.
        $rows = $this->db->fetchFirstColumn(
            'SELECT id FROM push_subscriptions WHERE account_id = ? ORDER BY id FOR UPDATE',
            [$this->encodeId($accountId)],
            [$this->idType()],
        );

        return array_map($this->id(...), $rows);
    }

    public function enqueuePush(array $subscriptionIds, #[SensitiveParameter] string $payload, DateTimeImmutable $now, DateTimeImmutable $expires): void
    {
        $created = Database::sqlInstant($this->db, $now);
        $deadline = Database::sqlInstant($this->db, $expires);
        $idType = $this->idType();
        foreach ($subscriptionIds as $id) {
            $this->db->executeStatement(
                'INSERT INTO push_outbox (id, subscription_id, payload, created_at, expires_at, next_attempt_at) VALUES (?, ?, ?, ?, ?, ?)',
                [$this->encodeId(Uuid::v7()->toRfc4122()), $this->encodeId($id), $payload, $created, $deadline, $created],
                [$idType, $idType, ParameterType::STRING, ParameterType::STRING, ParameterType::STRING, ParameterType::STRING],
            );
        }
    }

    /**
     * @param array<string,mixed> $row
     * @return Entry
     */
    private function entry(array $row): array
    {
        return ['id' => $this->id($row['id']), 'personId' => $row['person_id'] === null ? null : $this->id($row['person_id'])];
    }

    private function id(mixed $value): string
    {
        if (!is_string($value)) {
            throw new RuntimeException('Expected an event waitlist ID');
        }

        return $this->isPostgres() ? $value : Uuid::fromBinary($value)->toRfc4122();
    }

    private function encodeId(string $id): string
    {
        return $this->isPostgres() ? $id : Uuid::fromString($id)->toBinary();
    }

    private function idType(): ParameterType
    {
        return $this->isPostgres() ? ParameterType::STRING : ParameterType::BINARY;
    }

    private function isPostgres(): bool
    {
        return $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }
}
