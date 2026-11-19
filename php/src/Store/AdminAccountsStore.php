<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Closure;
use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Exception\ForeignKeyConstraintViolationException;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;
use StudioSchedule\Service\AccountAdministrationFailure;
use Symfony\Component\Uid\Uuid;

/**
 * @phpstan-type AccountEntry array{id:string,email:string,role:string,givenName:string,familyName:string,confirmed:bool,disabled:bool}
 * @phpstan-type LockedAccount array{id:string,personId:string,email:string,role:string,disabled:bool,lastAdmin:bool}
 */
final readonly class AdminAccountsStore
{
    private const string ENTRIES = 'SELECT a.id, a.email, a.role, p.given_name, p.family_name, a.confirmed_at, a.disabled_at '
        . 'FROM accounts a JOIN persons p ON p.id = a.person_id';
    private const string DELETE_ACCOUNT = 'DELETE FROM accounts WHERE id = ?';

    public function __construct(private Connection $connection) {}

    /** @return list<AccountEntry> */
    public function list(): array
    {
        // The account email indexes fold ASCII case and preserve all other bytes.
        $emailOrder = $this->isPostgres() ? 'lower(a.email)' : 'a.email_folded';
        $rows = $this->connection->fetchAllAssociative(self::ENTRIES . " ORDER BY $emailOrder, a.id");

        return array_map($this->project(...), $rows);
    }

    /**
     * @template T
     * @param Closure():T $operation
     * @return T|AccountAdministrationFailure
     */
    public function write(Closure $operation): mixed
    {
        try {
            // After waiting on a person, the checks must see committed changes to the admin set.
            return Database::retryWrite($this->connection, $operation, readCommitted: true);
        } catch (ForeignKeyConstraintViolationException $error) {
            if ($error->getQuery()?->getSQL() !== self::DELETE_ACCOUNT) {
                throw $error;
            }

            // The transaction has already rolled back the session ends, audit retargeting and mail.
            return AccountAdministrationFailure::InUse;
        }
    }

    /** @return LockedAccount|null */
    public function lock(string $id): ?array
    {
        $key = $this->databaseId($id);
        $locked = [];
        // Use Go's person-first order so the guard also works when both backends share PostgreSQL.
        while (true) {
            $target = $this->connection->fetchAssociative('SELECT person_id, role FROM accounts WHERE id = ?', [$key]);
            if ($target === false) {
                return null;
            }
            assert(is_string($target['person_id']));
            $admins = $target['role'] !== 'admin' ? [] : $this->connection->fetchAllAssociative(
                "SELECT id, person_id, disabled_at, confirmed_at FROM accounts WHERE role = 'admin'",
            );
            /** @var list<string> $persons */
            $persons = array_column($admins, 'person_id');
            $persons[] = $target['person_id'];
            $missing = array_diff(array_unique($persons), $locked);
            if ($missing === []) {
                break;
            }

            sort($missing, SORT_STRING);
            foreach ($missing as $person) {
                $this->connection->fetchOne('SELECT id FROM persons WHERE id = ? FOR UPDATE', [$person]);
                $locked[] = $person;
            }
        }

        $row = $this->connection->fetchAssociative('SELECT person_id, email, role, disabled_at FROM accounts WHERE id = ? FOR UPDATE', [$key]);
        if ($row === false) {
            return null;
        }
        assert(is_string($row['person_id']) && is_string($row['email']) && is_string($row['role']));
        $activeAdmins = array_filter($admins, static fn(array $admin): bool => $admin['disabled_at'] === null && $admin['confirmed_at'] !== null);

        return [
            'id' => $key, 'personId' => $row['person_id'], 'email' => $row['email'], 'role' => $row['role'],
            'disabled' => $row['disabled_at'] !== null, 'lastAdmin' => array_column($activeAdmins, 'id') === [$key],
        ];
    }

    /** @return AccountEntry */
    public function entry(string $key): array
    {
        $row = $this->connection->fetchAssociative(self::ENTRIES . ' WHERE a.id = ?', [$key]);
        if ($row === false) {
            throw new RuntimeException('The locked account has no person');
        }

        return $this->project($row);
    }

    public function setDisabled(string $key, ?DateTimeImmutable $at): void
    {
        $timestamp = $at === null ? null : Database::sqlInstant($this->connection, $at);
        $this->connection->executeStatement('UPDATE accounts SET disabled_at = ? WHERE id = ?', [$timestamp, $key]);
    }

    public function setRole(string $key, string $role): void
    {
        $this->connection->executeStatement('UPDATE accounts SET role = ? WHERE id = ?', [$role, $key]);
    }

    public function delete(string $key, string $person): void
    {
        $this->connection->executeStatement(
            "UPDATE audit_events SET target_kind = 'person', target_id = NULL, target_person_id = ? WHERE target_id = ?",
            [$person, $key],
        );
        $this->connection->executeStatement(self::DELETE_ACCOUNT, [$key]);
    }

    /** @param array<string,string> $details */
    public function audit(?string $actor, string $action, string $kind, string $target, DateTimeImmutable $now, array $details = []): void
    {
        $this->connection->executeStatement(
            'INSERT INTO audit_events (id, occurred_at, actor_id, action, target_kind, target_id, target_person_id, details) '
            . 'VALUES (?, ?, ?, ?, ?, ?, ?, ?)',
            [
                $this->databaseId(Uuid::v7()->toRfc4122()), Database::sqlInstant($this->connection, $now),
                $actor === null ? null : $this->databaseId($actor), $action, $kind,
                $kind === 'account' ? $target : null, $kind === 'person' ? $target : null,
                $details === [] ? null : json_encode($details, JSON_THROW_ON_ERROR),
            ],
        );
    }

    /** @param array<string,mixed> $row
     * @return AccountEntry
     */
    private function project(array $row): array
    {
        assert(is_string($row['id']) && is_string($row['email']) && is_string($row['role'])
            && is_string($row['given_name']) && is_string($row['family_name']));

        return [
            'id' => $this->isPostgres() ? $row['id'] : Uuid::fromBinary($row['id'])->toRfc4122(),
            'email' => $row['email'], 'role' => $row['role'], 'givenName' => $row['given_name'], 'familyName' => $row['family_name'],
            'confirmed' => $row['confirmed_at'] !== null, 'disabled' => $row['disabled_at'] !== null,
        ];
    }

    private function databaseId(string $id): string
    {
        return $this->isPostgres() ? $id : Uuid::fromString($id)->toBinary();
    }

    private function isPostgres(): bool
    {
        return $this->connection->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }
}
