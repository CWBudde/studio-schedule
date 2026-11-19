<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use DateTimeImmutable;
use DateTimeZone;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Exception\UniqueConstraintViolationException;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
use Psr\Clock\ClockInterface;
use StudioSchedule\Service\AccountConfirmation;
use StudioSchedule\Service\ConfirmationFailure;
use StudioSchedule\Service\PasswordHash;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\Migrator;
use StudioSchedule\Store\Schema;
use StudioSchedule\Store\SessionStore;

final class AccountConfirmationTest extends TestCase
{
    private const ACCOUNT = '018f0000-0000-7000-8000-000000000001';
    private const SESSION = '018f0000-0000-7000-8000-000000000002';
    private const NOW = '2026-12-04 12:00:00';
    private const ISSUED = '2026-12-03 12:00:00';
    private const EXPIRES = '2026-12-05 12:00:00';
    private const EMAIL = 'Member@example.test';
    private const TOKEN = 'fictional-confirmation-token';
    private const PASSWORD = 'fictional confirmation passphrase';

    private Connection $db;
    private bool $postgres;
    private SessionStore $sessions;
    private PasswordHash $passwords;
    private ClockInterface $clock;

    protected function setUp(): void
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($dsn === false) {
            self::markTestSkipped('Requires the disposable database from just php-test');
        }
        $db = Database::connect($dsn);
        self::assertSame([], (new Schema($db))->relations(), 'Requires an empty disposable database');
        $this->db = $db;
        $this->postgres = $db->getDatabasePlatform() instanceof PostgreSQLPlatform;
        $pgPath = getenv('STUDIO_PHP_TEST_POSTGRES_MIGRATIONS') ?: dirname(__DIR__, 2) . '/internal/store/postgres/migrations';
        (new Migrator($dsn))->up($this->postgres ? $pgPath : dirname(__DIR__) . '/migrations/mysql');

        $this->sessions = new SessionStore($db);
        $this->passwords = new PasswordHash();
        $this->clock = new class (new DateTimeImmutable(self::NOW, new DateTimeZone('UTC'))) implements ClockInterface {
            public function __construct(private readonly DateTimeImmutable $instant) {}

            public function now(): DateTimeImmutable
            {
                return $this->instant;
            }
        };
    }

    protected function tearDown(): void
    {
        if (!isset($this->db)) {
            return;
        }
        if (!$this->postgres) {
            $this->db->executeStatement('SET FOREIGN_KEY_CHECKS = 0');
        }
        foreach ($this->db->createSchemaManager()->listTableNames() as $table) {
            $this->db->executeStatement('DROP TABLE IF EXISTS ' . $this->db->quoteSingleIdentifier($table) . ($this->postgres ? ' CASCADE' : ''));
        }
        if ($this->postgres) {
            $this->db->executeStatement('DROP FUNCTION IF EXISTS receipts_immutable()');
        }
        $this->db->close();
    }

    public function testConfirmationConsumesTheLinkOnceAndUpgradesThePasswordWithoutChangingItsAge(): void
    {
        $old = password_hash(self::PASSWORD, 'argon2id', ['memory_cost' => 16384, 'time_cost' => 2, 'threads' => 1]);
        $this->account($old);
        $this->link(self::TOKEN);
        $service = new AccountConfirmation($this->sessions, $this->passwords, $this->clock);

        self::assertSame(ConfirmationFailure::InvalidCredentials, $service->confirm(self::TOKEN, 'wrong password'));
        self::assertSame(1, $this->countRows('account_tokens'));
        self::assertNull($this->db->fetchOne('SELECT confirmed_at FROM accounts'));
        self::assertSame(0, $this->countRows('sessions'));

        $changedAt = $this->db->fetchOne('SELECT changed_at FROM account_credentials');
        $result = $service->confirm(self::TOKEN, self::PASSWORD);
        self::assertIsArray($result);
        self::assertSame(self::ACCOUNT, $result['principal']->accountId);
        self::assertSame(18, $result['principal']->visibleDays);
        self::assertSame('2026-12-18 12:00:00', $result['expiresAt']->format('Y-m-d H:i:s'));
        self::assertMatchesRegularExpression('/^[A-Za-z0-9_-]{43}$/', $result['token']);
        self::assertSame(1, $this->countRows('sessions'));
        self::assertNotNull($this->sessions->findActiveAccount(hash('sha256', $result['token'])));
        $confirmedAt = $this->db->fetchOne('SELECT confirmed_at FROM accounts');
        self::assertIsString($confirmedAt);
        self::assertStringStartsWith(self::NOW, $confirmedAt);
        $new = $this->db->fetchOne('SELECT password_hash FROM account_credentials');
        self::assertIsString($new);
        self::assertNotSame($old, $new);
        self::assertFalse($this->passwords->needsRehash($new));
        self::assertTrue($this->passwords->verify($new, self::PASSWORD));
        self::assertSame($changedAt, $this->db->fetchOne('SELECT changed_at FROM account_credentials'));
        self::assertSame(0, $this->countRows('account_tokens'));
        self::assertSame(ConfirmationFailure::InvalidToken, $service->confirm(self::TOKEN, self::PASSWORD));
    }

    public function testWrongPurposeChangedAddressExpiredOrDisabledCannotConfirm(): void
    {
        $this->account($this->passwords->hash(self::PASSWORD));
        $this->link('reset', purpose: 'reset-password');
        $this->link('mismatch', email: strtolower(self::EMAIL));
        $this->link('expired', expires: self::NOW);
        $this->link('disabled');
        $this->db->executeStatement('UPDATE accounts SET disabled_at = ?', [self::NOW]);
        $service = new AccountConfirmation($this->sessions, $this->passwords, $this->clock);

        foreach (['reset', 'mismatch', 'expired'] as $token) {
            self::assertSame(ConfirmationFailure::InvalidToken, $service->confirm($token, self::PASSWORD));
        }
        self::assertSame(ConfirmationFailure::InvalidCredentials, $service->confirm('disabled', self::PASSWORD));
        $this->db->executeStatement('DELETE FROM account_credentials');
        self::assertSame(ConfirmationFailure::AccountNotFound, $service->confirm('disabled', self::PASSWORD));
        self::assertSame(4, $this->countRows('account_tokens'));
        self::assertNull($this->db->fetchOne('SELECT confirmed_at FROM accounts'));
        self::assertSame(0, $this->countRows('sessions'));
    }

    public function testStalePasswordHashAndFailureToCreateASessionLeaveConfirmationUntouched(): void
    {
        $old = $this->passwords->hash(self::PASSWORD);
        $this->account($old);
        $this->link(self::TOKEN);
        $replacement = $this->passwords->hash('later passphrase');
        $tokenHash = hash('sha256', self::TOKEN);
        $sessionHash = hash('sha256', 'existing session');
        $digest = $this->postgres ? "decode(?, 'hex')" : 'UNHEX(?)';
        $this->db->executeStatement(
            'INSERT INTO sessions (id, account_id, token_hash, created_at, last_seen_at, expires_at) '
            . "VALUES (?, ?, $digest, ?, ?, ?)",
            [$this->id(self::SESSION), $this->id(self::ACCOUNT), $sessionHash, self::ISSUED, self::ISSUED, self::EXPIRES],
        );
        $storedSession = $this->db->fetchAssociative("SELECT created_at, last_seen_at, expires_at FROM sessions WHERE token_hash = $digest", [$sessionHash]);
        self::assertIsArray($storedSession);
        $this->db->executeStatement('UPDATE account_credentials SET password_hash = ?', [$replacement]);

        self::assertSame(ConfirmationFailure::InvalidToken, $this->sessions->confirmForCredentials(
            self::ACCOUNT,
            $tokenHash,
            $old,
            $old,
            $sessionHash,
            $this->clock,
        ));

        try {
            $this->sessions->confirmForCredentials(self::ACCOUNT, $tokenHash, $replacement, $old, $sessionHash, $this->clock);
            self::fail('The duplicate session digest must fail the confirmation transaction');
        } catch (UniqueConstraintViolationException) {
            self::assertSame(1, $this->countRows('account_tokens'));
            self::assertNull($this->db->fetchOne('SELECT confirmed_at FROM accounts'));
            self::assertSame($replacement, $this->db->fetchOne('SELECT password_hash FROM account_credentials'));
            self::assertSame(1, $this->countRows('sessions'));
            self::assertSame(
                $storedSession,
                $this->db->fetchAssociative("SELECT created_at, last_seen_at, expires_at FROM sessions WHERE token_hash = $digest", [$sessionHash]),
            );
        }
    }

    public function testConfirmationRechecksTheLinkExpiryAfterPasswordVerification(): void
    {
        $this->account($this->passwords->hash(self::PASSWORD));
        $this->link(self::TOKEN);
        $clock = self::createStub(ClockInterface::class);
        $clock->method('now')->willReturnOnConsecutiveCalls(
            $this->clock->now(),
            new DateTimeImmutable(self::EXPIRES, new DateTimeZone('UTC')),
        );

        $service = new AccountConfirmation($this->sessions, $this->passwords, $clock);
        self::assertSame(ConfirmationFailure::InvalidToken, $service->confirm(self::TOKEN, self::PASSWORD));
        self::assertSame(1, $this->countRows('account_tokens'));
        self::assertNull($this->db->fetchOne('SELECT confirmed_at FROM accounts'));
        self::assertSame(0, $this->countRows('sessions'));
    }

    private function account(string $hash): void
    {
        $id = $this->id(self::ACCOUNT);
        $this->db->executeStatement("INSERT INTO persons (id, given_name, family_name) VALUES (?, 'Ari', 'Fable')", [$id]);
        $this->db->executeStatement(
            "INSERT INTO accounts (id, person_id, email, role, visible_days) VALUES (?, ?, ?, 'member', 18)",
            [$id, $id, self::EMAIL],
        );
        $this->db->executeStatement(
            'INSERT INTO account_credentials (account_id, password_hash, changed_at) VALUES (?, ?, ?)',
            [$id, $hash, self::ISSUED],
        );
    }

    private function link(string $token, string $purpose = 'confirm-email', string $email = self::EMAIL, string $expires = self::EXPIRES): void
    {
        $digest = $this->postgres ? "decode(?, 'hex')" : 'UNHEX(?)';
        $this->db->executeStatement(
            'INSERT INTO account_tokens (token_hash, account_id, purpose, email, created_at, expires_at) '
            . "VALUES ($digest, ?, ?, ?, ?, ?)",
            [hash('sha256', $token), $this->id(self::ACCOUNT), $purpose, $email, self::ISSUED, $expires],
        );
    }

    private function countRows(string $table): int
    {
        $count = $this->db->fetchOne('SELECT COUNT(*) FROM ' . $table);
        self::assertIsNumeric($count);

        return (int) $count;
    }

    private function id(string $uuid): string
    {
        return $this->postgres ? $uuid : pack('H*', str_replace('-', '', $uuid));
    }
}
