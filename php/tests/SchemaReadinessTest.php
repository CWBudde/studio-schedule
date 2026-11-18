<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use Doctrine\DBAL\DriverManager;
use InvalidArgumentException;
use PHPUnit\Framework\TestCase;
use Psr\Log\NullLogger;
use StudioSchedule\Service\DatabaseReadiness;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\Schema;

final class SchemaReadinessTest extends TestCase
{
    public function testRootDatabaseDriverIsRestrictedToSupportedEngines(): void
    {
        $this->expectException(InvalidArgumentException::class);
        Database::connect('sqlite:///private.db');
    }

    public function testExactMigrationSetAndLatestApplicationState(): void
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        $connection = $dsn === false
            ? DriverManager::getConnection(['driver' => 'pdo_sqlite', 'memory' => true])
            : Database::connect($dsn);
        $schema = new Schema($connection);
        $readiness = new DatabaseReadiness($schema, [1, 2], new NullLogger());
        self::assertFalse($readiness->ready());

        $connection->executeStatement(
            'CREATE TEMPORARY TABLE goose_db_version (id INT PRIMARY KEY, version_id BIGINT NOT NULL, is_applied BOOLEAN NOT NULL)',
        );
        foreach ([[1, 0, 1], [2, 1, 1], [3, 2, 1]] as $entry) {
            $connection->executeStatement('INSERT INTO goose_db_version VALUES (?, ?, ?)', $entry);
        }

        self::assertSame([1, 2], $schema->appliedVersions());
        self::assertTrue($readiness->ready());
        $connection->executeStatement('INSERT INTO goose_db_version VALUES (4, 2, FALSE)');
        self::assertFalse($readiness->ready());
        $connection->executeStatement('INSERT INTO goose_db_version VALUES (5, 2, TRUE)');
        self::assertTrue($readiness->ready());
        $connection->executeStatement('INSERT INTO goose_db_version VALUES (6, 9, TRUE)');
        self::assertFalse($readiness->ready());
        self::assertFalse((new DatabaseReadiness($schema, [], new NullLogger()))->ready());

        $connection->close();
    }

    public function testMigrationFileNamesAreSortedAndMatchGoNumbers(): void
    {
        $path = sys_get_temp_dir() . '/studio-migrations-' . bin2hex(random_bytes(8));
        self::assertTrue(mkdir($path));
        try {
            touch($path . '/00012_twelve.sql');
            touch($path . '/00001_first.sql');
            touch($path . '/notes.txt');
            self::assertSame([1, 12], Schema::migrationVersions($path));
        } finally {
            foreach (['00012_twelve.sql', '00001_first.sql', 'notes.txt'] as $file) {
                unlink($path . '/' . $file);
            }
            rmdir($path);
        }
        self::assertSame([], Schema::migrationVersions($path));
    }
}
