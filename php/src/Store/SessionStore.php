<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use DateInterval;
use DateTimeImmutable;
use DateTimeZone;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use Psr\Clock\ClockInterface;
use RuntimeException;
use StudioSchedule\Service\ConfirmationFailure;
use StudioSchedule\Service\ImpersonationFailure;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Service\SessionRecord;
use StudioSchedule\Service\SetupInput;
use Symfony\Component\Uid\Uuid;

final readonly class SessionStore
{
    public function __construct(private Connection $connection) {}

    public function purgeExpired(DateTimeImmutable $now): void
    {
        $cutoff = $this->timestamp($now);
        $this->connection->transactional(static function (Connection $database) use ($cutoff): void {
            $database->executeStatement('DELETE FROM sessions WHERE expires_at <= ?', [$cutoff]);
            $database->executeStatement('DELETE FROM account_tokens WHERE expires_at <= ?', [$cutoff]);
        });
    }

    /** @return array{id:string,hash:string,confirmed:bool,disabled:bool}|null */
    public function credentialsForEmail(#[\SensitiveParameter] string $email): ?array
    {
        $email = SetupInput::trim($email);
        if (preg_match('/\A[^\x00]+\z/u', $email) !== 1) {
            return null;
        }
        [$lookup, $parameter] = $this->emailLookup($email);
        $row = $this->connection->fetchAssociative(
            'SELECT a.id, a.confirmed_at, a.disabled_at, c.password_hash '
            . "FROM accounts a JOIN account_credentials c ON c.account_id = a.id WHERE $lookup",
            [$parameter],
        );
        if ($row === false) {
            return null;
        }

        return [
            'id' => $this->id($row['id']),
            'hash' => self::text($row['password_hash']),
            'confirmed' => $row['confirmed_at'] !== null,
            'disabled' => $row['disabled_at'] !== null,
        ];
    }

    public function createForCredentials(
        string $accountId,
        #[\SensitiveParameter]
        string $email,
        #[\SensitiveParameter]
        string $verifiedHash,
        #[\SensitiveParameter]
        ?string $newHash,
        #[\SensitiveParameter]
        string $tokenHash,
        DateTimeImmutable $now,
        DateTimeImmutable $expires,
    ): ?SessionPrincipal {
        return $this->connection->transactional(function (Connection $connection) use (
            $accountId,
            $email,
            $verifiedHash,
            $newHash,
            $tokenHash,
            $now,
            $expires,
        ): ?SessionPrincipal {
            // The reset path locks the account before changing credentials and revoking sessions.
            $account = $this->encodeId($accountId);
            [$lookup, $parameter] = $this->emailLookup(SetupInput::trim($email));
            $row = $connection->fetchAssociative(
                "SELECT a.person_id, a.role, a.visible_days FROM accounts a WHERE a.id = ? AND $lookup "
                . 'AND a.confirmed_at IS NOT NULL AND a.disabled_at IS NULL FOR UPDATE',
                [$account, $parameter],
            );
            if ($row === false) {
                return null;
            }
            $current = $connection->fetchOne('SELECT password_hash FROM account_credentials WHERE account_id = ? FOR UPDATE', [$account]);
            if (!is_string($current) || !hash_equals($verifiedHash, $current)) {
                return null;
            }
            if ($newHash !== null) {
                $connection->executeStatement('UPDATE account_credentials SET password_hash = ? WHERE account_id = ?', [$newHash, $account]);
            }

            return $this->insertSession($connection, $accountId, $row, $tokenHash, $now, $expires);
        });
    }

    /** @return array{id:string,hash:?string,disabled:bool}|null */
    public function confirmationCandidate(#[\SensitiveParameter] string $tokenHash, DateTimeImmutable $now): ?array
    {
        $digest = $this->binaryParameter();
        $row = $this->connection->fetchAssociative(
            'SELECT a.id, a.email, a.disabled_at, t.email AS token_email, t.expires_at, c.password_hash '
            . 'FROM account_tokens t JOIN accounts a ON a.id = t.account_id '
            . 'LEFT JOIN account_credentials c ON c.account_id = a.id '
            . "WHERE t.token_hash = $digest AND t.purpose = 'confirm-email'",
            [$tokenHash],
        );
        if ($row === false || !self::matchesConfirmationLink($row['email'], $row['token_email'], $row['expires_at'], $now)) {
            return null;
        }

        return [
            'id' => $this->id($row['id']),
            'hash' => $row['password_hash'] === null ? null : self::text($row['password_hash']),
            'disabled' => $row['disabled_at'] !== null,
        ];
    }

    public function confirmForCredentials(
        string $accountId,
        #[\SensitiveParameter]
        string $tokenHash,
        #[\SensitiveParameter]
        string $verifiedHash,
        #[\SensitiveParameter]
        ?string $newHash,
        #[\SensitiveParameter]
        string $sessionHash,
        ClockInterface $clock,
    ): SessionRecord|ConfirmationFailure {
        return $this->connection->transactional(function (Connection $connection) use (
            $accountId,
            $tokenHash,
            $verifiedHash,
            $newHash,
            $sessionHash,
            $clock,
        ): SessionRecord|ConfirmationFailure {
            // Registration and password reset lock the account before changing credentials or tokens.
            $account = $this->encodeId($accountId);
            $row = $connection->fetchAssociative(
                'SELECT email, disabled_at, person_id, role, visible_days FROM accounts WHERE id = ? FOR UPDATE',
                [$account],
            );
            if ($row === false) {
                return ConfirmationFailure::InvalidToken;
            }

            $digest = $this->binaryParameter();
            $token = $connection->fetchAssociative(
                'SELECT email, expires_at FROM account_tokens '
                . "WHERE token_hash = $digest AND account_id = ? AND purpose = 'confirm-email' FOR UPDATE",
                [$tokenHash, $account],
            );
            if ($token === false) {
                return ConfirmationFailure::InvalidToken;
            }
            $current = $connection->fetchOne('SELECT password_hash FROM account_credentials WHERE account_id = ? FOR UPDATE', [$account]);
            $now = $clock->now()->setTimezone(new DateTimeZone('UTC'));
            if (!self::matchesConfirmationLink($row['email'], $token['email'], $token['expires_at'], $now)) {
                return ConfirmationFailure::InvalidToken;
            }
            if ($current === false) {
                return ConfirmationFailure::AccountNotFound;
            }
            if (!hash_equals($verifiedHash, self::text($current))) {
                return ConfirmationFailure::InvalidToken;
            }
            if ($row['disabled_at'] !== null) {
                return ConfirmationFailure::InvalidCredentials;
            }

            $connection->executeStatement("DELETE FROM account_tokens WHERE token_hash = $digest", [$tokenHash]);
            $connection->executeStatement(
                'UPDATE accounts SET confirmed_at = ? WHERE id = ? AND confirmed_at IS NULL',
                [$this->timestamp($now), $account],
            );
            if ($newHash !== null) {
                $connection->executeStatement('UPDATE account_credentials SET password_hash = ? WHERE account_id = ?', [$newHash, $account]);
            }

            $expiresAt = $now->add(new DateInterval(SessionRecord::IDLE_TIMEOUT));
            $principal = $this->insertSession($connection, $accountId, $row, $sessionHash, $now, $expiresAt);

            return new SessionRecord($principal, $now, $now, $expiresAt);
        });
    }

    public function rotateImpersonation(
        string $actorId,
        string $targetId,
        bool $starting,
        #[\SensitiveParameter]
        string $oldHash,
        #[\SensitiveParameter]
        string $newHash,
        ClockInterface $clock,
    ): SessionRecord|ImpersonationFailure {
        return $this->connection->transactional(function (Connection $connection) use (
            $actorId,
            $targetId,
            $starting,
            $oldHash,
            $newHash,
            $clock,
        ): SessionRecord|ImpersonationFailure {
            $actor = $this->encodeId($actorId);
            $target = $this->encodeId($targetId);
            // Lock both accounts before the session to serialize with account and credential changes.
            // Shared row locks still allow foreign key checks by concurrent session and audit writes.
            $lock = $this->isPostgres() ? 'FOR SHARE' : 'LOCK IN SHARE MODE';
            $accounts = $connection->fetchAllAssociative(
                "SELECT id, person_id, role, visible_days, confirmed_at, disabled_at FROM accounts WHERE id IN (?, ?) ORDER BY id $lock",
                [$actor, $target],
            );
            $byId = array_column($accounts, null, 'id');
            $actorAccount = $byId[$actor] ?? null;
            $targetAccount = $byId[$target] ?? null;
            if ($actorAccount === null || $actorAccount['role'] !== 'admin' || !self::canSignIn($actorAccount)) {
                return ImpersonationFailure::NoSession;
            }

            $digest = $this->binaryParameter();
            $old = $connection->fetchAssociative(
                "SELECT account_id, impersonator_id, created_at, expires_at FROM sessions WHERE token_hash = $digest FOR UPDATE",
                [$oldHash],
            );
            $zone = new DateTimeZone('UTC');
            $now = $clock->now()->setTimezone($zone);
            if ($old === false
                || $old['account_id'] !== ($starting ? $actor : $target)
                || $old['impersonator_id'] !== ($starting ? null : $actor)
                || new DateTimeImmutable(self::text($old['expires_at']), $zone) <= $now
                || new DateTimeImmutable(self::text($old['created_at']), $zone) <= $now->sub(new DateInterval(SessionRecord::MAX_AGE))) {
                return ImpersonationFailure::NoSession;
            }
            if ($targetAccount === null) {
                return $starting ? ImpersonationFailure::AccountNotFound : ImpersonationFailure::NoSession;
            }
            if ($targetAccount['role'] === 'admin') {
                return $starting ? ImpersonationFailure::NotImpersonable : ImpersonationFailure::NoSession;
            }
            if (!self::canSignIn($targetAccount)) {
                return $starting ? ImpersonationFailure::AccountInactive : ImpersonationFailure::NoSession;
            }

            $connection->executeStatement("DELETE FROM sessions WHERE token_hash = $digest", [$oldHash]);
            $expiresAt = $now->add(new DateInterval($starting ? 'PT1H' : SessionRecord::IDLE_TIMEOUT));
            $principal = $this->insertSession(
                $connection,
                $starting ? $targetId : $actorId,
                $starting ? $targetAccount : $actorAccount,
                $newHash,
                $now,
                $expiresAt,
                $starting ? $actorId : null,
            );
            $action = $starting ? 'impersonation-started' : 'impersonation-ended';
            $this->auditImpersonation($connection, $now, $action, $actor, $target);

            return new SessionRecord($principal, $now, $now, $expiresAt);
        });
    }

    public function end(#[\SensitiveParameter] string $hash, DateTimeImmutable $now): void
    {
        $connection = $this->connection;
        Database::retryWrite($connection, function () use ($connection, $hash, $now): void {
            $target = 'WHERE token_hash = ' . $this->binaryParameter();
            $cutoff = $now->setTimezone(new DateTimeZone('UTC'))->sub(new DateInterval(SessionRecord::MAX_AGE));
            $instant = $this->timestamp($now);
            $impersonation = $connection->fetchNumeric(
                "SELECT account_id, impersonator_id FROM sessions $target "
                . 'AND impersonator_id IS NOT NULL AND expires_at > ? AND created_at > ? FOR UPDATE',
                [$hash, $instant, $this->timestamp($cutoff)],
            );
            $connection->executeStatement("DELETE FROM sessions $target", [$hash]);
            if ($impersonation !== false) {
                $this->auditImpersonation($connection, $now, 'impersonation-ended', self::text($impersonation[1]), self::text($impersonation[0]));
            }
        });
    }

    /** Revoke sessions and push registrations for a locked account in its write transaction. IDs use the database's representation. */
    public function endForAccount(string $accountId, DateTimeImmutable $now, ?string $keepSessionId = null): void
    {
        $clause = 'WHERE (account_id = ? OR impersonator_id = ?)';
        $params = [$accountId, $accountId];
        if ($keepSessionId !== null) {
            $clause .= ' AND id <> ?';
            $params[] = $keepSessionId;
        }
        $absoluteCutoff = $now->setTimezone(new DateTimeZone('UTC'))->sub(new DateInterval(SessionRecord::MAX_AGE));
        $sessions = $this->connection->fetchAllNumeric(
            "SELECT account_id, impersonator_id FROM sessions $clause "
            . 'AND impersonator_id IS NOT NULL AND expires_at > ? AND created_at > ? ORDER BY id FOR UPDATE',
            [...$params, $this->timestamp($now), $this->timestamp($absoluteCutoff)],
        );
        $this->connection->executeStatement("DELETE FROM sessions $clause", $params);
        $this->connection->executeStatement('DELETE FROM push_subscriptions WHERE account_id = ?', [$accountId]);

        foreach ($sessions as [$target, $actor]) {
            $this->auditImpersonation($this->connection, $now, 'impersonation-ended', self::text($actor), self::text($target));
        }
    }

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

    /** @param array<string,mixed> $account */
    private static function canSignIn(array $account): bool
    {
        return $account['confirmed_at'] !== null && $account['disabled_at'] === null;
    }

    public function updateLocale(string $accountId, ?string $locale): bool
    {
        $id = $this->encodeId($accountId);
        $updated = $this->connection->executeStatement('UPDATE accounts SET locale = ? WHERE id = ?', [$locale, $id]);

        // MySQL can report zero affected rows for an unchanged preference.
        return (int) $updated !== 0 || $this->connection->fetchOne('SELECT id FROM accounts WHERE id = ?', [$id]) !== false;
    }

    private function auditImpersonation(
        Connection $connection,
        DateTimeImmutable $now,
        string $action,
        string $actor,
        string $target,
    ): void {
        // The authenticated HTTP API is the default audit channel, represented by absent details.
        $connection->executeStatement(
            'INSERT INTO audit_events (id, occurred_at, actor_id, action, target_kind, target_id) '
            . "VALUES (?, ?, ?, ?, 'account', ?)",
            [$this->encodeId(Uuid::v7()->toRfc4122()), $this->timestamp($now), $actor, $action, $target],
        );
    }

    private static function matchesConfirmationLink(mixed $accountEmail, mixed $tokenEmail, mixed $expiry, DateTimeImmutable $now): bool
    {
        return $accountEmail === $tokenEmail && new DateTimeImmutable(self::text($expiry), new DateTimeZone('UTC')) > $now;
    }

    /** @param array<string,mixed> $account */
    private function insertSession(
        Connection $connection,
        string $accountId,
        array $account,
        #[\SensitiveParameter]
        string $tokenHash,
        DateTimeImmutable $now,
        DateTimeImmutable $expires,
        ?string $impersonatorId = null,
    ): SessionPrincipal {
        $digest = $this->binaryParameter();
        $seen = $this->timestamp($now);
        $connection->executeStatement(
            'INSERT INTO sessions (id, account_id, token_hash, created_at, last_seen_at, expires_at, impersonator_id) '
            . "VALUES (?, ?, $digest, ?, ?, ?, ?)",
            [
                $this->encodeId(Uuid::v7()->toRfc4122()), $this->encodeId($accountId), $tokenHash,
                $seen, $seen, $this->timestamp($expires), $impersonatorId === null ? null : $this->encodeId($impersonatorId),
            ],
        );

        return new SessionPrincipal(
            $accountId,
            $this->id($account['person_id']),
            self::text($account['role']),
            $account['visible_days'] === null ? null : (int) self::text($account['visible_days']),
            $impersonatorId,
        );
    }

    /** @return array{string,string} SQL lookup and its parameter. */
    private function emailLookup(#[\SensitiveParameter] string $email): array
    {
        return $this->isPostgres()
            ? ['lower(a.email) = lower(CAST(? AS text) COLLATE "C")', $email]
            : ['a.email_folded = ?', strtolower($email)];
    }

    private function encodeId(string $value): string
    {
        return $this->isPostgres() ? $value : Uuid::fromString($value)->toBinary();
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
