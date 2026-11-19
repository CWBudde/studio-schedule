<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use DateTimeImmutable;
use DateTimeZone;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;
use StudioSchedule\Domain\CalendarFeed;
use StudioSchedule\Domain\CalendarFeedProblem;
use StudioSchedule\Domain\CalendarFeedScope;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Service\SettingsNotFound;
use Symfony\Component\Uid\Uuid;

final readonly class CalendarFeedStore
{
    public function __construct(private Connection $connection) {}

    /** @return list<CalendarFeed> */
    public function list(string $accountId): array
    {
        $columns = $this->feedColumns();
        $id = $this->idParam();
        $rows = $this->connection->fetchAllAssociative(
            "SELECT $columns "
            . 'FROM calendar_feeds f LEFT JOIN locations l ON l.id = f.location_id '
            . "WHERE f.account_id = $id AND f.revoked_at IS NULL ORDER BY f.created_at, f.id",
            [$this->idValue($accountId)],
        );

        return array_map($this->readFeed(...), $rows);
    }

    /** @return array{feed:CalendarFeed,owner:SessionPrincipal,locale:?string}|null */
    public function activeByTokenHash(#[\SensitiveParameter] string $digest): ?array
    {
        $columns = $this->feedColumns();
        $hex = $this->isPostgres() ? "decode(?, 'hex')" : 'UNHEX(?)';
        $row = $this->connection->fetchAssociative(
            "SELECT $columns, a.id AS owner_id, a.person_id, a.role, a.visible_days, a.locale "
            . 'FROM calendar_feeds f JOIN accounts a ON a.id = f.account_id LEFT JOIN locations l ON l.id = f.location_id '
            . "WHERE f.token_hash = $hex AND f.revoked_at IS NULL AND a.confirmed_at IS NOT NULL AND a.disabled_at IS NULL",
            [$digest],
        );
        if ($row === false) {
            return null;
        }
        $days = $row['visible_days'];

        return [
            'feed' => $this->readFeed($row),
            'owner' => new SessionPrincipal(
                $this->readId($row['owner_id']),
                $this->readId($row['person_id']),
                self::text($row['role']),
                is_numeric($days) ? (int) $days : null,
                null,
            ),
            'locale' => $row['locale'] === null ? null : self::text($row['locale']),
        ];
    }

    /** @return array{name:string,locale:string,longestVisibleDays:int} */
    public function school(): array
    {
        $row = $this->connection->fetchAssociative(
            'SELECT school_name, default_locale, (SELECT COALESCE(MAX(visible_days), 0) FROM accounts) AS longest_visible_days '
            . 'FROM settings WHERE id = TRUE',
        );
        if ($row === false) {
            throw new SettingsNotFound();
        }
        $days = $row['longest_visible_days'];

        return [
            'name' => self::text($row['school_name']),
            'locale' => self::text($row['default_locale']),
            'longestVisibleDays' => is_numeric($days) ? (int) $days : 0,
        ];
    }

    public function touch(string $id, DateTimeImmutable $now, DateTimeImmutable $today): void
    {
        $param = $this->idParam();
        $this->connection->executeStatement(
            'UPDATE calendar_feeds SET last_used_at = GREATEST(?, created_at) '
            . "WHERE id = $param AND revoked_at IS NULL AND (last_used_at IS NULL OR last_used_at < ?)",
            [$this->instantParam($now), $this->idValue($id), $this->instantParam($today)],
        );
    }

    public function replace(
        SessionPrincipal $principal,
        CalendarFeed $feed,
        #[\SensitiveParameter]
        string $tokenHash,
    ): CalendarFeed|CalendarFeedProblem {
        return $this->connection->transactional(function (Connection $connection) use ($principal, $feed, $tokenHash): CalendarFeed|CalendarFeedProblem {
            $id = $this->idParam();
            $person = $this->idValue($principal->personId);
            $account = $this->idValue($principal->accountId);
            $location = $feed->locationId === null ? null : $this->idValue($feed->locationId);
            $created = $this->instantParam($feed->createdAt);

            // The Go backend takes the same person lock before replacing a feed.
            if ($connection->fetchOne("SELECT id FROM persons WHERE id = $id FOR UPDATE", [$person]) === false) {
                return CalendarFeedProblem::NotFound;
            }
            if ($feed->scope === CalendarFeedScope::Teaching
                && $connection->fetchOne("SELECT 1 FROM staff WHERE person_id = $id AND active = TRUE LIMIT 1", [$person]) === false) {
                return CalendarFeedProblem::Forbidden;
            }

            $name = null;
            if ($location !== null) {
                $name = $connection->fetchOne("SELECT name FROM locations WHERE id = $id", [$location]);
                if ($name === false) {
                    return CalendarFeedProblem::UnknownLocation;
                }
                $name = self::text($name);
            }
            $whereLocation = $this->isPostgres() ? 'location_id IS NOT DISTINCT FROM ?' : 'location_id <=> UNHEX(?)';
            $connection->executeStatement(
                'UPDATE calendar_feeds SET revoked_at = GREATEST(?, created_at) '
                . "WHERE account_id = $id AND scope = ? AND $whereLocation AND revoked_at IS NULL",
                [$created, $account, $feed->scope->value, $location],
            );
            $hex = $this->isPostgres() ? "decode(?, 'hex')" : 'UNHEX(?)';
            $connection->executeStatement(
                'INSERT INTO calendar_feeds (id, account_id, scope, location_id, token_hash, created_at) '
                . "VALUES ($id, $id, ?, $id, $hex, ?)",
                [$this->idValue($feed->id), $account, $feed->scope->value, $location, $tokenHash, $created],
            );

            return new CalendarFeed($feed->id, $feed->scope, $feed->createdAt, $feed->locationId, $name);
        });
    }

    public function revoke(string $accountId, string $feedId, DateTimeImmutable $now): bool
    {
        $id = $this->idParam();

        return $this->connection->executeStatement(
            'UPDATE calendar_feeds SET revoked_at = GREATEST(?, created_at) '
            . "WHERE id = $id AND account_id = $id AND revoked_at IS NULL",
            [$this->instantParam($now), $this->idValue($feedId), $this->idValue($accountId)],
        ) === 1;
    }

    private function feedColumns(): string
    {
        $createdAt = $this->selectInstant('f.created_at');
        $lastUsedAt = $this->selectInstant('f.last_used_at');

        return "f.id, f.scope, f.location_id, l.name AS location_name, $createdAt AS created_at, $lastUsedAt AS last_used_at";
    }

    /** @param array<string,mixed> $row */
    private function readFeed(array $row): CalendarFeed
    {
        $utc = new DateTimeZone('UTC');

        return new CalendarFeed(
            $this->readId($row['id']),
            CalendarFeedScope::from(self::text($row['scope'])),
            new DateTimeImmutable(self::text($row['created_at']), $utc),
            $row['location_id'] === null ? null : $this->readId($row['location_id']),
            $row['location_name'] === null ? null : self::text($row['location_name']),
            $row['last_used_at'] === null ? null : new DateTimeImmutable(self::text($row['last_used_at']), $utc),
        );
    }

    private function selectInstant(string $column): string
    {
        return $this->isPostgres() ? "TO_CHAR($column AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS.US')" : $column;
    }

    private function instantParam(DateTimeImmutable $time): string
    {
        return $time->setTimezone(new DateTimeZone('UTC'))->format($this->isPostgres() ? 'Y-m-d H:i:s.uP' : 'Y-m-d H:i:s.u');
    }

    private function idParam(): string
    {
        return $this->isPostgres() ? '?' : 'UNHEX(?)';
    }

    private function idValue(string $id): string
    {
        return $this->isPostgres() ? $id : str_replace('-', '', $id);
    }

    private function readId(mixed $value): string
    {
        $text = self::text($value);

        return $this->isPostgres() ? $text : Uuid::fromBinary($text)->toRfc4122();
    }

    private function isPostgres(): bool
    {
        return $this->connection->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }

    private static function text(mixed $value): string
    {
        if (!is_string($value)) {
            throw new RuntimeException('Unexpected calendar feed field type');
        }

        return $value;
    }
}
