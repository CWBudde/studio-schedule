<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Closure;
use DateInterval;
use DateTimeImmutable;
use DateTimeZone;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use Psr\Clock\ClockInterface;
use RuntimeException;
use StudioSchedule\Mail\Rendered;
use StudioSchedule\Service\PasswordResetAccount;
use StudioSchedule\Service\PasswordResetPersistence;
use StudioSchedule\Service\SessionRecord;
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

    public function lockResetRecipient(#[\SensitiveParameter] string $email): ?array
    {
        $postgres = $this->isPostgres();
        $lookup = $postgres ? 'lower(a.email) = lower(CAST(? AS text) COLLATE "C")' : 'a.email_folded = ?';
        $lock = $postgres ? 'FOR UPDATE OF a' : 'FOR UPDATE';
        $row = $this->connection->fetchAssociative(
            'SELECT a.id, a.email, a.confirmed_at, c.account_id AS credential '
            . 'FROM accounts a LEFT JOIN account_credentials c ON c.account_id = a.id '
            . "WHERE $lookup AND a.disabled_at IS NULL $lock",
            [$postgres ? $email : strtolower($email)],
        );
        // An invited account may recover a lost invitation. A self-registration
        // already has a password and must use its confirmation link.
        if ($row === false || ($row['confirmed_at'] === null && $row['credential'] !== null)) {
            return null;
        }

        assert(is_string($row['id']) && is_string($row['email']));

        return ['id' => $row['id'], 'email' => $row['email']];
    }

    public function replaceResetLink(
        string $accountId,
        #[\SensitiveParameter]
        string $email,
        #[\SensitiveParameter]
        string $tokenHash,
        DateTimeImmutable $createdAt,
        DateTimeImmutable $expiresAt,
        #[\SensitiveParameter]
        Rendered $rendered,
        string $template = 'reset-password',
    ): void {
        $this->connection->executeStatement("DELETE FROM account_tokens WHERE account_id = ? AND purpose = 'reset-password'", [$accountId]);
        $digest = $this->isPostgres() ? "decode(?, 'hex')" : 'UNHEX(?)';
        $this->connection->executeStatement(
            'INSERT INTO account_tokens (token_hash, account_id, purpose, email, created_at, expires_at) '
            . "VALUES ($digest, ?, 'reset-password', ?, ?, ?)",
            [$tokenHash, $accountId, $email, $this->timestamp($createdAt), $this->timestamp($expiresAt)],
        );
        $this->queueMail($email, $template, $rendered, $createdAt);
    }

    public function transactional(Closure $write): mixed
    {
        return Database::retryWrite($this->connection, $write);
    }

    public function lockAccount(string $accountId): void
    {
        $this->connection->fetchOne('SELECT id FROM accounts WHERE id = ? FOR UPDATE', [$accountId]);
    }

    public function passwordHashForAccount(string $accountId, bool $locked = false): ?string
    {
        return $this->fetchPasswordHash($this->encodeId($accountId), $locked);
    }

    /** @return array{accountId:string,email:string,sessionId:string,hash:?string,now:DateTimeImmutable}|null */
    public function lockPasswordChange(
        string $accountId,
        #[\SensitiveParameter]
        string $tokenHash,
        ClockInterface $clock,
    ): ?array {
        $accountId = $this->encodeId($accountId);
        // The reset path also locks the account before it updates credentials or revokes sessions.
        $email = $this->connection->fetchOne(
            'SELECT email FROM accounts WHERE id = ? AND confirmed_at IS NOT NULL AND disabled_at IS NULL FOR UPDATE',
            [$accountId],
        );
        if ($email === false) {
            return null;
        }

        $digest = $this->isPostgres() ? "decode(?, 'hex')" : 'UNHEX(?)';
        $session = $this->connection->fetchAssociative(
            'SELECT id, created_at, expires_at FROM sessions '
            . "WHERE token_hash = $digest AND account_id = ? AND impersonator_id IS NULL FOR UPDATE",
            [$tokenHash, $accountId],
        );
        if ($session === false) {
            return null;
        }
        assert(is_string($email) && is_string($session['id']) && is_string($session['created_at']) && is_string($session['expires_at']));

        $hash = $this->fetchPasswordHash($accountId, true);
        $zone = new DateTimeZone('UTC');
        $now = $clock->now()->setTimezone($zone);
        if (new DateTimeImmutable($session['expires_at'], $zone) <= $now
            || new DateTimeImmutable($session['created_at'], $zone) <= $now->sub(new DateInterval(SessionRecord::MAX_AGE))) {
            return null;
        }

        return ['accountId' => $accountId, 'email' => $email, 'sessionId' => $session['id'], 'hash' => $hash, 'now' => $now];
    }

    public function setPassword(PasswordResetAccount $account, #[\SensitiveParameter] string $passwordHash, DateTimeImmutable $now): void
    {
        if (!$account->confirmed) {
            $this->connection->executeStatement('UPDATE accounts SET confirmed_at = ? WHERE id = ?', [$this->timestamp($now), $account->id]);
        }
        $this->replacePassword($account->id, $passwordHash, $now);
    }

    public function replacePassword(
        string $accountId,
        #[\SensitiveParameter]
        string $passwordHash,
        DateTimeImmutable $now,
        ?string $keepSessionId = null,
    ): void {
        $changedAt = $this->timestamp($now);
        $update = $this->isPostgres() ? 'ON CONFLICT (account_id) DO UPDATE SET' : 'ON DUPLICATE KEY UPDATE';
        $this->connection->executeStatement('INSERT INTO account_credentials (account_id, password_hash, changed_at) '
            . "VALUES (?, ?, ?) $update password_hash = ?, changed_at = ?", [$accountId, $passwordHash, $changedAt, $passwordHash, $changedAt]);
        (new SessionStore($this->connection))->endForAccount($accountId, $now, $keepSessionId);
        $this->connection->executeStatement('DELETE FROM account_tokens WHERE account_id = ?', [$accountId]);
    }

    public function passwordChangedData(string $accountId, string $fallbackLocale = ''): array
    {
        $row = $this->connection->fetchAssociative(
            'SELECT a.locale AS account_locale, s.default_locale AS school_locale, s.school_name, p.given_name '
            . 'FROM accounts a JOIN persons p ON p.id = a.person_id CROSS JOIN settings s WHERE a.id = ? AND s.id = TRUE',
            [$accountId],
        );
        if ($row === false) {
            throw new RuntimeException('Password change mail requires school settings and the account’s person');
        }

        $accountLocale = $row['account_locale'] ?? '';
        assert(is_string($accountLocale) && is_string($row['school_locale'])
            && is_string($row['school_name']) && is_string($row['given_name']));
        $locale = $accountLocale !== '' ? $accountLocale : $fallbackLocale;

        return [
            'locale' => $locale !== '' ? $locale : $row['school_locale'],
            'schoolName' => $row['school_name'],
            'givenName' => $row['given_name'],
        ];
    }

    public function queuePasswordChanged(string $email, Rendered $rendered, DateTimeImmutable $now): void
    {
        $this->queueMail($email, 'password-changed', $rendered, $now);
    }

    private function queueMail(
        #[\SensitiveParameter]
        string $email,
        string $template,
        #[\SensitiveParameter]
        Rendered $rendered,
        DateTimeImmutable $now,
    ): void {
        (new OutboxStore($this->connection))->enqueue($email, $template, $rendered, $now);
    }

    private function fetchPasswordHash(string $accountId, bool $locked): ?string
    {
        $sql = 'SELECT password_hash FROM account_credentials WHERE account_id = ?';
        $hash = $this->connection->fetchOne($locked ? $sql . ' FOR UPDATE' : $sql, [$accountId]);
        assert(is_string($hash) || $hash === false);

        return $hash === false ? null : $hash;
    }

    private function timestamp(DateTimeImmutable $instant): string
    {
        return $instant->setTimezone(new DateTimeZone('UTC'))->format($this->isPostgres() ? 'Y-m-d H:i:s.uP' : 'Y-m-d H:i:s.u');
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
