<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use Base64Url\Base64Url;
use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
use Psr\Clock\ClockInterface;
use Psr\Http\Message\ResponseInterface;
use Slim\Psr7\Factory\ServerRequestFactory;
use Slim\Psr7\Factory\StreamFactory;
use Slim\Psr7\Response;
use StudioSchedule\Bootstrap;
use StudioSchedule\Http\PushRoutes;
use StudioSchedule\Push\Sender;
use StudioSchedule\Push\VapidKeys;
use StudioSchedule\Service\SessionPrincipal;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\Migrator;
use StudioSchedule\Store\PushSubscriptionStore;
use StudioSchedule\Store\Schema;

final class PushRoutesTest extends TestCase
{
    private const string BASE = 'https://school.example.test/dance';
    private const string DEVICE = 'https://push.example.test/device/fixture-private-url';
    private const string BROWSER_KEY = 'BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4';
    private const string AUTH = 'BTBZMqHH6r4Tts7J_aSIgg';
    private const string NOW = '2026-11-19 12:00:00';

    private Connection $database;
    private bool $postgres;
    private string $dsn;
    private string $postgresMigrations;

    public function testMountedSubscriptionRoutesProtectOwnershipAndQueuedMessages(): void
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($dsn === false) {
            self::markTestSkipped('Requires the disposable database from just php-test');
        }
        $this->dsn = $dsn;
        $this->database = Database::connect($dsn);
        $this->postgres = $this->database->getDatabasePlatform() instanceof PostgreSQLPlatform;
        self::assertSame([], (new Schema($this->database))->relations(), 'Requires an empty disposable database');
        $this->postgresMigrations = getenv('STUDIO_PHP_TEST_POSTGRES_MIGRATIONS') ?: dirname(__DIR__, 2) . '/internal/store/postgres/migrations';
        try {
            (new Migrator($dsn))->up($this->postgres ? $this->postgresMigrations : dirname(__DIR__) . '/migrations/mysql');
            $this->seedAccount(1, 'member');
            $this->seedAccount(2, 'member');
            $this->seedAccount(3, 'admin');
            $this->seedSession(1, 1, 'first');
            $this->seedSession(2, 2, 'second');
            $this->seedSession(3, 1, 'impersonating', 3);

            $keys = VapidKeys::generate();
            $vapid = ['VAPID_PUBLIC_KEY' => $keys->publicKey(), 'VAPID_PRIVATE_KEY' => $keys->privateKey(), 'VAPID_SUBJECT' => 'mailto:owner@example.test'];
            $device = ['endpoint' => self::DEVICE, 'keys' => ['p256dh' => self::BROWSER_KEY . '=', 'auth' => self::AUTH . '=='], 'deviceLabel' => '  My phone  '];
            foreach (['GET', 'POST', 'DELETE'] as $method) {
                self::assertSame(401, $this->request($method, null, $vapid, $device)->getStatusCode());
            }
            $off = $this->request('GET', 'first');
            self::assertSame(200, $off->getStatusCode());
            self::assertSame(['enabled' => false], json_decode((string) $off->getBody(), true, flags: JSON_THROW_ON_ERROR));
            self::assertSame(422, $this->request('POST', 'first', input: ['endpoint' => self::DEVICE])->getStatusCode());
            self::assertSame(404, $this->request('POST', 'first', input: $device)->getStatusCode());
            $status = $this->request('GET', 'first', $vapid);
            self::assertSame(200, $status->getStatusCode());
            self::assertSame('no-store', $status->getHeaderLine('Cache-Control'));
            self::assertSame(['enabled' => true, 'publicKey' => $keys->publicKey()], json_decode((string) $status->getBody(), true, flags: JSON_THROW_ON_ERROR));
            $head = $this->request('HEAD', 'first', $vapid, $device);
            self::assertSame(405, $head->getStatusCode());
            self::assertSame('GET', $head->getHeaderLine('Allow'));
            self::assertSame('', (string) $head->getBody());
            self::assertFalse($this->database->fetchOne('SELECT id FROM push_subscriptions'));
            self::assertSame(422, $this->request('POST', 'impersonating', $vapid, ['deviceLabel' => false] + $device)->getStatusCode());
            self::assertSame(403, $this->request('POST', 'impersonating', $vapid, $device)->getStatusCode());
            foreach ([
                [null, 'application/json', 400],
                ['{', 'application/json', 400],
                ['{}', 'text/plain', 415],
                ['[]', 'application/json', 422],
                [str_repeat(' ', 1024 * 1024), 'application/json', 413],
                [['endpoint' => ''], 'application/json', 422],
                [['endpoint' => self::DEVICE, 'keys' => []], 'application/json', 422],
            ] as [$input, $contentType, $expected]) {
                $problem = $this->request('POST', 'first', $vapid, $input, $contentType);
                self::assertSame($expected, $problem->getStatusCode());
                self::assertSame('application/problem+json', $problem->getHeaderLine('Content-Type'));
            }
            foreach ([
                ['endpoint', ['endpoint' => 'http://push.example.test/fixture-private-url'] + $device],
                ['endpoint', ['endpoint' => 'https://private@push.example.test/device'] + $device],
                ['keys.p256dh', ['keys' => ['p256dh' => 'invalid', 'auth' => self::AUTH]] + $device],
                ['keys.p256dh', ['keys' => ['p256dh' => self::BROWSER_KEY . "\0", 'auth' => self::AUTH]] + $device],
                ['keys.auth', ['keys' => ['p256dh' => self::BROWSER_KEY, 'auth' => 'invalid']] + $device],
                ['keys.auth', ['keys' => ['p256dh' => self::BROWSER_KEY, 'auth' => 'AAAAAAAAAAAAAAAAAAAAAA']] + $device],
                ['deviceLabel', ['deviceLabel' => str_repeat('日', 101)] + $device],
            ] as [$field, $input]) {
                $invalid = $this->request('POST', 'first', $vapid, $input);
                self::assertSame(422, $invalid->getStatusCode());
                self::assertSame('application/problem+json', $invalid->getHeaderLine('Content-Type'));
                $body = (string) $invalid->getBody();
                self::assertStringContainsString('"location":"body.' . $field . '"', $body);
                self::assertStringNotContainsString('fixture-private-url', $body);
            }
            foreach ([['fixture-private-url' => true] + $device, ['keys' => $device['keys'] + ['fixture-private-url' => true]] + $device] as $input) {
                $invalid = $this->request('POST', 'first', $vapid, $input);
                self::assertSame(422, $invalid->getStatusCode());
                self::assertStringNotContainsString('fixture-private-url', (string) $invalid->getBody());
            }
            self::assertFalse($this->database->fetchOne('SELECT id FROM push_subscriptions'));

            self::assertSame(204, $this->request('POST', 'first', $vapid, $device)->getStatusCode());
            $id = $this->database->fetchOne('SELECT id FROM push_subscriptions');
            self::assertIsString($id);
            self::assertSame($this->id(1), $this->database->fetchOne('SELECT account_id FROM push_subscriptions'));
            self::assertSame('My phone', $this->database->fetchOne('SELECT device_label FROM push_subscriptions'));
            $this->database->executeStatement(
                "INSERT INTO push_outbox (id, subscription_id, payload, created_at, expires_at, next_attempt_at) VALUES (?, ?, '{}', ?, ?, ?)",
                [$this->id(21), $id, self::NOW, '2026-11-20 12:00:00', self::NOW],
            );

            self::assertSame(204, $this->request('DELETE', 'second', input: ['endpoint' => self::DEVICE])->getStatusCode());
            self::assertSame(422, $this->request('DELETE', 'first', input: ['endpoint' => self::DEVICE, 'extra' => true])->getStatusCode());
            self::assertSame(204, $this->request('DELETE', 'first', input: ['endpoint' => self::DEVICE . "\0"])->getStatusCode());
            self::assertSame($id, $this->database->fetchOne('SELECT id FROM push_subscriptions'));
            $newAuth = 'another auth key';
            $updated = ['deviceLabel' => 'New name', 'keys' => ['p256dh' => self::BROWSER_KEY, 'auth' => Base64Url::encode($newAuth)]] + $device;
            $refresh = $this->request('POST', 'first', $vapid, $updated);
            self::assertSame(204, $refresh->getStatusCode(), (string) $refresh->getBody());
            self::assertSame('New name', $this->database->fetchOne('SELECT device_label FROM push_subscriptions'));
            $authHex = $this->database->fetchOne($this->postgres ? "SELECT encode(auth, 'hex') FROM push_subscriptions" : 'SELECT HEX(auth) FROM push_subscriptions');
            self::assertIsString($authHex);
            self::assertSame(bin2hex($newAuth), strtolower($authHex));
            self::assertSame($id, $this->database->fetchOne('SELECT id FROM push_subscriptions'));
            self::assertSame($this->id(21), $this->database->fetchOne('SELECT id FROM push_outbox'));

            $transferred = [
                'ENDPOINT' => self::DEVICE,
                'Keys' => ['P256DH' => "\u{2002}" . substr_replace(self::BROWSER_KEY, "\r\n", 12, 0) . "=\u{00a0}", 'AUTH' => self::AUTH],
                'DeviceLabel' => null,
            ];
            $move = $this->request('POST', 'second', $vapid, $transferred);
            self::assertSame(204, $move->getStatusCode(), (string) $move->getBody());
            self::assertSame($this->id(2), $this->database->fetchOne('SELECT account_id FROM push_subscriptions'));
            self::assertNotSame($id, $this->database->fetchOne('SELECT id FROM push_subscriptions'));
            self::assertFalse($this->database->fetchOne('SELECT id FROM push_outbox'));
            foreach (['first', 'second', 'second'] as $owner) {
                self::assertSame(204, $this->request('DELETE', $owner, input: ['endpoint' => self::DEVICE])->getStatusCode());
            }
            self::assertFalse($this->database->fetchOne('SELECT id FROM push_subscriptions'));
            $this->assertStaleSessionCannotSubscribe($device, $keys);
        } finally {
            if (!$this->postgres) {
                $this->database->executeStatement('SET FOREIGN_KEY_CHECKS = 0');
            }
            foreach ($this->database->createSchemaManager()->listTableNames() as $table) {
                $this->database->executeStatement('DROP TABLE ' . $this->database->quoteSingleIdentifier($table) . ($this->postgres ? ' CASCADE' : ''));
            }
            if ($this->postgres) {
                $this->database->executeStatement('DROP FUNCTION IF EXISTS receipts_immutable()');
            }
            $this->database->close();
        }
    }

    /** @param array<string,mixed> $device */
    private function assertStaleSessionCannotSubscribe(array $device, VapidKeys $keys): void
    {
        $owner = '018f0000-0000-7000-8000-000000000001';
        $request = (new ServerRequestFactory())->createServerRequest('POST', self::BASE . '/api/v1/my/push/subscriptions')
            ->withHeader('Content-Type', 'application/json')
            ->withBody((new StreamFactory())->createStream(json_encode($device, JSON_THROW_ON_ERROR)))
            ->withAttribute('sessionPrincipal', new SessionPrincipal($owner, $owner, 'member'))
            ->withAttribute('sessionToken', 'first');
        $clock = new class implements ClockInterface {
            public function now(): DateTimeImmutable
            {
                return new DateTimeImmutable('2026-11-19T12:00:00Z');
            }
        };
        $routes = new PushRoutes(new PushSubscriptionStore($this->database), $clock, static fn(): Sender => new Sender($keys, 'mailto:fixture@example.test'));

        // The session cookie was accepted before the account mutation removed the session.
        $this->database->executeStatement('DELETE FROM sessions WHERE id = ?', [$this->id(11)]);
        $response = $routes($request, new Response());
        self::assertSame(401, $response->getStatusCode(), (string) $response->getBody());
        self::assertFalse($this->database->fetchOne('SELECT id FROM push_subscriptions'));
    }

    /**
     * @param array<string,string> $vapid
     * @param array<string,mixed>|string|null $input
     */
    private function request(string $method, ?string $token, array $vapid = [], array|string|null $input = null, string $contentType = 'application/json'): ResponseInterface
    {
        $path = self::BASE . '/api/v1/my/push' . (in_array($method, ['GET', 'HEAD'], true) ? '' : '/subscriptions');
        $request = (new ServerRequestFactory())->createServerRequest($method, $path);
        if ($token !== null) {
            $request = $request->withCookieParams(['studio_session' => $token]);
        }
        if ($input !== null) {
            $request = $request->withHeader('Content-Type', $contentType)
                ->withBody((new StreamFactory())->createStream(is_array($input) ? json_encode($input, JSON_THROW_ON_ERROR) : $input));
        }
        $environment = $vapid + ['DATABASE_URL' => $this->dsn, 'BASE_URL' => self::BASE, 'LOG_LEVEL' => 'error'];
        $clock = new class implements ClockInterface {
            public function now(): DateTimeImmutable
            {
                return new DateTimeImmutable('2026-11-19 12:00:00 UTC');
            }
        };

        return Bootstrap::handle(
            static fn() => $request,
            dirname(__DIR__),
            '/missing',
            $this->postgresMigrations,
            static fn(string $key): ?string => $environment[$key] ?? null,
            clock: $clock,
        );
    }

    private function seedAccount(int $id, string $role): void
    {
        $this->database->executeStatement("INSERT INTO persons (id, given_name, family_name) VALUES (?, 'Fixture', 'Person')", [$this->id($id)]);
        $this->database->executeStatement(
            'INSERT INTO accounts (id, person_id, email, role, confirmed_at) VALUES (?, ?, ?, ?, ?)',
            [$this->id($id), $this->id($id), 'fixture' . $id . '@example.test', $role, self::NOW],
        );
    }

    private function seedSession(int $id, int $account, string $token, ?int $impersonator = null): void
    {
        $hash = $this->postgres ? "decode(?, 'hex')" : 'UNHEX(?)';
        $this->database->executeStatement(
            "INSERT INTO sessions (id, account_id, token_hash, created_at, last_seen_at, expires_at, impersonator_id) VALUES (?, ?, $hash, ?, ?, ?, ?)",
            [$this->id(10 + $id), $this->id($account), hash('sha256', $token), self::NOW, self::NOW, '2026-11-19 12:30:00', $impersonator === null ? null : $this->id($impersonator)],
        );
    }

    private function id(int $id): string
    {
        $uuid = sprintf('018f0000-0000-7000-8000-%012x', $id);

        return $this->postgres ? $uuid : pack('H*', str_replace('-', '', $uuid));
    }
}
