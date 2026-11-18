<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use DateTimeImmutable;
use DateTimeZone;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Service\SessionRecord;
use Symfony\Component\Uid\Uuid;

final readonly class SessionStore
{
    public function __construct(private Connection $connection) {}

    public function findActiveAccount(#[\SensitiveParameter] string $hash): ?SessionRecord
    {
        $row = $this->connection->fetchAssociative(
            'SELECT a.id AS account_id, a.person_id, a.role, a.visible_days, s.impersonator_id, '
            . 's.created_at, s.last_seen_at, s.expires_at FROM sessions s '
            . 'JOIN accounts a ON a.id = s.account_id LEFT JOIN accounts i ON i.id = s.impersonator_id '
            . 'WHERE s.token_hash = ' . $this->binaryParameter() . ' '
            . 'AND a.confirmed_at IS NOT NULL AND a.disabled_at IS NULL '
            . "AND (s.impersonator_id IS NULL OR (a.role <> 'admin' AND i.role = 'admin' "
            . 'AND i.confirmed_at IS NOT NULL AND i.disabled_at IS NULL))',
            [$hash],
        );
        if ($row === false) {
            return null;
        }

        $impersonator = $row['impersonator_id'];
        $visible = $row['visible_days'];
        $zone = new DateTimeZone('UTC');

        return new SessionRecord(
            new SessionPrincipal(
                $this->id($row['account_id']),
                $this->id($row['person_id']),
                self::text($row['role']),
                $visible === null ? null : (int) self::text($visible),
                $impersonator === null ? null : $this->id($impersonator),
            ),
            new DateTimeImmutable(self::text($row['created_at']), $zone),
            new DateTimeImmutable(self::text($row['last_seen_at']), $zone),
            new DateTimeImmutable(self::text($row['expires_at']), $zone),
        );
    }

    public function renew(
        #[\SensitiveParameter]
        string $hash,
        DateTimeImmutable $lastSeen,
        DateTimeImmutable $now,
        DateTimeImmutable $expiresAt,
    ): bool {
        return $this->connection->executeStatement(
            'UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE token_hash = ' . $this->binaryParameter()
            . ' AND last_seen_at = ? AND impersonator_id IS NULL AND expires_at > ?',
            [$this->timestamp($now), $this->timestamp($expiresAt), $hash, $this->timestamp($lastSeen), $this->timestamp($now)],
        ) === 1;
    }

    /**
     * @return array{accountId:string,personId:string,email:string,role:string,givenName:string,familyName:string,locale:?string,teaches:bool}|null
     */
    public function profile(string $accountId): ?array
    {
        $id = $this->isPostgres() ? '?' : 'UNHEX(?)';
        $row = $this->connection->fetchAssociative(
            'SELECT a.id, a.person_id, a.email, a.role, a.locale, p.given_name, p.family_name, '
            . "CASE WHEN a.role IN ('instructor', 'office', 'admin') AND EXISTS ("
            . 'SELECT 1 FROM staff WHERE person_id = p.id AND active = TRUE) THEN 1 ELSE 0 END AS teaches '
            . "FROM accounts a JOIN persons p ON p.id = a.person_id WHERE a.id = $id",
            [$this->isPostgres() ? $accountId : str_replace('-', '', $accountId)],
        );
        if ($row === false) {
            return null;
        }

        return [
            'accountId' => $this->id($row['id']),
            'personId' => $this->id($row['person_id']),
            'email' => self::text($row['email']),
            'role' => self::text($row['role']),
            'givenName' => self::text($row['given_name']),
            'familyName' => self::text($row['family_name']),
            'locale' => $row['locale'] === null ? null : self::text($row['locale']),
            'teaches' => in_array($row['teaches'], [1, '1'], true),
        ];
    }

    private function binaryParameter(): string
    {
        return $this->isPostgres() ? "decode(?, 'hex')" : 'UNHEX(?)';
    }

    private function id(mixed $value): string
    {
        $id = self::text($value);

        return $this->isPostgres() ? $id : Uuid::fromBinary($id)->toRfc4122();
    }

    private function timestamp(DateTimeImmutable $instant): string
    {
        return $instant->setTimezone(new DateTimeZone('UTC'))->format($this->isPostgres() ? 'Y-m-d H:i:s.uP' : 'Y-m-d H:i:s.u');
    }

    private function isPostgres(): bool
    {
        return $this->connection->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }

    private static function text(mixed $value): string
    {
        if (is_string($value) || is_int($value)) {
            return (string) $value;
        }

        throw new RuntimeException('Unexpected session column type');
    }
}
