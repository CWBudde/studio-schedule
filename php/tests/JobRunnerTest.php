<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use DateInterval;
use DateTimeImmutable;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
use Psr\Clock\ClockInterface;
use Psr\Log\NullLogger;
use RuntimeException;
use StudioSchedule\Service\JobResult;
use StudioSchedule\Service\JobRunner;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\JobLocks;
use StudioSchedule\Store\JobRunStore;
use StudioSchedule\Store\MigrationSql;
use StudioSchedule\Store\Schema;

final class JobRunnerTest extends TestCase
{
    public function testDueRunsRecordContentionAndFailureWithoutLeakingDetails(): void
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($dsn === false) {
            self::markTestSkipped('Requires the disposable database from just php-test');
        }
        $database = Database::connect($dsn);
        $ownsTable = false;
        try {
            self::assertNotContains('job_runs', (new Schema($database))->relations());
            $ownsTable = true;
            $directory = $database->getDatabasePlatform() instanceof PostgreSQLPlatform
                ? (getenv('STUDIO_PHP_TEST_POSTGRES_MIGRATIONS') ?: dirname(__DIR__, 2) . '/internal/store/postgres/migrations')
                : dirname(__DIR__) . '/migrations/mysql';
            foreach (MigrationSql::up($directory . '/00028_job_runs.sql') as $statement) {
                $database->executeStatement($statement);
            }

            $clock = new class implements ClockInterface {
                public DateTimeImmutable $time;

                public function now(): DateTimeImmutable
                {
                    return $this->time;
                }
            };
            $clock->time = new DateTimeImmutable('2026-12-04T12:00:00Z');
            $interval = new DateInterval('PT1M');
            $locks = new JobLocks($dsn);
            $runner = new JobRunner($locks, new JobRunStore($database), $clock, new NullLogger());
            $name = 'fixture-delivery';
            $unexpected = static function (): never {
                self::fail('The job should not run');
            };
            self::assertTrue($locks->runIfFree($name, static function () use ($runner, $name, $interval, $unexpected): void {
                self::assertSame(JobResult::Skipped, $runner->runDue($name, $interval, $unexpected));
            }));

            $failed = $runner->runDue($name, $interval, static function (): never {
                throw new RuntimeException('private fixture transport failure');
            });
            self::assertSame(JobResult::Failed, $failed);
            self::assertNull($runner->runDue($name, $interval, $unexpected));
            $clock->time = $clock->time->add($interval);
            $successful = $runner->runDue($name, $interval, static function (): void {});
            self::assertSame(JobResult::Ok, $successful);

            self::assertSame([
                ['result' => 'skipped', 'error' => null],
                ['result' => 'failed', 'error' => 'background job failed'],
                ['result' => 'ok', 'error' => null],
            ], $database->fetchAllAssociative('SELECT result, error FROM job_runs ORDER BY started_at, id'));
        } finally {
            try {
                if ($ownsTable) {
                    $database->executeStatement('DROP TABLE IF EXISTS job_runs');
                }
            } finally {
                $database->close();
            }
        }
    }
}
