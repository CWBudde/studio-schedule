<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Exception as DBALException;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
use Psr\Log\NullLogger;
use Slim\Psr7\Factory\ServerRequestFactory;
use StudioSchedule\Config;
use StudioSchedule\Http\Application;
use RuntimeException;
use StudioSchedule\Service\DatabaseReadiness;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\MigrationRecoveryRequired;
use StudioSchedule\Store\Migrator;
use StudioSchedule\Store\Schema;
use UnexpectedValueException;

final class MigratorTest extends TestCase
{
    private Connection $database;
    private Migrator $migrator;
    private string $migrations;

    protected function setUp(): void
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($dsn === false) {
            self::markTestSkipped('Requires the database provisioned by just php-test');
        }

        $this->database = Database::connect($dsn);
        self::assertSame([], (new Schema($this->database))->relations(), 'The migration tests require a disposable, empty database');
        $this->migrator = new Migrator($dsn);
        $this->migrations = sys_get_temp_dir() . '/studio-migrator-' . bin2hex(random_bytes(8));
        self::assertTrue(mkdir($this->migrations));
    }

    protected function tearDown(): void
    {
        if (!isset($this->migrations)) {
            return;
        }

        try {
            $this->dropFixture();
        } finally {
            $this->database->close();
            foreach (glob($this->migrations . '/*.sql') ?: [] as $file) {
                unlink($file);
            }
            rmdir($this->migrations);
        }
    }

    public function testInterruptedUpgradeRequiresDatabaseRestore(): void
    {
        if ($this->database->getDatabasePlatform() instanceof PostgreSQLPlatform) {
            self::markTestSkipped('MySQL and MariaDB implicitly commit DDL');
        }

        $this->installBaseline();
        $schema = new Schema($this->database);
        $this->database->executeStatement('INSERT INTO upgrade_base VALUES (41)');
        $backupData = $this->database->fetchFirstColumn('SELECT id FROM upgrade_base ORDER BY id');
        $backupHistory = $this->database->fetchAllNumeric(
            'SELECT id, version_id, is_applied, tstamp FROM goose_db_version ORDER BY id',
        );
        $this->writeUpgrade('INSERT INTO missing_table VALUES (1);');
        try {
            $this->migrator->up($this->migrations);
            self::fail('Upgrade started without a verified backup');
        } catch (RuntimeException $error) {
            self::assertNotInstanceOf(MigrationRecoveryRequired::class, $error);
            self::assertStringContainsString('backup', $error->getMessage());
        }
        self::assertSame([1], $schema->appliedVersions());
        self::assertFalse($this->database->createSchemaManager()->tablesExist(['upgrade_extra']));

        try {
            $this->migrator->up($this->migrations, backupVerified: true);
            self::fail('Expected the statement after the DDL to fail');
        } catch (MigrationRecoveryRequired $error) {
            self::assertStringContainsString('Replace the database with the verified pre-upgrade backup', $error->getMessage());
        }
        self::assertTrue($this->database->createSchemaManager()->tablesExist(['upgrade_extra']));
        $newRelease = new DatabaseReadiness($schema, [1, 2], new NullLogger());
        self::assertFalse($newRelease->ready());
        $oldRelease = new DatabaseReadiness($schema, [1], new NullLogger());
        self::assertFalse($oldRelease->ready());
        $app = Application::create(
            Config::load($this->migrations . '/.env', static fn(string $key): ?string => null),
            $this->migrations,
            $oldRelease,
            new NullLogger(),
        );
        $request = (new ServerRequestFactory())->createServerRequest('GET', '/api/v1/health');
        $response = $app->handle($request);
        self::assertSame(503, $response->getStatusCode());
        self::assertStringContainsString('"type":"urn:studio-schedule:maintenance"', (string) $response->getBody());

        $this->writeUpgrade('INSERT INTO upgrade_extra VALUES (1);');
        try {
            $this->migrator->up($this->migrations, backupVerified: true);
            self::fail('A second confirmation must never clear the recovery requirement');
        } catch (MigrationRecoveryRequired) {
            self::assertContains($this->database->fetchOne('SELECT COUNT(*) FROM upgrade_extra'), [0, '0']);
        }

        // Replace the damaged database with the pre-upgrade data and history.
        $this->dropFixture();
        $this->database->executeStatement('CREATE TABLE upgrade_base (id INTEGER PRIMARY KEY)');
        $this->database->executeStatement(
            'CREATE TABLE goose_db_version (id BIGINT AUTO_INCREMENT PRIMARY KEY, version_id BIGINT NOT NULL, '
            . 'is_applied BOOLEAN NOT NULL, tstamp TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP) ENGINE=InnoDB',
        );
        foreach ($backupData as $id) {
            $this->database->executeStatement('INSERT INTO upgrade_base VALUES (?)', [$id]);
        }
        foreach ($backupHistory as $row) {
            $this->database->executeStatement(
                'INSERT INTO goose_db_version (id, version_id, is_applied, tstamp) VALUES (?, ?, ?, ?)',
                $row,
            );
        }
        $this->writeUpgrade('INSERT INTO upgrade_extra VALUES (1);');
        self::assertSame([2], $this->migrator->up($this->migrations, backupVerified: true));
        self::assertTrue($newRelease->ready());
        self::assertSame($backupData, $this->database->fetchFirstColumn('SELECT id FROM upgrade_base ORDER BY id'));
        self::assertSame([], $this->migrator->up($this->migrations));
    }

    public function testPostgresRollsBackAnUpgradeAndCanRetry(): void
    {
        if (!($this->database->getDatabasePlatform() instanceof PostgreSQLPlatform)) {
            self::markTestSkipped('PostgreSQL transactional DDL');
        }

        $this->installBaseline();
        $this->writeUpgrade('INSERT INTO missing_table VALUES (1);');
        try {
            $this->migrator->up($this->migrations);
            self::fail('Expected the second statement to fail');
        } catch (DBALException) {
            self::assertFalse($this->database->createSchemaManager()->tablesExist(['upgrade_extra']));
        }
        self::assertSame([1], (new Schema($this->database))->appliedVersions());
        $this->writeUpgrade('INSERT INTO upgrade_extra VALUES (1);');
        self::assertSame([2], $this->migrator->up($this->migrations));
    }

    public function testEmptyReleaseDoesNotCreateMigrationHistory(): void
    {
        try {
            $this->migrator->up($this->migrations);
            self::fail('Expected the empty release to be refused');
        } catch (RuntimeException $error) {
            self::assertStringContainsString('no migration files', $error->getMessage());
        }
        self::assertSame([], (new Schema($this->database))->relations());
    }

    public function testMissingHistoryForPopulatedDatabasePreventsMigrations(): void
    {
        $this->database->executeStatement('CREATE TABLE upgrade_base (id INTEGER PRIMARY KEY)');
        $this->writeUpgrade('INSERT INTO upgrade_extra VALUES (1);');

        $this->expectException(RuntimeException::class);
        $this->expectExceptionMessage('migration history');
        $this->migrator->up($this->migrations, backupVerified: true);
    }

    public function testMigrationHistoryMustBeAPrefixOfTheRelease(): void
    {
        $this->installBaseline();
        $this->database->executeStatement('INSERT INTO goose_db_version (version_id, is_applied) VALUES (9, TRUE)');
        $this->writeUpgrade('INSERT INTO upgrade_extra VALUES (1);');

        $this->expectException(RuntimeException::class);
        $this->expectExceptionMessage('migration history');
        $this->migrator->up($this->migrations, backupVerified: true);
    }

    public function testInvalidFileDoesNotStartMigration(): void
    {
        $this->installBaseline();
        file_put_contents($this->migrations . '/00002_upgrade.sql', "-- +goose Up\nCREATE TABLE upgrade_extra (id INTEGER)");

        try {
            $this->migrator->up($this->migrations, backupVerified: true);
            self::fail('An unterminated statement must be rejected before it is run');
        } catch (UnexpectedValueException $error) {
            self::assertStringContainsString('00002_upgrade.sql', $error->getMessage());
        }
        self::assertSame([1], (new Schema($this->database))->appliedVersions());
        self::assertFalse($this->database->createSchemaManager()->tablesExist(['upgrade_extra']));
    }

    public function testConcurrentMigratorDoesNotRunStatements(): void
    {
        $postgres = $this->database->getDatabasePlatform() instanceof PostgreSQLPlatform;
        $sql = $postgres
            ? 'SELECT pg_advisory_lock(4097083626)'
            : "SELECT GET_LOCK(SHA2(CONCAT('studio-schedule:', DATABASE(), ':migrations'), 256), 0)";
        $this->database->fetchOne($sql);
        $this->writeUpgrade('INSERT INTO upgrade_extra VALUES (1);');
        try {
            $this->migrator->up($this->migrations);
            self::fail('A concurrent migration must not start');
        } catch (RuntimeException $error) {
            self::assertStringContainsString('already running', $error->getMessage());
        } finally {
            $sql = $postgres
                ? 'SELECT pg_advisory_unlock(4097083626)'
                : "SELECT RELEASE_LOCK(SHA2(CONCAT('studio-schedule:', DATABASE(), ':migrations'), 256))";
            $this->database->fetchOne($sql);
        }
        self::assertSame([], $this->database->createSchemaManager()->listTableNames());
    }

    private function installBaseline(): void
    {
        file_put_contents($this->migrations . '/00001_baseline.sql', <<<'SQL'
            -- Baseline and a data row from an earlier release.
            -- +goose Up
            CREATE TABLE upgrade_base (
                -- This comment ends with a semicolon;
                id INTEGER PRIMARY KEY
            );
            INSERT INTO upgrade_base VALUES (17);
            -- +goose Down
            DROP TABLE upgrade_base;
            SQL);
        self::assertSame([1], $this->migrator->up($this->migrations));
    }

    private function writeUpgrade(string $statement): void
    {
        file_put_contents($this->migrations . '/00002_upgrade.sql', <<<SQL
            -- +goose Up
            CREATE TABLE upgrade_extra (id INTEGER PRIMARY KEY);
            $statement
            -- +goose Down
            DROP TABLE upgrade_extra;
            SQL);
    }

    private function dropFixture(): void
    {
        foreach (['upgrade_extra', 'upgrade_base', 'goose_db_version'] as $table) {
            $this->database->executeStatement('DROP TABLE IF EXISTS ' . $table);
        }
    }
}
