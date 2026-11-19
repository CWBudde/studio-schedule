<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Closure;
use DateTimeImmutable;
use DateTimeZone;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Exception\RetryableException;
use Doctrine\DBAL\Exception\UniqueConstraintViolationException;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use RuntimeException;
use StudioSchedule\Mail\Rendered;
use Symfony\Component\Uid\Uuid;
use Throwable;

final readonly class RegistrationStore
{
    private const int MAX_WRITE_ATTEMPTS = 3;

    public function __construct(private Connection $connection) {}

    /** @param Closure(): bool $write */
    public function transactional(Closure $write): bool
    {
        for ($attempt = 1; ; ++$attempt) {
            try {
                $this->connection->beginTransaction();
                $result = $write();
                $this->connection->commit();

                return $result;
            } catch (Throwable $error) {
                $this->abandonTransaction();
                // Two fresh registrations for an address can both miss the row.
                // Reload after a uniqueness failure or an InnoDB gap-lock deadlock.
                $retryable = $error instanceof RetryableException || $error instanceof UniqueConstraintViolationException;
                if (!$retryable || $attempt >= self::MAX_WRITE_ATTEMPTS) {
                    throw $error;
                }
            }
        }
    }

    /** @return array{id: string, email: string, locale: ?string}|null */
    public function savePending(
        string $givenName,
        string $familyName,
        #[\SensitiveParameter]
        string $email,
        ?string $locale,
    ): ?array {
        $postgres = $this->isPostgres();
        $lookup = $postgres ? 'lower(email) = lower(CAST(? AS text) COLLATE "C")' : 'email_folded = ?';
        $key = $postgres ? $email : strtolower($email);
        // Account administration locks the person first. Match that order
        // so renewing a pending registration can safely update the name.
        $lockedPerson = $this->connection->fetchOne(
            "SELECT id FROM persons WHERE id = (SELECT person_id FROM accounts WHERE $lookup) FOR UPDATE",
            [$key],
        );
        $row = $this->connection->fetchAssociative(
            "SELECT id, person_id, email, confirmed_at, locale FROM accounts WHERE $lookup FOR UPDATE",
            [$key],
        );

        if ($row === false) {
            $personId = $this->newId();
            $accountId = $this->newId();
            $this->connection->executeStatement(
                'INSERT INTO persons (id, given_name, family_name) VALUES (?, ?, ?)',
                [$personId, $givenName, $familyName],
            );
            $this->connection->executeStatement(
                "INSERT INTO accounts (id, person_id, email, role, locale) VALUES (?, ?, ?, 'member', ?)",
                [$accountId, $personId, $email, $locale],
            );

            return ['id' => $accountId, 'email' => $email, 'locale' => $locale];
        }

        assert(is_string($row['id']) && is_string($row['person_id']) && is_string($row['email']));
        assert(is_string($row['locale']) || $row['locale'] === null);
        if ($row['person_id'] !== $lockedPerson) {
            throw new RegistrationChanged('The registered account changed during the person lookup.');
        }

        // Lock accounts before credentials or tokens, as in reset and confirmation.
        if ($row['confirmed_at'] !== null
            || $this->connection->fetchOne('SELECT account_id FROM account_credentials WHERE account_id = ? FOR UPDATE', [$row['id']]) === false) {
            return null;
        }

        $this->connection->executeStatement(
            'UPDATE persons SET given_name = ?, family_name = ? WHERE id = ?',
            [$givenName, $familyName, $row['person_id']],
        );
        if ($locale !== null && $locale !== $row['locale']) {
            $this->connection->executeStatement('UPDATE accounts SET locale = ? WHERE id = ?', [$locale, $row['id']]);
        }

        return ['id' => $row['id'], 'email' => $row['email'], 'locale' => $locale ?? $row['locale']];
    }

    /** @return array{name: string, locale: string} */
    public function school(): array
    {
        $row = $this->connection->fetchAssociative('SELECT school_name, default_locale FROM settings WHERE id = TRUE');
        if ($row === false) {
            throw new RuntimeException('Registration mail requires school settings.');
        }
        assert(is_string($row['school_name']) && is_string($row['default_locale']));

        return ['name' => $row['school_name'], 'locale' => $row['default_locale']];
    }

    public function writeConfirmation(
        string $accountId,
        #[\SensitiveParameter]
        string $email,
        #[\SensitiveParameter]
        string $passwordHash,
        #[\SensitiveParameter]
        string $tokenHash,
        DateTimeImmutable $createdAt,
        DateTimeImmutable $expiresAt,
        #[\SensitiveParameter]
        Rendered $rendered,
    ): void {
        $postgres = $this->isPostgres();
        $now = $this->timestamp($createdAt);
        $update = $postgres ? 'ON CONFLICT (account_id) DO UPDATE SET' : 'ON DUPLICATE KEY UPDATE';
        $this->connection->executeStatement(
            "INSERT INTO account_credentials (account_id, password_hash, changed_at) VALUES (?, ?, ?) $update password_hash = ?, changed_at = ?",
            [$accountId, $passwordHash, $now, $passwordHash, $now],
        );

        $this->connection->executeStatement("DELETE FROM account_tokens WHERE account_id = ? AND purpose = 'confirm-email'", [$accountId]);
        $digest = $postgres ? "decode(?, 'hex')" : 'UNHEX(?)';
        $this->connection->executeStatement(
            "INSERT INTO account_tokens (token_hash, account_id, purpose, email, created_at, expires_at) VALUES ($digest, ?, 'confirm-email', ?, ?, ?)",
            [$tokenHash, $accountId, $email, $now, $this->timestamp($expiresAt)],
        );
        $this->connection->executeStatement(
            'INSERT INTO outbox_mails (id, recipient, subject, body_text, body_html, template, attempts, created_at, next_attempt_at) '
            . "VALUES (?, ?, ?, ?, ?, 'confirm-email', 0, ?, ?)",
            [$this->newId(), $email, $rendered->subject, $rendered->text, $rendered->html, $now, $now],
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

    private function newId(): string
    {
        $id = Uuid::v7();

        return $this->isPostgres() ? $id->toRfc4122() : $id->toBinary();
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
