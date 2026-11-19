<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Closure;
use DateInterval;
use DateTimeImmutable;
use DateTimeZone;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;
use StudioSchedule\Mail\Rendered;
use StudioSchedule\Service\PasswordResetAccount;
use StudioSchedule\Service\PasswordResetPersistence;
use StudioSchedule\Service\PasswordResetResult;
use Symfony\Component\Uid\Uuid;

final readonly class PasswordResetStore implements PasswordResetPersistence
{
    public function __construct(private Connection $connection) {}

    public function findAccount(#[\SensitiveParameter] string $tokenHash, ?string $lockedAccountId = null): ?PasswordResetAccount
    {
        $postgres = $this->isPostgres();
        $digest = $postgres ? "decode(?, 'hex')" : 'UNHEX(?)';
        $sql = 'SELECT a.id, a.email, a.confirmed_at, a.disabled_at, t.email AS token_email, t.expires_at, '
            . 'CASE WHEN c.account_id IS NULL THEN 0 ELSE 1 END AS has_password '
            . 'FROM account_tokens t JOIN accounts a ON a.id = t.account_id '
            . 'LEFT JOIN account_credentials c ON c.account_id = a.id '
            . "WHERE t.token_hash = $digest AND t.purpose = 'reset-password'";
        $params = [$tokenHash];
        if ($lockedAccountId !== null) {
            // InnoDB must use a locking read here even under REPEATABLE READ.
            $sql .= $postgres ? ' AND a.id = ? FOR UPDATE OF t' : ' AND a.id = ? FOR UPDATE';
            $params[] = $lockedAccountId;
        }

        $row = $this->connection->fetchAssociative($sql, $params);
        if ($row === false) {
            return null;
        }

        assert(is_string($row['id']) && is_string($row['email']) && is_string($row['token_email']) && is_string($row['expires_at']));

        return new PasswordResetAccount(
            $row['id'],
            $row['email'],
            $row['token_email'],
            new DateTimeImmutable($row['expires_at'], new DateTimeZone('UTC')),
            $row['confirmed_at'] !== null,
            $row['disabled_at'] !== null,
            (bool) $row['has_password'],
        );
    }

    public function transactional(Closure $reset): PasswordResetResult
    {
        return Database::retryWrite($this->connection, $reset);
    }

    public function lockAccount(string $accountId): void
    {
        $this->connection->fetchOne('SELECT id FROM accounts WHERE id = ? FOR UPDATE', [$accountId]);
    }

    public function setPassword(PasswordResetAccount $account, #[\SensitiveParameter] string $passwordHash, DateTimeImmutable $now): void
    {
        $id = $account->id;
        $changedAt = $this->timestamp($now);
        if (!$account->confirmed) {
            $this->connection->executeStatement('UPDATE accounts SET confirmed_at = ? WHERE id = ?', [$changedAt, $id]);
        }

        $update = $this->isPostgres() ? 'ON CONFLICT (account_id) DO UPDATE SET' : 'ON DUPLICATE KEY UPDATE';
        $this->connection->executeStatement('INSERT INTO account_credentials (account_id, password_hash, changed_at) '
            . "VALUES (?, ?, ?) $update password_hash = ?, changed_at = ?", [$id, $passwordHash, $changedAt, $passwordHash, $changedAt]);
        $this->endSessions($id, $now);
        $this->connection->executeStatement('DELETE FROM account_tokens WHERE account_id = ?', [$id]);
    }

    public function passwordChangedData(string $accountId): array
    {
        $row = $this->connection->fetchAssociative(
            "SELECT COALESCE(NULLIF(a.locale, ''), s.default_locale) AS locale, s.school_name, p.given_name "
            . 'FROM accounts a JOIN persons p ON p.id = a.person_id CROSS JOIN settings s WHERE a.id = ? AND s.id = TRUE',
            [$accountId],
        );
        if ($row === false) {
            throw new RuntimeException('Password change mail requires school settings and the account’s person');
        }

        assert(is_string($row['locale']) && is_string($row['school_name']) && is_string($row['given_name']));

        return ['locale' => $row['locale'], 'schoolName' => $row['school_name'], 'givenName' => $row['given_name']];
    }

    public function queuePasswordChanged(string $email, Rendered $rendered, DateTimeImmutable $now): void
    {
        (new OutboxStore($this->connection))->enqueue($email, 'password-changed', $rendered, $now);
    }

    private function endSessions(string $accountId, DateTimeImmutable $now): void
    {
        $clause = 'WHERE (account_id = ? OR impersonator_id = ?)';
        // Match Go's earlier of the idle expiry and 30 days after creation.
        $absoluteCutoff = $now->setTimezone(new DateTimeZone('UTC'))->sub(new DateInterval('P30D'));
        $instant = $this->timestamp($now);
        $sessions = $this->connection->fetchAllNumeric(
            "SELECT account_id, impersonator_id FROM sessions $clause "
            . 'AND impersonator_id IS NOT NULL AND expires_at > ? AND created_at > ? ORDER BY id FOR UPDATE',
            [$accountId, $accountId, $instant, $this->timestamp($absoluteCutoff)],
        );
        $this->connection->executeStatement("DELETE FROM sessions $clause", [$accountId, $accountId]);
        $this->connection->executeStatement('DELETE FROM push_subscriptions WHERE account_id = ?', [$accountId]);

        foreach ($sessions as [$targetId, $actorId]) {
            $this->connection->executeStatement(
                'INSERT INTO audit_events (id, occurred_at, actor_id, action, target_kind, target_id) '
                . "VALUES (?, ?, ?, 'impersonation-ended', 'account', ?)",
                [$this->newId(), $instant, $actorId, $targetId],
            );
        }
    }

    private function timestamp(DateTimeImmutable $instant): string
    {
        return $instant->setTimezone(new DateTimeZone('UTC'))->format($this->isPostgres() ? 'Y-m-d H:i:s.uP' : 'Y-m-d H:i:s.u');
    }

    private function newId(): string
    {
        $id = Uuid::v7();

        return $this->isPostgres() ? $id->toRfc4122() : $id->toBinary();
    }

    private function isPostgres(): bool
    {
        return $this->connection->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }
}
