<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Closure;
use DateInterval;
use DateTimeImmutable;
use DateTimeZone;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Exception\RetryableException;
use Doctrine\DBAL\Exception\UniqueConstraintViolationException;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;
use SensitiveParameter;
use StudioSchedule\Mail\Rendered;
use StudioSchedule\Service\EmailChangeFailure;
use StudioSchedule\Service\SessionRecord;
use Symfony\Component\Uid\Uuid;
use Throwable;

final readonly class EmailChangeStore
{
    private const int MAX_WRITE_ATTEMPTS = 3;
    private const string CHANGE_EMAIL = 'UPDATE accounts SET email = ? WHERE id = ?';

    public function __construct(private Connection $connection) {}

    public function passwordHash(string $accountId): ?string
    {
        $hash = $this->connection->fetchOne('SELECT password_hash FROM account_credentials WHERE account_id = ?', [$this->encodeId($accountId)]);
        assert($hash === false || is_string($hash));

        return $hash === false ? null : $hash;
    }

    /** @param Closure(): ?EmailChangeFailure $write */
    public function transactional(Closure $write): ?EmailChangeFailure
    {
        for ($attempt = 1; ; ++$attempt) {
            try {
                $this->connection->beginTransaction();
                $result = $write();
                $this->connection->commit();
                return $result;
            } catch (Throwable $error) {
                $this->abandonTransaction();
                if ($error instanceof UniqueConstraintViolationException && $error->getQuery()?->getSQL() === self::CHANGE_EMAIL) {
                    return EmailChangeFailure::EmailTaken;
                }
                if (!$error instanceof RetryableException || $attempt >= self::MAX_WRITE_ATTEMPTS) {
                    throw $error;
                }
            }
        }
    }

    /** @return array{personId:string,email:string,locale:?string}|null */
    public function lockEligibleAccount(string $accountId): ?array
    {
        // Match the account-first lock order used by changes to credentials and sessions.
        $row = $this->connection->fetchAssociative(
            'SELECT person_id, email, locale FROM accounts WHERE id = ? AND confirmed_at IS NOT NULL AND disabled_at IS NULL FOR UPDATE',
            [$this->encodeId($accountId)],
        );
        if ($row === false) {
            return null;
        }
        assert(is_string($row['person_id']) && is_string($row['email']) && (is_string($row['locale']) || $row['locale'] === null));

        return ['personId' => $row['person_id'], 'email' => $row['email'], 'locale' => $row['locale']];
    }

    public function lockSessionEnd(string $accountId, #[SensitiveParameter] string $sessionHash): ?DateTimeImmutable
    {
        $digest = $this->digestSql();
        $row = $this->connection->fetchAssociative(
            'SELECT created_at, expires_at FROM sessions '
            . "WHERE account_id = ? AND token_hash = $digest AND impersonator_id IS NULL FOR UPDATE",
            [$this->encodeId($accountId), $sessionHash],
        );
        if ($row === false) {
            return null;
        }
        assert(is_string($row['created_at']) && is_string($row['expires_at']));
        $utc = new DateTimeZone('UTC');
        $created = (new DateTimeImmutable($row['created_at'], $utc))->setTimezone($utc);

        return min(new DateTimeImmutable($row['expires_at'], $utc), $created->add(new DateInterval(SessionRecord::MAX_AGE)));
    }

    public function matchesLockedPassword(string $accountId, #[SensitiveParameter] string $verifiedHash): bool
    {
        $current = $this->connection->fetchOne(
            'SELECT password_hash FROM account_credentials WHERE account_id = ? FOR UPDATE',
            [$this->encodeId($accountId)],
        );

        return is_string($current) && hash_equals($verifiedHash, $current);
    }

    public function emailTaken(#[SensitiveParameter] string $email, string $accountId): bool
    {
        $postgres = $this->isPostgres();
        $lookup = $postgres ? 'lower(email) = lower(CAST(? AS text) COLLATE "C")' : 'email_folded = ?';
        // The unique index resolves any race without locking a second account.
        return $this->connection->fetchOne(
            "SELECT 1 FROM accounts WHERE $lookup AND id <> ?",
            [$postgres ? $email : strtolower($email), $this->encodeId($accountId)],
        ) !== false;
    }

    public function linkAccount(#[SensitiveParameter] string $tokenHash): ?string
    {
        $digest = $this->digestSql();
        $id = $this->connection->fetchOne(
            "SELECT account_id FROM account_tokens WHERE token_hash = $digest AND purpose = 'change-email'",
            [$tokenHash],
        );
        if ($id === false) {
            return null;
        }
        assert(is_string($id));

        return $this->isPostgres() ? $id : Uuid::fromBinary($id)->toRfc4122();
    }

    /** @return array{email:string,expiresAt:DateTimeImmutable}|null */
    public function lockLink(string $accountId, #[SensitiveParameter] string $tokenHash): ?array
    {
        $digest = $this->digestSql();
        $row = $this->connection->fetchAssociative(
            "SELECT email, expires_at FROM account_tokens WHERE account_id = ? AND token_hash = $digest AND purpose = 'change-email' FOR UPDATE",
            [$this->encodeId($accountId), $tokenHash],
        );
        if ($row === false) {
            return null;
        }
        assert(is_string($row['email']) && is_string($row['expires_at']));

        return ['email' => $row['email'], 'expiresAt' => new DateTimeImmutable($row['expires_at'], new DateTimeZone('UTC'))];
    }

    /** @return array{schoolName:string,locale:string,givenName:string} */
    public function mailData(string $personId): array
    {
        $row = $this->connection->fetchAssociative(
            'SELECT s.school_name, s.default_locale, p.given_name FROM settings s CROSS JOIN persons p WHERE s.id = TRUE AND p.id = ?',
            [$personId],
        );
        if ($row === false) {
            throw new RuntimeException('Email change mail requires school settings and the account’s person.');
        }
        assert(is_string($row['school_name']) && is_string($row['default_locale']) && is_string($row['given_name']));

        return ['schoolName' => $row['school_name'], 'locale' => $row['default_locale'], 'givenName' => $row['given_name']];
    }

    public function replaceLink(
        string $accountId,
        #[SensitiveParameter]
        string $tokenHash,
        #[SensitiveParameter]
        string $newEmail,
        DateTimeImmutable $now,
        DateTimeImmutable $expiresAt,
        #[SensitiveParameter]
        Rendered $rendered,
    ): void {
        $id = $this->encodeId($accountId);
        $created = $this->timestamp($now);
        $digest = $this->digestSql();
        $this->connection->executeStatement("DELETE FROM account_tokens WHERE account_id = ? AND purpose = 'change-email'", [$id]);
        $this->connection->executeStatement(
            'INSERT INTO account_tokens (token_hash, account_id, purpose, email, created_at, expires_at) '
            . "VALUES ($digest, ?, 'change-email', ?, ?, ?)",
            [$tokenHash, $id, $newEmail, $created, $this->timestamp($expiresAt)],
        );
        $this->queueMail($newEmail, 'change-email', $rendered, $created);
    }

    public function changeEmail(
        string $accountId,
        #[SensitiveParameter]
        string $oldEmail,
        #[SensitiveParameter]
        string $newEmail,
        DateTimeImmutable $now,
        #[SensitiveParameter]
        Rendered $rendered,
    ): void {
        $id = $this->encodeId($accountId);
        $this->connection->executeStatement(self::CHANGE_EMAIL, [$newEmail, $id]);
        $this->connection->executeStatement('DELETE FROM account_tokens WHERE account_id = ?', [$id]);
        $this->queueMail($oldEmail, 'email-changed', $rendered, $this->timestamp($now));
    }

    private function queueMail(
        #[SensitiveParameter]
        string $recipient,
        string $template,
        #[SensitiveParameter]
        Rendered $mail,
        string $created,
    ): void {
        $this->connection->executeStatement(
            'INSERT INTO outbox_mails (id, recipient, subject, body_text, body_html, template, attempts, created_at, next_attempt_at) '
            . 'VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?)',
            [$this->encodeId(Uuid::v7()->toRfc4122()), $recipient, $mail->subject, $mail->text, $mail->html, $template, $created, $created],
        );
    }

    private function abandonTransaction(): void
    {
        if ($this->connection->isTransactionActive()) {
            try {
                $this->connection->rollBack();
                return;
            } catch (Throwable) {
                // InnoDB may already have rolled back its deadlock victim.
            }
        }
        $this->connection->close();
    }

    private function encodeId(string $id): string
    {
        return $this->isPostgres() ? $id : Uuid::fromString($id)->toBinary();
    }

    private function digestSql(): string
    {
        return $this->isPostgres() ? "decode(?, 'hex')" : 'UNHEX(?)';
    }

    private function timestamp(DateTimeImmutable $instant): string
    {
        return $instant->setTimezone(new DateTimeZone('UTC'))->format($this->isPostgres() ? 'Y-m-d H:i:s.uP' : 'Y-m-d H:i:s.u');
    }

    private function isPostgres(): bool
    {
        return $this->connection->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }
}
