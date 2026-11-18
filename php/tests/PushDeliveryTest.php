<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use Base64Url\Base64Url;
use DateTimeImmutable;
use DateTimeZone;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
use Psr\Clock\ClockInterface;
use Psr\Http\Client\ClientInterface;
use Psr\Http\Message\RequestInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Log\NullLogger;
use Slim\Psr7\Factory\ResponseFactory;
use Slim\Psr7\Factory\StreamFactory;
use StudioSchedule\Push\Sender;
use StudioSchedule\Push\TransportError;
use StudioSchedule\Push\VapidKeys;
use StudioSchedule\Service\BackgroundJobs;
use StudioSchedule\Service\JobResult;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\Migrator;
use StudioSchedule\Store\Schema;

final class PushDeliveryTest extends TestCase
{
    private const string NOW = '2026-11-18 12:00:00.123456';
    private const string CREATED = '2026-11-18 11:00:00';
    private const string EXPIRY = '2026-11-20 12:00:00.123456';
    private const string BROWSER_KEY = 'BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4';
    private const string AUTH = 'BTBZMqHH6r4Tts7J_aSIgg';

    private Connection $db;
    private bool $postgres;

    public function testCronSendsLeasedMessagesAndRecordsProviderOutcomes(): void
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($dsn === false) {
            self::markTestSkipped('Requires the disposable database provisioned by just php-test');
        }
        $this->db = Database::connect($dsn);
        $this->postgres = $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform;
        self::assertSame([], (new Schema($this->db))->relations(), 'Push tests require an empty disposable database');
        try {
            $migrations = $this->postgres
                ? (getenv('STUDIO_PHP_TEST_POSTGRES_MIGRATIONS') ?: dirname(__DIR__, 2) . '/internal/store/postgres/migrations')
                : dirname(__DIR__) . '/migrations/mysql';
            (new Migrator($dsn))->up($migrations);
            $this->seed();
            $clock = new class implements ClockInterface {
                public DateTimeImmutable $time;

                public function now(): DateTimeImmutable
                {
                    return $this->time;
                }
            };
            $clock->time = self::time(self::NOW);
            $requests = [];
            $client = self::createStub(ClientInterface::class);
            $client->method('sendRequest')->willReturnCallback(function (RequestInterface $request) use (&$requests, $dsn): ResponseInterface {
                self::assertFalse($this->db->isTransactionActive());
                $device = basename($request->getUri()->getPath());
                $requests[] = [$device, $request->getHeaderLine('TTL')];
                self::assertSame('high', $request->getHeaderLine('Urgency'));
                self::assertSame('aes128gcm', $request->getHeaderLine('Content-Encoding'));
                self::assertStringStartsWith('vapid t=', $request->getHeaderLine('Authorization'));
                if ($device === 'transport-error') {
                    throw new TransportError('Provider log contains fixture-private-url');
                }
                if ($device === 'refreshed') {
                    $other = Database::connect($dsn, dedicated: true);
                    try {
                        $other->executeStatement(
                            'UPDATE push_subscriptions SET last_used_at = ?, auth = ? WHERE id = ?',
                            ['2026-11-18 12:00:01', str_repeat('x', 16), $this->id(18)],
                            [ParameterType::STRING, ParameterType::BINARY, $this->idType()],
                        );
                    } finally {
                        $other->close();
                    }
                }
                if ($device === 'moved') {
                    $this->db->executeStatement('UPDATE push_subscriptions SET account_id = ? WHERE id = ?', [$this->id(4), $this->id(21)]);
                }
                if ($device === 'raced') {
                    $this->db->executeStatement('UPDATE push_outbox SET attempts = 2, next_attempt_at = ? WHERE id = ?', ['2026-11-19 12:01:00', $this->id(112)]);
                }
                if ($device === 'removed') {
                    $this->db->executeStatement('DELETE FROM push_subscriptions WHERE id = ?', [$this->id(25)]);
                }
                $status = match ($device) {
                    'gone', 'refreshed', 'moved', 'raced', 'removed' => 410,
                    'refused' => 403,
                    'flaky', 'too-soon' => 503,
                    default => 201,
                };

                return (new ResponseFactory())->createResponse($status)->withBody(
                    (new StreamFactory())->createStream('fixture-private-url'),
                );
            });
            $sender = new Sender(VapidKeys::generate(), 'mailto:admin@example.test', $clock, $client);
            $jobs = new BackgroundJobs($this->db, $dsn, $clock, new NullLogger());
            if ($this->postgres) {
                $this->db->executeStatement("SET DateStyle TO 'SQL, DMY'");
            }
            try {
                $result = $jobs->deliverPush($sender);
            } finally {
                if ($this->postgres) {
                    $this->db->executeStatement("SET DateStyle TO 'ISO, YMD'");
                }
            }
            self::assertSame(JobResult::Ok, $result);
            self::assertNull($jobs->deliverPush($sender));
            self::assertSame([
                ['working', '604800'], ['gone', '172800'], ['refused', '172800'],
                ['flaky', '172800'], ['too-soon', '30'], ['transport-error', '172800'], ['refreshed', '172800'],
                ['moved', '172800'], ['raced', '172800'], ['subsecond', '0'], ['removed', '172800'],
            ], $requests);

            self::assertNotNull($this->instant($this->message(101)['sent_at']));
            self::assertSame(self::NOW, $this->instant($this->db->fetchOne('SELECT last_used_at FROM push_subscriptions WHERE id = ?', [$this->id(11)])));
            self::assertFalse($this->db->fetchOne('SELECT endpoint FROM push_subscriptions WHERE id = ?', [$this->id(12)]));
            self::assertFalse($this->db->fetchOne('SELECT id FROM push_outbox WHERE id = ?', [$this->id(102)]));
            self::assertFalse($this->db->fetchOne('SELECT id FROM push_outbox WHERE id = ?', [$this->id(150)]));
            $endpoint = $this->db->fetchOne('SELECT endpoint FROM push_subscriptions WHERE id = ?', [$this->id(18)]);
            self::assertIsString($endpoint);
            self::assertStringEndsWith('/refreshed', $endpoint);
            self::assertSame($this->id(4), $this->db->fetchOne('SELECT account_id FROM push_subscriptions WHERE id = ?', [$this->id(21)]));
            self::assertSame('2026-11-19 12:01:00.000000', $this->instant($this->message(112)['next_attempt_at']));
            self::assertSame(2, (int) $this->message(112)['attempts']);
            self::assertSame(self::NOW, $this->instant($this->message(113)['sent_at']));
            self::assertFalse($this->db->fetchOne('SELECT id FROM push_outbox WHERE id = ?', [$this->id(115)]));
            self::assertSame('2026-11-18 12:01:00.123456', $this->instant($this->message(108)['next_attempt_at']));
            self::assertSame('2026-11-18 12:01:00.123456', $this->instant($this->message(107)['next_attempt_at']));
            foreach ([103, 105, 106, 110, 111, 114] as $final) {
                self::assertSame(self::NOW, $this->instant($this->message($final)['failed_at']));
                self::assertNull($this->message($final)['next_attempt_at']);
            }
            self::assertSame('expired before delivery', $this->message(106)['last_error']);
            self::assertSame(0, (int) $this->message(106)['attempts']);
            self::assertSame(5, (int) $this->message(110)['attempts']);
            foreach ($this->db->fetchFirstColumn('SELECT last_error FROM push_outbox WHERE last_error IS NOT NULL') as $logged) {
                self::assertIsString($logged);
                self::assertStringNotContainsString('fixture-private-url', $logged);
            }

            foreach ([
                '12:01:00.123456' => '12:06:00.123456',
                '12:06:00.123456' => '12:21:00.123456',
                '12:21:00.123456' => '13:21:00.123456',
                '13:21:00.123456' => null,
            ] as $at => $next) {
                $clock->time = self::time('2026-11-18 ' . $at);
                self::assertSame(JobResult::Ok, $jobs->deliverPush($sender));
                self::assertSame($next === null ? null : '2026-11-18 ' . $next, $this->instant($this->message(104)['next_attempt_at']));
            }
            self::assertSame(5, (int) $this->message(104)['attempts']);
            self::assertSame('2026-11-18 13:21:00.123456', $this->instant($this->message(104)['failed_at']));
            $clock->time = self::time('2026-11-18 14:21:00.123456');
            self::assertSame(JobResult::Ok, $jobs->deliverPush($sender));
            self::assertCount(5, array_filter($requests, static fn(array $request): bool => $request[0] === 'flaky'));

            for ($i = 0; $i < 51; ++$i) {
                $this->enqueue(200 + $i, 11, self::EXPIRY, '2026-11-18 15:21:00.123456', 0);
            }
            $clock->time = self::time('2026-11-18 15:21:00.123456');
            $count = count($requests);
            self::assertSame(JobResult::Ok, $jobs->deliverPush($sender));
            self::assertCount($count + 50, $requests);
            self::assertNull($this->message(250)['sent_at']);
            self::assertSame(0, (int) $this->message(250)['attempts']);
        } finally {
            try {
                if (!$this->postgres) {
                    $this->db->executeStatement('SET FOREIGN_KEY_CHECKS = 0');
                }
                foreach ($this->db->createSchemaManager()->listTableNames() as $table) {
                    $this->db->executeStatement('DROP TABLE ' . $this->db->quoteSingleIdentifier($table) . ($this->postgres ? ' CASCADE' : ''));
                }
                if ($this->postgres) {
                    $this->db->executeStatement('DROP FUNCTION IF EXISTS receipts_immutable()');
                }
            } finally {
                $this->db->close();
            }
        }
    }

    private function seed(): void
    {
        $this->db->insert('persons', ['id' => $this->id(1), 'given_name' => 'Fixture', 'family_name' => 'Person']);
        $this->db->insert('accounts', ['id' => $this->id(2), 'person_id' => $this->id(1), 'email' => 'member@example.test', 'role' => 'member']);
        $this->db->insert('persons', ['id' => $this->id(3), 'given_name' => 'Other', 'family_name' => 'Person']);
        $this->db->insert('accounts', ['id' => $this->id(4), 'person_id' => $this->id(3), 'email' => 'other@example.test', 'role' => 'member']);
        foreach ([
            11 => 'working', 12 => 'gone', 13 => 'refused', 14 => 'flaky', 15 => 'too-soon',
            16 => 'expired', 17 => 'transport-error', 18 => 'refreshed', 20 => 'abandoned-final-lease',
            21 => 'moved', 22 => 'raced', 23 => 'subsecond', 24 => 'invalid', 25 => 'removed',
        ] as $id => $device) {
            $this->db->insert('push_subscriptions', [
                'id' => $this->id($id), 'account_id' => $this->id(2), 'endpoint' => 'https://push.example.test/' . $device,
                'p256dh' => $id === 24 ? str_repeat("\0", 65) : Base64Url::decode(self::BROWSER_KEY), 'auth' => Base64Url::decode(self::AUTH),
                'created_at' => self::CREATED, 'last_used_at' => self::CREATED,
            ], ['id' => $this->idType(), 'account_id' => $this->idType(), 'p256dh' => ParameterType::BINARY, 'auth' => ParameterType::BINARY]);
            $this->enqueue(90 + $id, $id, match ($id) {
                11 => '2026-12-18 12:00:00.123456',
                15 => '2026-11-18 12:00:30.123456',
                16 => self::NOW,
                23 => '2026-11-18 12:00:01.000000',
                default => self::EXPIRY,
            }, self::NOW, $id === 20 ? 5 : 0);
        }
        $this->enqueue(150, 12, self::EXPIRY, '2026-11-18 13:00:00.123456', 0);
    }

    private function enqueue(int $id, int $subscription, string $expiry, string $due, int $attempts): void
    {
        $this->db->insert('push_outbox', [
            'id' => $this->id($id), 'subscription_id' => $this->id($subscription), 'payload' => '{"title":"A place is free"}',
            'created_at' => self::CREATED, 'expires_at' => $expiry, 'next_attempt_at' => $due, 'attempts' => $attempts,
        ], ['id' => $this->idType(), 'subscription_id' => $this->idType(), 'attempts' => ParameterType::INTEGER]);
    }

    /** @return array{sent_at:?string,failed_at:?string,next_attempt_at:?string,attempts:int|numeric-string,last_error:?string} */
    private function message(int $id): array
    {
        /** @var array{sent_at:?string,failed_at:?string,next_attempt_at:?string,attempts:int|numeric-string,last_error:?string}|false $row */
        $row = $this->db->fetchAssociative('SELECT sent_at, failed_at, next_attempt_at, attempts, last_error FROM push_outbox WHERE id = ?', [$this->id($id)]);
        self::assertIsArray($row);

        return $row;
    }

    private function instant(mixed $value): ?string
    {
        return is_string($value) ? self::time($value)->setTimezone(new DateTimeZone('UTC'))->format('Y-m-d H:i:s.u') : null;
    }

    private static function time(string $value): DateTimeImmutable
    {
        return new DateTimeImmutable($value, new DateTimeZone('UTC'));
    }

    private function id(int $number): string
    {
        $uuid = sprintf('018f0000-0000-7000-8000-%012x', $number);

        return $this->postgres ? $uuid : pack('H*', str_replace('-', '', $uuid));
    }

    private function idType(): ParameterType
    {
        return $this->postgres ? ParameterType::STRING : ParameterType::BINARY;
    }
}
