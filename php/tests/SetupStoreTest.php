<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
use Psr\Clock\ClockInterface;
use StudioSchedule\Service\AlreadyInitialized;
use StudioSchedule\Service\InvalidSetupInput;
use StudioSchedule\Service\SchoolSetup;
use StudioSchedule\Service\SetupInput;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\MigrationSql;
use StudioSchedule\Store\Migrator;
use StudioSchedule\Store\Schema;
use StudioSchedule\Store\SetupStore;

final class SetupStoreTest extends TestCase
{
    private Connection $database;
    private string $dsn;
    private string $migrations;
    private bool $postgres;
    private bool $fixtureDatabase = false;

    protected function setUp(): void
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($dsn === false) {
            self::markTestSkipped('Requires the disposable database provisioned by just php-test');
        }

        $this->dsn = $dsn;
        $this->database = Database::connect($dsn);
        $this->postgres = $this->database->getDatabasePlatform() instanceof PostgreSQLPlatform;
        self::assertSame([], (new Schema($this->database))->relations(), 'Setup tests require an empty disposable database');
        $this->fixtureDatabase = true;
        $this->migrations = $this->postgres
            ? (getenv('STUDIO_PHP_TEST_POSTGRES_MIGRATIONS') ?: __DIR__ . '/../../internal/store/postgres/migrations')
            : __DIR__ . '/../migrations/mysql';
    }

    protected function tearDown(): void
    {
        if (!$this->fixtureDatabase) {
            return;
        }

        try {
            if (!$this->postgres) {
                $this->database->executeStatement('SET FOREIGN_KEY_CHECKS = 0');
            }
            $tables = $this->database->createSchemaManager()->listTableNames();
            foreach ($tables as $table) {
                $suffix = $this->postgres ? ' CASCADE' : '';
                $this->database->executeStatement('DROP TABLE IF EXISTS ' . $this->database->quoteSingleIdentifier($table) . $suffix);
            }
            if ($this->postgres) {
                $this->database->executeStatement('DROP FUNCTION IF EXISTS receipts_immutable()');
            }
        } finally {
            $this->database->close();
        }
    }

    public function testSetupUsesGoDefaultsAndStoresOnlyTheResetTokenHash(): void
    {
        $store = new SetupStore($this->database);
        self::assertFalse($store->isInitialized());
        self::assertNull($store->adminPasswordHash('admin@example.org'));
        (new Migrator($this->dsn))->up($this->migrations);
        self::assertFalse($store->isInitialized());
        $setup = new SchoolSetup($store, self::clock());
        $result = $setup->init(new SetupInput(
            "\u{2003}Fictional Dance Hall\u{00a0}",
            ' Europe/Vienna ',
            ' DE_at ',
            ' eur ',
            ' ÉDA@fictional-dance.example ',
            ' Ada ',
            ' Example ',
        ));
        self::assertTrue($store->isInitialized());

        $cutoffColumns = $this->postgres
            ? 'EXTRACT(EPOCH FROM booking_cutoff), EXTRACT(EPOCH FROM cancellation_cutoff), EXTRACT(EPOCH FROM waitlist_invite_expiry)'
            : 'booking_cutoff / 1000000, cancellation_cutoff / 1000000, waitlist_invite_expiry / 1000000';
        $settings = $this->database->fetchNumeric(
            'SELECT school_name, timezone, default_locale, currency, receipt_prefix, release_weekday, release_time, '
            . 'release_weeks_ahead, default_visible_days, redemption_mode, ' . $cutoffColumns . ' FROM settings',
        );
        self::assertIsArray($settings);
        $expectedSettings = ['Fictional Dance Hall', 'Europe/Vienna', 'de', 'EUR', 'R', 0, '18:00:00', 2, 14, 'attendance'];
        foreach ($expectedSettings as $column => $expected) {
            if ($column === 6) {
                self::assertContains($settings[$column], ['18:00:00', '18:00:00.000000']);
            } elseif (is_int($expected)) {
                self::assertContains($settings[$column], [$expected, (string) $expected]);
            } else {
                self::assertSame($expected, $settings[$column]);
            }
        }
        self::assertEquals([0, 7200, 43200], array_slice($settings, 10));

        self::assertSame(
            ['Ada', 'Example', 'ÉDA@fictional-dance.example', 'ÉDA@fictional-dance.example', 'admin', 'de', null, null, null],
            $this->database->fetchNumeric('SELECT p.given_name, p.family_name, p.email, a.email, a.role, a.locale, '
                . 'a.visible_days, a.confirmed_at, a.disabled_at FROM accounts a JOIN persons p ON p.id = a.person_id'),
        );
        self::assertNull($store->adminPasswordHash('Éda@fictional-dance.example'));
        self::assertContains($this->database->fetchOne('SELECT COUNT(*) FROM account_credentials'), [0, '0']);
        self::assertContains($this->database->fetchOne('SELECT COUNT(*) FROM outbox_mails'), [0, '0']);

        $hashExpression = $this->postgres ? "encode(token_hash, 'hex')" : 'LOWER(HEX(token_hash))';
        $token = $this->database->fetchAssociative(
            'SELECT ' . $hashExpression . ' AS hash, account_tokens.email, purpose, created_at, expires_at FROM account_tokens '
            . 'JOIN accounts ON accounts.id = account_tokens.account_id',
        );
        self::assertIsArray($token);
        self::assertSame(hash('sha256', $result->token), $token['hash']);
        self::assertSame('ÉDA@fictional-dance.example', $token['email']);
        self::assertSame('reset-password', $token['purpose']);
        self::assertIsString($token['created_at']);
        self::assertIsString($token['expires_at']);
        self::assertEquals(self::clock()->now(), new DateTimeImmutable($token['created_at'] . ($this->postgres ? '' : ' UTC')));
        self::assertEquals(new DateTimeImmutable('2026-03-29T10:15:27.321456+00:00'), $result->expiresAt);
        self::assertEquals($result->expiresAt, new DateTimeImmutable($token['expires_at'] . ($this->postgres ? '' : ' UTC')));
        self::assertMatchesRegularExpression('/\A[A-Za-z0-9_-]{43}\z/', $result->token);
        self::assertSame('/studio/reset-password?token=' . $result->token, $result->passwordLink('/studio/'));

        $id = $this->database->fetchOne('SELECT id FROM accounts');
        self::assertIsString($id);
        self::assertSame($this->postgres ? $result->accountId : str_replace('-', '', $result->accountId), $this->postgres ? $id : bin2hex($id));
        self::assertMatchesRegularExpression('/\A[\da-f]{8}-[\da-f]{4}-7[\da-f]{3}-[89ab][\da-f]{3}-[\da-f]{12}\z/', $result->accountId);

        try {
            $setup->init(new SetupInput('Other School', 'UTC', 'en', 'GBP', 'other@example.org', 'Al', 'Other'));
            self::fail('A second initialization was accepted');
        } catch (AlreadyInitialized) {
            self::assertSame('Fictional Dance Hall', $this->database->fetchOne('SELECT school_name FROM settings'));
            foreach (['persons', 'accounts', 'account_tokens'] as $table) {
                self::assertContains($this->database->fetchOne("SELECT COUNT(*) FROM $table"), [1, '1']);
            }
        }
    }

    public function testDuplicateEmailRollsBackSettingsAndPerson(): void
    {
        (new Migrator($this->dsn))->up($this->migrations);
        $store = new SetupStore($this->database);
        $this->addPersonAndAccount('Clerk@fictional-dance.example', 'office');
        $setup = new SchoolSetup($store, self::clock());

        try {
            $setup->init(new SetupInput('Example Studio', 'UTC', 'en', 'GBP', 'clerk@fictional-dance.example', 'Al', 'Other'));
            self::fail('The existing email was accepted for a new account');
        } catch (InvalidSetupInput $error) {
            self::assertSame('email', $error->field);
        }

        self::assertFalse($store->isInitialized());
        self::assertContains($this->database->fetchOne('SELECT COUNT(*) FROM persons'), [1, '1']);
        self::assertContains($this->database->fetchOne('SELECT COUNT(*) FROM account_tokens'), [0, '0']);
    }

    public function testAdminAuthenticationWorksWithTheFirstPasswordCapableSchema(): void
    {
        foreach (array_slice(Schema::migrationFiles($this->migrations), 0, 2) as $file) {
            foreach (MigrationSql::up($file) as $statement) {
                $this->database->executeStatement($statement);
            }
        }

        $store = new SetupStore($this->database);
        self::assertFalse($store->isInitialized());
        $id = $this->addPersonAndAccount('ÉDA@Fictional-Dance.example', 'admin');
        $email = "\u{2003}Éda@fictional-dance.example ";
        self::assertNull($store->adminPasswordHash($email));

        $this->database->executeStatement(
            'INSERT INTO account_credentials (account_id, password_hash, changed_at) VALUES (?, ?, CURRENT_TIMESTAMP)',
            [$id, 'fixture-password-hash'],
        );
        self::assertNull($store->adminPasswordHash($email));
        $this->database->executeStatement('UPDATE accounts SET confirmed_at = CURRENT_TIMESTAMP');
        self::assertSame('fixture-password-hash', $store->adminPasswordHash($email));
        self::assertNull($store->adminPasswordHash('éda@fictional-dance.example'));
        self::assertNull($store->adminPasswordHash('other@fictional-dance.example'));
        self::assertNull($store->adminPasswordHash("admin\0@example.org"));
        $this->database->executeStatement('UPDATE accounts SET disabled_at = CURRENT_TIMESTAMP');
        self::assertNull($store->adminPasswordHash($email));
        $this->database->executeStatement("UPDATE accounts SET disabled_at = NULL, role = 'office'");
        self::assertNull($store->adminPasswordHash($email));
    }

    private function addPersonAndAccount(string $email, string $role): string
    {
        $uuid = '00000000-0000-4000-8000-000000000001';
        $id = $this->postgres ? $uuid : pack('H*', str_replace('-', '', $uuid));
        $this->database->executeStatement("INSERT INTO persons (id, given_name, family_name) VALUES (?, 'Al', 'Example')", [$id]);
        $this->database->executeStatement('INSERT INTO accounts (id, person_id, email, role) VALUES (?, ?, ?, ?)', [$id, $id, $email, $role]);

        return $id;
    }

    private static function clock(): ClockInterface
    {
        return new class implements ClockInterface {
            public function now(): DateTimeImmutable
            {
                return new DateTimeImmutable('2026-03-26T11:15:27.321456+01:00');
            }
        };
    }
}
