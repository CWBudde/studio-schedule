<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
use Slim\Psr7\Factory\ServerRequestFactory;
use StudioSchedule\Bootstrap;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\Migrator;
use StudioSchedule\Store\Schema;

final class BootstrapConnectionTest extends TestCase
{
    public function testRepeatedRequestsReleaseConnectionsWithoutWaitingForGarbageCollection(): void
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($dsn === false) {
            self::markTestSkipped('Requires the disposable database from just php-test');
        }
        $database = Database::connect($dsn);
        self::assertSame([], (new Schema($database))->relations(), 'Requires an empty disposable database');
        $postgres = $database->getDatabasePlatform() instanceof PostgreSQLPlatform;
        $pgMigrations = getenv('STUDIO_PHP_TEST_POSTGRES_MIGRATIONS') ?: dirname(__DIR__, 2) . '/internal/store/postgres/migrations';
        $migrations = $postgres ? $pgMigrations : dirname(__DIR__) . '/migrations/mysql';
        $environment = ['DATABASE_URL' => $dsn, 'LOG_LEVEL' => 'error'];
        $lookup = static fn(string $key): ?string => $environment[$key] ?? null;
        $request = static fn() => (new ServerRequestFactory())->createServerRequest('GET', '/api/v1/health');

        $collectionEnabled = gc_enabled();
        try {
            (new Migrator($dsn))->up($migrations);
            gc_collect_cycles();
            gc_disable();
            for ($attempt = 0; $attempt < 110; $attempt++) {
                $response = Bootstrap::handle($request, dirname(__DIR__), '/missing', $pgMigrations, $lookup);
                self::assertSame(200, $response->getStatusCode(), "Request $attempt lost database access");
            }
        } finally {
            if ($collectionEnabled) {
                gc_enable();
            }
            gc_collect_cycles();
            if (!$postgres) {
                $database->executeStatement('SET FOREIGN_KEY_CHECKS = 0');
            }
            foreach ($database->createSchemaManager()->listTableNames() as $table) {
                $database->executeStatement('DROP TABLE IF EXISTS ' . $database->quoteSingleIdentifier($table) . ($postgres ? ' CASCADE' : ''));
            }
            if ($postgres) {
                $database->executeStatement('DROP FUNCTION IF EXISTS receipts_immutable()');
            }
            $database->close();
        }
    }
}
