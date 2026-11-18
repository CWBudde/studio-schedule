<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Exception\TableNotFoundException;
use Doctrine\DBAL\Exception\UniqueConstraintViolationException;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use StudioSchedule\Service\AlreadyInitialized;
use StudioSchedule\Service\InvalidSetupInput;
use StudioSchedule\Service\SetupInput;
use StudioSchedule\Service\SetupPersistence;
use Symfony\Component\Uid\UuidV7;

final readonly class SetupStore implements SetupPersistence
{
    public function __construct(private Connection $connection) {}

    public function isInitialized(): bool
    {
        try {
            return $this->connection->fetchOne('SELECT 1 FROM settings WHERE id = TRUE') !== false;
        } catch (TableNotFoundException) {
            return false;
        }
    }

    public function adminPasswordHash(string $email): ?string
    {
        $email = SetupInput::trim($email);
        if (preg_match('/\A[^\x00]+\z/u', $email) !== 1) {
            return null;
        }

        if ($this->isPostgres()) {
            $lookup = 'lower(a.email) = lower(CAST(? AS text) COLLATE "C")';
        } else {
            // PHP's strtolower, like the generated email_folded column, folds ASCII only.
            $lookup = 'a.email_folded = ?';
            $email = strtolower($email);
        }

        try {
            // All these columns were present when password login was introduced.
            $hash = $this->connection->fetchOne(
                'SELECT c.password_hash FROM accounts a JOIN account_credentials c ON c.account_id = a.id '
                . "WHERE $lookup AND a.role = 'admin' AND a.confirmed_at IS NOT NULL AND a.disabled_at IS NULL",
                [$email],
            );
        } catch (TableNotFoundException) {
            return null;
        }

        return is_string($hash) ? $hash : null;
    }

    public function initialize(
        SetupInput $input,
        UuidV7 $personId,
        UuidV7 $accountId,
        #[\SensitiveParameter]
        string $tokenHash,
        DateTimeImmutable $createdAt,
        DateTimeImmutable $expiresAt,
    ): void {
        $this->connection->transactional(function (Connection $db) use ($input, $personId, $accountId, $tokenHash, $createdAt, $expiresAt): void {
            $postgres = $this->isPostgres();
            $id = $postgres ? '?' : 'UNHEX(?)';
            $digest = $postgres ? "decode(?, 'hex')" : 'UNHEX(?)';
            $dateFormat = $postgres ? 'Y-m-d H:i:s.uP' : 'Y-m-d H:i:s.u';
            $person = $postgres ? $personId->toRfc4122() : bin2hex($personId->toBinary());
            $account = $postgres ? $accountId->toRfc4122() : bin2hex($accountId->toBinary());

            // Go SetupService.Init inserts the same singleton. A concurrent
            // winner makes this insert fail before a person or account is written.
            $durations = $postgres ? "'0 seconds', '2 hours', '12 hours'" : '0, 7200000000, 43200000000';
            try {
                $db->executeStatement(
                    'INSERT INTO settings (school_name, timezone, default_locale, currency, receipt_prefix, release_weekday, release_time, '
                    . 'release_weeks_ahead, default_visible_days, redemption_mode, booking_cutoff, cancellation_cutoff, waitlist_invite_expiry) '
                    . "VALUES (?, ?, ?, ?, 'R', 0, '18:00:00', 2, 14, 'attendance', $durations)",
                    [$input->schoolName, $input->timezone, $input->locale, $input->currency],
                );
            } catch (UniqueConstraintViolationException) {
                throw new AlreadyInitialized();
            }

            $db->executeStatement(
                "INSERT INTO persons (id, given_name, family_name, email) VALUES ($id, ?, ?, ?)",
                [$person, $input->adminGivenName, $input->adminFamilyName, $input->adminEmail],
            );
            try {
                $db->executeStatement(
                    "INSERT INTO accounts (id, person_id, email, role, locale) VALUES ($id, $id, ?, 'admin', ?)",
                    [$account, $person, $input->adminEmail, $input->locale],
                );
            } catch (UniqueConstraintViolationException) {
                throw new InvalidSetupInput('email', 'Email address is already in use');
            }

            $db->executeStatement(
                'INSERT INTO account_tokens (token_hash, account_id, purpose, email, created_at, expires_at) '
                . "VALUES ($digest, $id, 'reset-password', ?, ?, ?)",
                [$tokenHash, $account, $input->adminEmail, $createdAt->format($dateFormat), $expiresAt->format($dateFormat)],
            );
        });
    }

    private function isPostgres(): bool
    {
        return $this->connection->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }
}
