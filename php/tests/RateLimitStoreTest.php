<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Exception\TableNotFoundException;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use InvalidArgumentException;
use PHPUnit\Framework\TestCase;
use Psr\Clock\ClockInterface;
use Psr\Log\LoggerInterface;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\MigrationSql;
use StudioSchedule\Store\RateLimitReservation;
use StudioSchedule\Store\RateLimitStore;
use StudioSchedule\Store\Schema;

final class RateLimitStoreTest extends TestCase
{
    private const string START = '2026-12-04T12:00:00.187500Z';

    private ?Connection $database = null;
    private bool $ownsTable = false;
    private string $dsn;

    protected function setUp(): void
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($dsn === false) {
            self::markTestSkipped('Requires the disposable database from just php-test');
        }
        $this->dsn = $dsn;
        $this->database = Database::connect($dsn);
        self::assertNotContains('http_rate_limits', (new Schema($this->database))->relations());

        $directory = $this->database->getDatabasePlatform() instanceof PostgreSQLPlatform
            ? (getenv('STUDIO_PHP_TEST_POSTGRES_MIGRATIONS') ?: dirname(__DIR__, 2) . '/internal/store/postgres/migrations')
            : dirname(__DIR__) . '/migrations/mysql';
        $files = glob($directory . '/*_http_rate_limits.sql');
        self::assertIsArray($files);
        self::assertCount(1, $files, 'Expected one HTTP rate-limit migration for this engine');
        $this->ownsTable = true;
        foreach (MigrationSql::up($files[0]) as $statement) {
            $this->database->executeStatement($statement);
        }
    }

    protected function tearDown(): void
    {
        if ($this->database !== null) {
            try {
                if ($this->ownsTable) {
                    $this->database->executeStatement('DROP TABLE IF EXISTS http_rate_limits');
                }
            } finally {
                $this->database->close();
            }
        }
    }

    public function testIndependentConnectionsShareAWindowAndBlockedAttemptsDoNotExtendIt(): void
    {
        $database = $this->db();
        $worker = Database::connect($this->dsn, dedicated: true);
        try {
            $first = new RateLimitStore($database, self::at(self::START));
            $peer = new RateLimitStore($worker, self::at('2026-12-04T12:00:01Z'));
            $client = '2001:db8::f17';
            self::assertSame(0, $first->allow('calendar-ip', $client, 2, 10));
            self::assertSame(0, $peer->allow('calendar-ip', $client, 2, 10));
            self::assertSame(10, $peer->allow('calendar-ip', $client, 2, 10));
            self::assertContains($database->fetchOne('SELECT attempts FROM http_rate_limits'), [2, '2']);

            $digest = $database->getDatabasePlatform() instanceof PostgreSQLPlatform
                ? "encode(key_hash, 'hex')"
                : 'LOWER(HEX(key_hash))';
            self::assertSame(hash('sha256', $client), $database->fetchOne("SELECT $digest FROM http_rate_limits"));

            self::assertSame(0, $peer->allow('sign-in-ip', $client, 2, 10));
            self::assertSame(0, $peer->allow('calendar-ip', '2001:db8::b20', 2, 10));
            $justBefore = new RateLimitStore($worker, self::at('2026-12-04T12:00:10.187499Z'));
            self::assertSame(1, $justBefore->allow('calendar-ip', $client, 2, 10));
            $boundary = new RateLimitStore($database, self::at('2026-12-04T12:00:10.187500Z'));
            self::assertSame([0, 0, 10], [
                $boundary->allow('calendar-ip', $client, 2, 10),
                $boundary->allow('calendar-ip', $client, 2, 10),
                $boundary->allow('calendar-ip', $client, 2, 10),
            ]);
        } finally {
            $worker->close();
        }
    }

    public function testReservationsShareCapacityWithDirectAdmission(): void
    {
        $store = new RateLimitStore($this->db(), self::at(self::START));
        $first = $store->reserve('password-failures', 'member', 2, 30);
        $second = $store->reserve('password-failures', 'member', 2, 30);
        self::assertInstanceOf(RateLimitReservation::class, $first);
        self::assertInstanceOf(RateLimitReservation::class, $second);
        self::assertSame(30, $store->reserve('password-failures', 'member', 2, 30));
        self::assertSame(30, $store->allow('password-failures', 'member', 2, 30));
        self::assertSame(30, $store->blocked('password-failures', 'member', 2));

        $first->count();
        $first->release();
        self::assertSame(30, $store->blocked('password-failures', 'member', 2));

        $second->release();
        $second->count();
        self::assertSame(0, $store->allow('password-failures', 'member', 2, 30));
        self::assertSame(30, $store->blocked('password-failures', 'member', 2));
    }

    public function testClearingFailuresPreservesOtherPendingReservations(): void
    {
        $database = $this->db();
        $worker = Database::connect($this->dsn, dedicated: true);
        try {
            $first = new RateLimitStore($database, self::at(self::START));
            $peer = new RateLimitStore($worker, self::at(self::START));
            self::assertSame(0, $first->allow('password-failures', 'member', 3, 30));
            $passing = $first->reserve('password-failures', 'member', 3, 30);
            $failing = $peer->reserve('password-failures', 'member', 3, 30);
            self::assertInstanceOf(RateLimitReservation::class, $passing);
            self::assertInstanceOf(RateLimitReservation::class, $failing);

            $passing->clearFailures();
            self::assertSame(30, $peer->blocked('password-failures', 'member', 1));
            self::assertSame(0, $peer->blocked('password-failures', 'member', 2));
            self::assertSame(0, $first->allow('password-failures', 'member', 3, 30));
            $peer->forget('password-failures', 'member');
            self::assertSame(30, $first->blocked('password-failures', 'member', 1));
            self::assertSame(0, $first->blocked('password-failures', 'member', 2));

            $failing->count();
            self::assertSame(30, $first->blocked('password-failures', 'member', 1));
            self::assertContains($database->fetchOne('SELECT attempts FROM http_rate_limits'), [1, '1']);
            self::assertContains($database->fetchOne('SELECT pending FROM http_rate_limits'), [0, '0']);
        } finally {
            $worker->close();
        }
    }

    public function testLateCompletionAndAbandonmentCannotConsumeOrClearANewWindow(): void
    {
        $database = $this->db();
        $first = new RateLimitStore($database, self::at(self::START));
        $lateCount = $first->reserve('test', 'member', 2, 10);
        $lateClear = $first->reserve('test', 'member', 2, 10);
        self::assertInstanceOf(RateLimitReservation::class, $lateCount);
        self::assertInstanceOf(RateLimitReservation::class, $lateClear);

        $later = new RateLimitStore($database, self::at('2026-12-04T12:00:10.187500Z'));
        self::assertSame(0, $later->allow('test', 'member', 2, 10));
        $live = $later->reserve('test', 'member', 2, 10);
        self::assertInstanceOf(RateLimitReservation::class, $live);
        $lateCount->count();
        $lateClear->clearFailures();
        self::assertSame(10, $later->blocked('test', 'member', 2));
        self::assertContains($database->fetchOne('SELECT attempts FROM http_rate_limits'), [1, '1']);
        self::assertContains($database->fetchOne('SELECT pending FROM http_rate_limits'), [1, '1']);

        $live->release();
        self::assertSame(0, $later->blocked('test', 'member', 2));
    }

    public function testEmptyWindowStartsFreshWithTheSameFrozenClock(): void
    {
        $database = $this->db();
        $store = new RateLimitStore($database, self::at(self::START));
        $released = $store->reserve('test', 'member', 1, 10);
        self::assertInstanceOf(RateLimitReservation::class, $released);
        $digest = $database->getDatabasePlatform() instanceof PostgreSQLPlatform
            ? "encode(generation, 'hex')"
            : 'LOWER(HEX(generation))';
        $previous = $database->fetchOne("SELECT $digest FROM http_rate_limits");
        $released->release();

        $current = $store->reserve('test', 'member', 1, 10);
        self::assertInstanceOf(RateLimitReservation::class, $current);
        self::assertNotSame($previous, $database->fetchOne("SELECT $digest FROM http_rate_limits"));
        self::assertSame(10, $store->blocked('test', 'member', 1));

        $current->release();
        $moved = new RateLimitStore($database, self::at('2026-12-04T12:00:05.187500Z'));
        self::assertSame(0, $moved->allow('test', 'member', 1, 10));
        self::assertSame(10, $moved->blocked('test', 'member', 1));
    }

    public function testSettlementFailureLogsWithoutRetryingAnUncertainCount(): void
    {
        $logger = self::createMock(LoggerInterface::class);
        $logger->expects(self::once())->method('warning')
            ->with('Rate-limit reservation settlement failed', ['exception' => TableNotFoundException::class]);
        $database = $this->db();
        $store = new RateLimitStore($database, self::at(self::START), $logger);
        $reservation = $store->reserve('test', 'member', 1, 10);
        self::assertInstanceOf(RateLimitReservation::class, $reservation);
        $database->executeStatement('DROP TABLE http_rate_limits');

        $reservation->count();
        $reservation->release();
    }

    public function testSweepFailurePreservesTheReservationHandle(): void
    {
        $database = $this->db();
        $worker = $this->getMockBuilder(Connection::class)
            ->setConstructorArgs([$database->getParams(), $database->getDriver(), $database->getConfiguration()])
            ->onlyMethods(['fetchAllNumeric'])->getMock();
        $worker->expects(self::once())->method('fetchAllNumeric')
            ->willThrowException(new \Doctrine\DBAL\Exception\InvalidArgumentException('Synthetic sweep query failure'));
        try {
            $clock = self::at(self::START);
            $store = new RateLimitStore($worker, $clock);
            $peer = new RateLimitStore($database, $clock);
            $reservation = $store->reserve('test', 'member', 1, 10);
            self::assertInstanceOf(RateLimitReservation::class, $reservation);
            self::assertSame(10, $peer->blocked('test', 'member', 1));
            $reservation->release();
            self::assertSame(0, $peer->allow('test', 'member', 1, 10));
        } finally {
            $worker->close();
        }
    }

    public function testAdmissionSamplesTimeAfterLockingTheWindow(): void
    {
        $database = $this->db();
        $clock = self::createStub(ClockInterface::class);
        $clock->method('now')->willReturnCallback(static function () use ($database): DateTimeImmutable {
            self::assertTrue($database->isTransactionActive());
            self::assertContains($database->fetchOne('SELECT attempts FROM http_rate_limits'), [0, '0']);

            return new DateTimeImmutable(self::START);
        });
        $store = new RateLimitStore($database, $clock);

        self::assertSame(0, $store->allow('test', 'member', 1, 10));
    }

    public function testNewWindowsDeleteExpiredKeysAndPreserveLiveCounters(): void
    {
        $database = $this->db();
        $now = new RateLimitStore($database, self::at(self::START));
        self::assertSame(0, $now->allow('calendar-token', 'longer-lived', 1, 60));
        foreach (['first-old', 'second-old', 'third-old'] as $key) {
            self::assertSame(0, $now->allow('calendar-ip', $key, 1, 1));
        }

        $later = new RateLimitStore($database, self::at('2026-12-04T12:00:02Z'));
        self::assertSame(0, $later->allow('calendar-ip', 'new-address', 1, 1));
        self::assertSame(['calendar-ip', 'calendar-token'], $database->fetchFirstColumn('SELECT bucket FROM http_rate_limits ORDER BY bucket'));
        self::assertSame(59, $later->allow('calendar-token', 'longer-lived', 1, 60));
    }

    public function testConcurrentWorkersAllowOnlyTheConfiguredNumber(): void
    {
        $worker = <<<'WORKER'
            <?php
            require 'vendor/autoload.php';
            $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
            $entry = new StudioSchedule\Store\RateLimitStore(
                StudioSchedule\Store\Database::connect($dsn),
                new class implements Psr\Clock\ClockInterface {
                    public function now(): DateTimeImmutable {
                        return new DateTimeImmutable('2026-12-04T12:00:00Z');
                    }
                },
            );
            echo "ready\n";
            fgets(STDIN);
            $results = [];
            $reserved = [];
            for ($j = 0; $j < 4; $j++) {
                $result = $argv[1] === 'reserve'
                    ? $entry->reserve('calendar-token', 'one-feed', 7, 60)
                    : $entry->allow('calendar-token', 'one-feed', 7, 60);
                if ($result instanceof StudioSchedule\Store\RateLimitReservation) {
                    $reserved[] = $result;
                    $result = 0;
                }
                $results[] = $result;
            }
            foreach ($reserved as $reservation) {
                $reservation->count();
            }
            echo json_encode($results);
            WORKER;
        $workers = [];
        try {
            for ($i = 0; $i < 6; $i++) {
                $process = proc_open(
                    [PHP_BINARY, '-r', substr($worker, 6), '--', $i % 2 === 0 ? 'reserve' : 'allow'],
                    [['pipe', 'r'], ['pipe', 'w'], ['pipe', 'w']],
                    $pipes,
                    dirname(__DIR__),
                );
                self::assertIsResource($process);
                $workers[] = [$process, $pipes];
            }
            foreach ($workers as [, $pipes]) {
                self::assertSame("ready\n", fgets($pipes[1]));
            }
            foreach ($workers as [, $pipes]) {
                self::assertSame(3, fwrite($pipes[0], "go\n"));
            }
            $results = [];
            foreach ($workers as [, $pipes]) {
                $stdout = stream_get_contents($pipes[1]);
                $stderr = stream_get_contents($pipes[2]);
                self::assertIsString($stdout);
                self::assertSame('', $stderr);
                $values = json_decode($stdout, associative: true, flags: JSON_THROW_ON_ERROR);
                self::assertIsArray($values);
                array_push($results, ...array_values($values));
            }

            $outcomes = array_count_values(array_filter($results, is_int(...)));
            ksort($outcomes);
            self::assertSame([0 => 7, 60 => 17], $outcomes);
        } finally {
            foreach ($workers as [$process, $pipes]) {
                foreach ($pipes as $pipe) {
                    fclose($pipe);
                }
                proc_close($process);
            }
        }
    }

    public function testRejectsAZeroLimitOrWindow(): void
    {
        $store = new RateLimitStore($this->db(), self::at(self::START));
        foreach ([[0, 10], [1, 0]] as [$limit, $window]) {
            try {
                $store->allow('test', 'fixture', $limit, $window);
                self::fail('Expected an invalid rate-limit configuration to fail');
            } catch (InvalidArgumentException) {
                self::assertSame([], $this->db()->fetchFirstColumn('SELECT attempts FROM http_rate_limits'));
            }
        }
    }

    private function db(): Connection
    {
        self::assertInstanceOf(Connection::class, $this->database);
        return $this->database;
    }

    private static function at(string $instant): ClockInterface
    {
        return new class ($instant) implements ClockInterface {
            public function __construct(private readonly string $instant) {}

            public function now(): DateTimeImmutable
            {
                return new DateTimeImmutable($this->instant);
            }
        };
    }
}
