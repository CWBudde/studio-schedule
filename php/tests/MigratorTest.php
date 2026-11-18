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
use StudioSchedule\Console\Operator;
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
    private string $appDirectory;
    private string $dsn;

    protected function setUp(): void
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($dsn === false) {
            self::markTestSkipped('Requires the database provisioned by just php-test');
        }

        $this->dsn = $dsn;
        $this->database = Database::connect($dsn);
        self::assertSame([], (new Schema($this->database))->relations(), 'The migration tests require a disposable, empty database');
        $this->migrator = new Migrator($dsn);
        $this->appDirectory = sys_get_temp_dir() . '/studio-migrator-' . bin2hex(random_bytes(8));
        $this->migrations = $this->appDirectory . '/migrations/mysql';
        self::assertTrue(mkdir($this->migrations, 0700, true));
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
            rmdir(dirname($this->migrations));
            rmdir($this->appDirectory);
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

    public function testCliReportsAndAppliesMigrationsAndRevertsGooseHistory(): void
    {
        $this->writeBasicMigration();
        $postgres = $this->database->getDatabasePlatform() instanceof PostgreSQLPlatform;
        $this->writeUpgrade('INSERT INTO upgrade_extra VALUES (1);');

        [$code, $out, $error] = $this->command(['migrate', 'status']);
        self::assertSame(0, $code, $error);
        self::assertSame("VERSION  STATE    APPLIED AT  NAME\n1        pending  -           00001_baseline.sql\n2        pending  -           00002_upgrade.sql\n", $out);
        self::assertSame([], (new Schema($this->database))->relations());

        [$code, $out, $error] = $this->command(['migrate', 'down']);
        self::assertSame(1, $code);
        self::assertSame('', $out);
        self::assertStringContainsString('no migration to roll back', $error);

        [$code, $out, $error] = $this->command(['migrate', 'up']);
        self::assertSame(0, $code, $error);
        self::assertMatchesRegularExpression('/^up   00001_baseline\.sql \(.+\)\nup   00002_upgrade\.sql \(.+\)\n$/D', $out);
        $this->database->executeStatement("UPDATE goose_db_version SET tstamp = '2026-01-02 03:04:05' WHERE version_id = 1");

        [$code, $out, $error] = $this->command(['migrate', 'status']);
        self::assertSame(0, $code, $error);
        self::assertMatchesRegularExpression('/^1 {8}applied  2026-01-02T03:04:05Z  00001_baseline\.sql$/m', $out);
        self::assertMatchesRegularExpression('/^2 {8}applied  .+Z  00002_upgrade\.sql$/m', $out);
        self::assertSame([0, "schema is up to date\n", ''], $this->command(['migrate', 'up']));

        if (!$postgres) {
            [$code, $out, $error] = $this->command(['migrate', 'down', '--backup-verified=false']);
            self::assertSame(1, $code);
            self::assertSame('', $out);
            self::assertStringContainsString('--backup-verified', $error);
            self::assertTrue($this->database->createSchemaManager()->tablesExist(['upgrade_extra']));
        }

        [$code, $out, $error] = $this->command($postgres ? ['migrate', 'down'] : ['migrate', 'down', '--backup-verified']);
        self::assertSame(0, $code, $error);
        self::assertMatchesRegularExpression('/^down 00002_upgrade\.sql \(.+\)\n$/D', $out);
        self::assertFalse($this->database->createSchemaManager()->tablesExist(['upgrade_extra']));
        $history = array_map(static fn(mixed $version): string => is_int($version) ? (string) $version : (is_string($version) ? $version : ''), $this->database->fetchFirstColumn('SELECT version_id FROM goose_db_version ORDER BY id'));
        self::assertSame(['0', '1'], $history);

        [$code, $out, $error] = $this->command(['migrate', 'status']);
        self::assertSame(0, $code, $error);
        self::assertMatchesRegularExpression('/^2 {8}pending  - {21}00002_upgrade\.sql$/m', $out);
        if (!$postgres) {
            [$code, $out, $error] = $this->command(['migrate', 'up']);
            self::assertSame(1, $code);
            self::assertSame('', $out);
            self::assertStringContainsString('--backup-verified', $error);
        }
        [$code, $out, $error] = $this->command(['migrate', 'up', '--backup-verified=true']);
        self::assertSame(0, $code, $error);
        self::assertMatchesRegularExpression('/^up   00002_upgrade\.sql \(.+\)\n$/D', $out);
        self::assertSame([1, 2], (new Schema($this->database))->appliedVersions());
    }

    public function testRevertingTheLastMigrationLeavesAnEmptyGooseHistoryThatCanApplyAgain(): void
    {
        $this->installBaseline();
        [$code, $out, $error] = $this->command(['migrate', 'down', '--backup-verified']);
        self::assertSame(0, $code, $error);
        self::assertMatchesRegularExpression('/^down 00001_baseline\.sql \(.+\)\n$/D', $out);
        self::assertSame([], (new Schema($this->database))->appliedVersions());

        if ($this->database->getDatabasePlatform() instanceof PostgreSQLPlatform) {
            $this->database->executeStatement('CREATE SEQUENCE unrelated_sequence');
            try {
                [$code, , $error] = $this->command(['migrate', 'up']);
                self::assertSame(1, $code);
                self::assertStringContainsString('missing its migration history', $error);
            } finally {
                $this->database->executeStatement('DROP SEQUENCE unrelated_sequence');
            }
        }

        [$code, $out, $error] = $this->command(['migrate', 'up']);
        self::assertSame(0, $code, $error);
        self::assertMatchesRegularExpression('/^up   00001_baseline\.sql \(.+\)\n$/D', $out);
        self::assertSame([1], (new Schema($this->database))->appliedVersions());
    }

    public function testFailedDownIsTransactionalOrRequiresRestoration(): void
    {
        $this->installBaseline();
        file_put_contents($this->migrations . '/00002_upgrade.sql', <<<'SQL'
            -- +goose Up
            CREATE TABLE upgrade_extra (id INTEGER PRIMARY KEY);
            -- +goose Down
            DROP TABLE upgrade_extra;
            INSERT INTO missing_table VALUES (1);
            SQL);
        $this->migrator->up($this->migrations, backupVerified: true);
        $postgres = $this->database->getDatabasePlatform() instanceof PostgreSQLPlatform;
        [$code, $out, $error] = $this->command(['migrate', 'down', '--backup-verified']);
        self::assertSame(1, $code);
        self::assertSame('', $out);
        self::assertSame($postgres, $this->database->createSchemaManager()->tablesExist(['upgrade_extra']));

        if ($postgres) {
            self::assertSame([1, 2], (new Schema($this->database))->appliedVersions());
            self::assertStringNotContainsString('missing_table', $error);
        } else {
            self::assertStringContainsString('Replace the database', $error);
            self::assertFalse((new DatabaseReadiness(new Schema($this->database), [1, 2], new NullLogger()))->ready());
            foreach ([['migrate', 'status'], ['migrate', 'up', '--backup-verified'], ['migrate', 'down', '--backup-verified']] as $command) {
                self::assertSame([1, '', $error], $this->command($command));
            }
        }
    }

    public function testCourseTypeRollbackRestoresTheRequiredForeignKey(): void
    {
        if ($this->database->getDatabasePlatform() instanceof PostgreSQLPlatform) {
            self::markTestSkipped('MySQL and MariaDB have their own rollback SQL for this foreign key');
        }
        $shippedMigrations = dirname(__DIR__) . '/migrations/mysql';
        foreach (Schema::migrationFiles($shippedMigrations) as $version => $file) {
            if ($version > 30) {
                break;
            }
            self::assertTrue(copy($file, $this->migrations . '/' . basename($file)));
        }

        try {
            self::assertSame(range(1, 30), $this->migrator->up($this->migrations));
            [$code, $out, $error] = $this->command(['migrate', 'down', '--backup-verified']);
            self::assertSame(0, $code, $error);
            self::assertStringContainsString('down 00030_lesson_rental_course_type.sql', $out);
            $lessons = $this->database->createSchemaManager()->introspectTable('lessons');
            self::assertTrue($lessons->getColumn('course_type_id')->getNotnull());
            $courseType = $lessons->getForeignKey('lessons_course_type_id_fkey');
            self::assertSame(['course_type_id'], $courseType->getLocalColumns());
            self::assertSame('course_types', $courseType->getForeignTableName());
            self::assertContains($courseType->onDelete(), [null, 'RESTRICT']);
            self::assertSame(range(1, 29), (new Schema($this->database))->appliedVersions());
        } finally {
            $this->database->executeStatement('SET FOREIGN_KEY_CHECKS = 0');
            try {
                foreach ($this->database->createSchemaManager()->listTableNames() as $table) {
                    $this->database->executeStatement('DROP TABLE ' . $this->database->quoteSingleIdentifier($table));
                }
            } finally {
                $this->database->executeStatement('SET FOREIGN_KEY_CHECKS = 1');
            }
        }
    }

    public function testMalformedDownLeavesSchemaAndHistoryUntouched(): void
    {
        $this->installBaseline();
        file_put_contents($this->migrations . '/00002_upgrade.sql', <<<'SQL'
            -- +goose Up
            CREATE TABLE upgrade_extra (id INTEGER PRIMARY KEY);
            -- +goose Down
            DROP TABLE upgrade_extra
            SQL);
        $this->migrator->up($this->migrations, backupVerified: true);

        [$code, $out, $error] = $this->command(['migrate', 'down', '--backup-verified']);
        self::assertSame(1, $code);
        self::assertSame('', $out);
        self::assertStringContainsString('Invalid Down section in migration 00002_upgrade.sql', $error);
        self::assertTrue($this->database->createSchemaManager()->tablesExist(['upgrade_extra']));
        self::assertSame([1, 2], (new Schema($this->database))->appliedVersions());
    }

    public function testMigrationCommandRejectsInvalidOptionsBeforeConnecting(): void
    {
        $invalid = [
            [['migrate', 'status', '--backup-verified'], 'unknown flag'],
            [['migrate', 'down', '--backup-verified=maybe'], '--backup-verified'],
            [['migrate', 'unknown'], 'unknown migration command'],
            [['migrate', 'up', '--database-url='], 'no database URL'],
            [['--database-url=unsupported://fixture:secret@invalid.invalid/db', 'migrate', 'status'], 'DATABASE_URL must name PostgreSQL, MySQL or MariaDB'],
            [['migrate', 'status', '--output=json'], 'prints text only'],
        ];
        foreach ($invalid as [$command, $message]) {
            [$code, $out, $error] = $this->command($command);
            self::assertSame(2, $code);
            self::assertSame('', $out);
            self::assertStringContainsString($message, $error);
        }
        [$code, $out, $error] = $this->command(['migrate', '--help']);
        self::assertSame(0, $code);
        self::assertSame('', $error);
        self::assertStringContainsString('migrate <up|down|status>', $out);
        self::assertSame([], (new Schema($this->database))->relations());
    }

    /**
     * @param list<string> $arguments
     * @return array{int,string,string}
     */
    private function command(array $arguments): array
    {
        $stdin = fopen('php://memory', 'w+');
        $stdout = fopen('php://memory', 'w+');
        $stderr = fopen('php://memory', 'w+');
        self::assertIsResource($stdin);
        self::assertIsResource($stdout);
        self::assertIsResource($stderr);

        try {
            $code = (new Operator($this->appDirectory, $this->migrations))->run(
                $arguments,
                $stdin,
                $stdout,
                $stderr,
                fn(string $name): ?string => $name === 'DATABASE_URL' ? $this->dsn : null,
            );
            rewind($stdout);
            rewind($stderr);

            return [$code, (string) stream_get_contents($stdout), (string) stream_get_contents($stderr)];
        } finally {
            fclose($stdin);
            fclose($stdout);
            fclose($stderr);
        }
    }

    private function installBaseline(): void
    {
        $this->writeBasicMigration();
        self::assertSame([1], $this->migrator->up($this->migrations));
    }

    private function writeBasicMigration(): void
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
