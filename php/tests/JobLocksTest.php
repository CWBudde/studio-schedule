<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use PHPUnit\Framework\TestCase;
use RuntimeException;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\JobLocks;

final class JobLocksTest extends TestCase
{
    public function testDedicatedConnectionsRefuseOverlapAndReleaseOnFailure(): void
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($dsn === false) {
            self::markTestSkipped('Set STUDIO_PHP_TEST_DATABASE_URL to exercise real advisory locks');
        }
        $first = new JobLocks($dsn);
        $second = new JobLocks($dsn);
        $name = 'fixture-' . bin2hex(random_bytes(8));
        $ran = 0;
        $run = static function () use (&$ran): void {
            ++$ran;
        };
        self::assertTrue($first->runIfFree($name, static function () use ($second, $name, $run): void {
            self::assertFalse($second->runIfFree($name, $run));
            self::assertTrue($second->runIfFree($name . '-other', $run));
        }));
        self::assertSame(1, $ran);

        try {
            $first->runIfFree($name, static function (): never {
                throw new RuntimeException('fixture job failed');
            });
            self::fail('job exception did not propagate');
        } catch (RuntimeException $error) {
            self::assertSame('fixture job failed', $error->getMessage());
        }
        self::assertTrue($second->runIfFree($name, $run));
        self::assertSame(2, $ran);
    }

    public function testPostgresUsesTheSameLockAsGo(): void
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($dsn === false || !str_starts_with($dsn, 'postgres')) {
            self::markTestSkipped('Set a PostgreSQL test connection to check Go/PHP lock parity');
        }
        $connection = Database::connect($dsn);
        $name = 'fixture-' . bin2hex(random_bytes(8));
        try {
            $connection->executeQuery('SELECT pg_advisory_lock(20, hashtext(?))', [$name]);
            $locks = new JobLocks($dsn);
            self::assertFalse($locks->runIfFree($name, static function (): never {
                self::fail('PHP ran a job while Go held its advisory lock');
            }));
        } finally {
            $connection->close();
        }
    }
}
