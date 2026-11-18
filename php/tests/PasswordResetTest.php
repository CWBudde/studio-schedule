<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use Closure;
use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
use Psr\Clock\ClockInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Http\Message\StreamInterface;
use RuntimeException;
use Slim\Psr7\Factory\ServerRequestFactory;
use Slim\Psr7\Response;
use Slim\Psr7\Stream;
use StudioSchedule\Bootstrap;
use StudioSchedule\Http\AfterResponse;
use StudioSchedule\Http\PasswordResetEndpoint;
use StudioSchedule\Mail\Renderer;
use StudioSchedule\Service\CommonPasswords;
use StudioSchedule\Service\PasswordHash;
use StudioSchedule\Service\PasswordReset;
use StudioSchedule\Service\SchoolSetup;
use StudioSchedule\Service\SetupInput;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\Migrator;
use StudioSchedule\Store\PasswordResetStore;
use StudioSchedule\Store\Schema;
use StudioSchedule\Store\SetupStore;

final class PasswordResetTest extends TestCase
{
    private const string ACCOUNT = '00000000-0000-4000-8000-000000000001';
    private const string OTHER = '00000000-0000-4000-8000-000000000002';
    private const string EMAIL = 'ada@fictional-studio.example';
    private const string NOW = '2026-03-26 10:15:27.123456';
    private const string PASSWORD = 'rêverie sous les projecteurs';

    private static ?string $dsn = null;
    private static bool $migrated = false;
    private Connection $db;
    private bool $postgres;

    public static function setUpBeforeClass(): void
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($dsn === false) {
            self::markTestSkipped('Requires the disposable database provisioned by just php-test');
        }
        $connection = Database::connect($dsn);
        try {
            self::assertSame([], (new Schema($connection))->relations(), 'Reset tests require an empty disposable database');
            self::$dsn = $dsn;
        } finally {
            $connection->close();
        }
    }

    protected function setUp(): void
    {
        $this->db = Database::connect(self::dsn());
        $this->postgres = $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform;
    }

    protected function tearDown(): void
    {
        try {
            if (self::$migrated) {
                foreach (['job_runs', 'audit_events', 'outbox_mails', 'push_subscriptions', 'sessions', 'account_tokens', 'account_credentials', 'accounts', 'persons', 'settings'] as $table) {
                    $this->db->executeStatement('DELETE FROM ' . $table);
                }
            }
        } finally {
            $this->db->close();
        }
    }

    public static function tearDownAfterClass(): void
    {
        if (self::$dsn === null) {
            return;
        }
        $db = Database::connect(self::$dsn);
        $postgres = $db->getDatabasePlatform() instanceof PostgreSQLPlatform;

        try {
            if (!$postgres) {
                $db->executeStatement('SET FOREIGN_KEY_CHECKS = 0');
            }
            foreach ($db->createSchemaManager()->listTableNames() as $table) {
                $suffix = $postgres ? ' CASCADE' : '';
                $db->executeStatement('DROP TABLE IF EXISTS ' . $db->quoteSingleIdentifier($table) . $suffix);
            }
            if ($postgres) {
                $db->executeStatement('DROP FUNCTION IF EXISTS receipts_immutable()');
            }
        } finally {
            $db->close();
            self::$dsn = null;
            self::$migrated = false;
        }
    }

    public function testFirstAdminCanSetPasswordOnlyOnce(): void
    {
        $this->migrate();
        $setup = new SchoolSetup(new SetupStore($this->db), self::clock());
        $result = $setup->init(new SetupInput('Example Studio', 'UTC', 'en', 'EUR', self::EMAIL, 'Ada', 'Example'));

        self::assertSame(410, $this->confirm('unknown-token')->getStatusCode());
        $success = $this->confirm($result->token, str_repeat('€', 100));
        self::assertSame(204, $success->getStatusCode());
        self::assertSame('', (string) $success->getBody());
        self::assertFalse($success->hasHeader('Set-Cookie'));
        self::assertSame(self::clock()->now()->getTimestamp(), $this->timestamp('SELECT confirmed_at FROM accounts'));
        self::assertSame(self::clock()->now()->getTimestamp(), $this->timestamp('SELECT changed_at FROM account_credentials'));
        $hash = $this->db->fetchOne('SELECT password_hash FROM account_credentials');
        self::assertIsString($hash);
        self::assertTrue((new PasswordHash())->verify($hash, str_repeat('€', 100)));
        self::assertSame(0, $this->countRows('account_tokens'));
        self::assertSame(0, $this->countRows('outbox_mails'));
        $used = $this->confirm($result->token);
        self::assertSame(410, $used->getStatusCode());
        self::assertStringContainsString('the link is invalid or has expired', (string) $used->getBody());
        self::assertStringNotContainsString($result->token, (string) $used->getBody());
        self::assertSame($hash, $this->db->fetchOne('SELECT password_hash FROM account_credentials'));
    }

    public function testRetriedResetRevokesSessionsTokensAndPushAndQueuesOnePasswordChangeNotice(): void
    {
        $this->migrate();
        $this->addAccount(self::ACCOUNT, self::EMAIL, true);
        $this->addAccount(self::OTHER, 'other@fictional-studio.example', true);
        $id = $this->id(self::ACCOUNT);
        $other = $this->id(self::OTHER);
        $this->db->executeStatement("UPDATE accounts SET locale = 'de' WHERE id = ?", [$id]);
        $this->addSettings();
        $this->addCredential();
        $this->addToken('reset-token');
        $this->addToken('other-credential-link', 'change-email');
        $this->addToken('confirmation-link', 'confirm-email');
        $this->addToken('unrelated-link', account: self::OTHER);
        $this->addSession(3, $id, null, '2026-03-25', '2026-03-27');
        $this->addSession(4, $id, $other, '2026-03-25', '2026-03-27');
        $this->addSession(5, $other, $id, '2026-03-25', '2026-03-27');
        $this->addSession(6, $other, $id, '2026-02-10', '2026-03-27');
        $this->addSession(7, $id, $other, '2026-03-24', '2026-03-26');
        $this->addSession(8, $other, null, '2026-03-25', '2026-03-27');
        $pushId = $this->id('00000000-0000-4000-8000-000000000009');
        $bytes = $this->postgres ? "decode(?, 'hex')" : 'UNHEX(?)';
        $this->db->executeStatement(
            'INSERT INTO push_subscriptions (id, account_id, endpoint, p256dh, auth, created_at, last_used_at) '
            . "VALUES (?, ?, ?, $bytes, $bytes, ?, ?)",
            [$pushId, $id, 'https://push.example.test/pad', str_repeat('42', 65), str_repeat('45', 16), self::NOW, self::NOW],
        );

        $this->makeOutboxInsertRetryOnce();
        $wakes = 0;
        $wakeMail = function () use (&$wakes): void {
            self::assertFalse($this->db->isTransactionActive());
            self::assertSame(1, $this->countRows('outbox_mails'));
            ++$wakes;
        };
        try {
            self::assertSame(204, $this->confirm('reset-token', wakeMail: $wakeMail)->getStatusCode());
            self::assertSame(1, $wakes);
            $attempts = $this->db->fetchOne(
                $this->postgres ? 'SELECT last_value FROM fixture_reset_attempts' : 'SELECT attempts FROM fixture_reset_attempts',
            );
            self::assertContains($attempts, [2, '2']);
        } finally {
            if ($this->postgres) {
                $this->db->executeStatement('DROP TRIGGER fixture_reset_retry ON outbox_mails');
                $this->db->executeStatement('DROP FUNCTION fixture_reset_retry()');
                $this->db->executeStatement('DROP SEQUENCE fixture_reset_attempts');
            } else {
                $this->db->executeStatement('DROP TRIGGER fixture_reset_retry');
                $this->db->executeStatement('DROP TABLE fixture_reset_attempts');
            }
        }
        self::assertSame(410, $this->confirm('reset-token', wakeMail: $wakeMail)->getStatusCode());
        self::assertSame(1, $wakes);
        self::assertSame('2026-03-25', gmdate('Y-m-d', $this->timestamp('SELECT confirmed_at FROM accounts WHERE id = ?', [$id])));
        $hash = $this->db->fetchOne('SELECT password_hash FROM account_credentials');
        self::assertIsString($hash);
        self::assertTrue((new PasswordHash())->verify($hash, self::PASSWORD));
        self::assertSame([$other], $this->db->fetchFirstColumn('SELECT account_id FROM sessions'));
        self::assertSame([$other], $this->db->fetchFirstColumn('SELECT account_id FROM account_tokens'));
        self::assertSame(0, $this->countRows('push_subscriptions'));
        self::assertSame([
            [$id, 'impersonation-ended', 'account', $other, null],
            [$other, 'impersonation-ended', 'account', $id, null],
        ], $this->db->fetchAllNumeric('SELECT actor_id, action, target_kind, target_id, details FROM audit_events ORDER BY actor_id'));

        $mail = $this->db->fetchAssociative('SELECT recipient, subject, body_text, body_html, template, attempts FROM outbox_mails');
        self::assertIsArray($mail);
        self::assertSame(self::EMAIL, $mail['recipient']);
        self::assertSame('Dein Passwort bei Beispielstudio wurde geändert', $mail['subject']);
        self::assertIsString($mail['body_text']);
        self::assertIsString($mail['body_html']);
        self::assertStringContainsString('Hallo Ada', $mail['body_text']);
        self::assertStringContainsString('Beispielstudio', $mail['body_html']);
        self::assertStringNotContainsString(self::PASSWORD, $mail['body_text']);
        self::assertStringNotContainsString('reset-token', $mail['body_html']);
        self::assertSame('password-changed', $mail['template']);
        self::assertContains($mail['attempts'], [0, '0']);
        self::assertSame(1, $this->countRows('outbox_mails'));
        self::assertSame(self::clock()->now()->getTimestamp(), $this->timestamp('SELECT next_attempt_at FROM outbox_mails'));
    }

    public function testOnlyMatchingCurrentResetTokensCanConfirmAnAccount(): void
    {
        $this->migrate();
        $this->addAccount(self::ACCOUNT, self::EMAIL, false);
        $this->addToken('expired', expires: self::NOW);
        $this->addToken('wrong-purpose', 'confirm-email');
        $this->addToken('different-address', email: 'Ada@fictional-studio.example');
        foreach (['expired', 'wrong-purpose', 'different-address'] as $token) {
            self::assertSame(410, $this->confirm($token)->getStatusCode());
        }

        $this->addToken('disabled');
        $this->db->executeStatement('UPDATE accounts SET disabled_at = ?', [self::NOW]);
        self::assertSame(410, $this->confirm('disabled')->getStatusCode());
        $this->db->executeStatement('UPDATE accounts SET disabled_at = NULL');
        $this->addCredential();
        self::assertSame(410, $this->confirm('disabled')->getStatusCode(), 'An unconfirmed registration needs its confirmation link');
        self::assertNull($this->db->fetchOne('SELECT confirmed_at FROM accounts'));
        self::assertSame(4, $this->countRows('account_tokens'));
    }

    public function testMountedPasswordResetWakesDeliveryAfterTheResponseIsFinished(): void
    {
        $this->migrate();
        $this->addSettings();
        $this->addAccount(self::ACCOUNT, self::EMAIL, true);
        $this->addCredential();
        $this->addToken('reset-token');

        // Reserve a local port without listening so the attempt is retryable.
        $socket = stream_socket_server('tcp://127.0.0.1:0', flags: STREAM_SERVER_BIND);
        self::assertIsResource($socket);
        try {
            $address = stream_socket_get_name($socket, false);
            self::assertIsString($address);
            $port = parse_url('tcp://' . $address, PHP_URL_PORT);
            self::assertIsInt($port);
            $values = [
                'BASE_URL' => 'http://example.test/dance/schedule',
                'DATABASE_URL' => self::dsn(),
                'SMTP_HOST' => '127.0.0.1',
                'SMTP_PORT' => (string) $port,
                'SMTP_TLS' => 'none',
                'SMTP_FROM' => 'studio@example.test',
                'SMTP_USERNAME' => '',
                'SMTP_PASSWORD' => '',
            ];
            $request = (new ServerRequestFactory())->createServerRequest('POST', $values['BASE_URL'] . '/api/v1/auth/password-reset/confirm')
                ->withHeader('Content-Type', 'application/json');
            $request->getBody()->write(json_encode(['token' => 'reset-token', 'password' => self::PASSWORD], JSON_THROW_ON_ERROR));
            $request->getBody()->rewind();
            $after = new AfterResponse();
            $response = Bootstrap::handle(
                static fn() => $request,
                dirname(__DIR__),
                '/missing/studio-web',
                getenv('STUDIO_PHP_TEST_POSTGRES_MIGRATIONS') ?: dirname(__DIR__, 2) . '/internal/store/postgres/migrations',
                static fn(string $key): ?string => $values[$key] ?? null,
                getenv('STUDIO_PHP_TEST_COMMON_PASSWORDS') ?: dirname(__DIR__, 2) . '/internal/auth/password/common-passwords.txt',
                self::clock(),
                $after,
            );
            self::assertSame(204, $response->getStatusCode());
            self::assertContains($this->db->fetchOne('SELECT attempts FROM outbox_mails'), [0, '0']);
            $flushed = false;
            $after->run(function () use (&$flushed): bool {
                self::assertSame(0, $this->countRows('job_runs'));
                $flushed = true;

                return true;
            });
            self::assertTrue($flushed);
            self::assertContains($this->db->fetchOne('SELECT attempts FROM outbox_mails'), [1, '1']);
            self::assertNull($this->db->fetchOne('SELECT sent_at FROM outbox_mails'));
            self::assertSame(['ok'], $this->db->fetchFirstColumn("SELECT result FROM job_runs WHERE job = 'deliver-mail'"));
        } finally {
            fclose($socket);
        }
    }

    public function testResetRechecksTheTokenIfAnotherRequestAlreadySetThePassword(): void
    {
        $this->migrate();
        $this->addAccount(self::ACCOUNT, self::EMAIL, false);
        $this->addToken('stale-link');
        $this->addToken('winning-link');

        $otherConnection = Database::connect(self::dsn());
        $winnerPassword = 'Fictional evening rehearsal';
        $clock = new class (self::clock()->now(), function () use ($otherConnection, $winnerPassword): void {
            // The first request just read its token but has yet to hash the password.
            $winner = $this->confirm('winning-link', $winnerPassword, connection: $otherConnection);
            self::assertSame(204, $winner->getStatusCode());
        }) implements ClockInterface {
            private ?Closure $afterRead;

            public function __construct(private readonly DateTimeImmutable $now, Closure $afterRead)
            {
                $this->afterRead = $afterRead;
            }

            public function now(): DateTimeImmutable
            {
                $afterRead = $this->afterRead;
                $this->afterRead = null;
                $afterRead?->__invoke();

                return $this->now;
            }
        };

        try {
            self::assertSame(410, $this->confirm('stale-link', clock: $clock)->getStatusCode());
        } finally {
            $otherConnection->close();
        }
        $hash = $this->db->fetchOne('SELECT password_hash FROM account_credentials');
        self::assertIsString($hash);
        self::assertTrue((new PasswordHash())->verify($hash, $winnerPassword));
        self::assertSame(0, $this->countRows('account_tokens'));
    }

    public function testPasswordAndSessionsRollBackIfTheNoticeCannotBeQueued(): void
    {
        $this->migrate();
        $this->addAccount(self::ACCOUNT, self::EMAIL, true);
        $this->addCredential();
        $this->addSession(3, $this->id(self::ACCOUNT), null, '2026-03-25', '2026-03-27');
        $this->addToken('reset-token');

        // The notice cannot be prepared without a settings row.
        $this->expectException(RuntimeException::class);
        $this->expectExceptionMessage('Password change mail requires school settings');
        try {
            $this->confirm('reset-token');
        } finally {
            self::assertSame('fixture-previous-hash', $this->db->fetchOne('SELECT password_hash FROM account_credentials'));
            self::assertSame(1, $this->countRows('sessions'));
            self::assertSame(1, $this->countRows('account_tokens'));
            self::assertSame(0, $this->countRows('outbox_mails'));
        }
    }

    public function testInvalidCredentialsReturnRedactedFieldErrors(): void
    {
        $invalidBodies = [
            ['{"token":"test-secret-token","password":"short"}', 422, 'body.password', 'expected length >= 8'],
            [json_encode(['token' => 'test-secret-token', 'password' => str_repeat('€', 257)], JSON_THROW_ON_ERROR), 422, 'body.password', 'expected length <= 256'],
            ['{"token":"test-secret-token","password":"Sunshine"}', 422, 'body.password', 'the password is one of the most common ones; choose another'],
            ['{"token":"test-secret-token","password":5}', 422, 'body.password', 'expected string'],
            ['{"token":"test-secret-token"}', 422, 'body', 'expected required property password to be present'],
            ['{"token":"","password":"test-private-password"}', 422, 'body.token', 'expected length >= 1'],
            ['{"test-secret-token":"test-private-password","token":"x","password":"drawn from the viewport"}', 422, 'body', 'unexpected property'],
            ['[]', 422, 'body', 'expected object'],
            ['{"token":"test-secret-token",', 400, 'body', 'invalid JSON'],
        ];
        foreach ($invalidBodies as [$body, $status, $location, $message]) {
            $response = $this->request($body);
            self::assertSame($status, $response->getStatusCode());
            self::assertSame('application/problem+json', $response->getHeaderLine('Content-Type'));
            self::assertSame('no-store', $response->getHeaderLine('Cache-Control'));
            $problem = (string) $response->getBody();
            self::assertStringNotContainsString('test-secret-token', $problem);
            self::assertStringNotContainsString('test-private-password', $problem);
            self::assertStringNotContainsString('Sunshine', $problem);
            self::assertStringContainsString('"requestId":"password-reset-test"', $problem);
            self::assertStringContainsString(json_encode(['message' => $message, 'location' => $location], JSON_THROW_ON_ERROR), $problem);
        }
    }

    public function testResetRejectsMissingOversizedAndUnsupportedRequestBodies(): void
    {
        self::assertSame(400, $this->request('')->getStatusCode());
        self::assertSame(413, $this->request(str_repeat(' ', 1024 * 1024))->getStatusCode());
        self::assertSame(415, $this->request('{"token":"test","password":"test-password"}', contentType: 'text/plain')->getStatusCode());
    }

    public function testResetReadsAllChunksOfTheRequestBody(): void
    {
        $resource = fopen('php://temp', 'r+');
        self::assertIsResource($resource);
        $stream = new class ($resource) extends Stream {
            /** @param int $length */
            public function read($length): string
            {
                return parent::read(min($length, 7));
            }
        };
        $response = $this->request('{"token":"test-token","password":"Sunshine"}', stream: $stream);
        self::assertSame(422, $response->getStatusCode());
        self::assertStringContainsString('the password is one of the most common ones', (string) $response->getBody());
    }

    private function confirm(
        string $token,
        string $password = self::PASSWORD,
        ?ClockInterface $clock = null,
        ?Connection $connection = null,
        ?Closure $wakeMail = null,
    ): ResponseInterface {
        return $this->request(json_encode(['token' => $token, 'password' => $password], JSON_THROW_ON_ERROR), $clock, $connection, wakeMail: $wakeMail);
    }

    private function request(
        string $body,
        ?ClockInterface $clock = null,
        ?Connection $connection = null,
        string $contentType = 'application/json',
        ?StreamInterface $stream = null,
        ?Closure $wakeMail = null,
    ): ResponseInterface {
        $controller = new PasswordResetEndpoint(new PasswordReset(
            new PasswordResetStore($connection ?? $this->db),
            new PasswordHash(),
            new CommonPasswords(getenv('STUDIO_PHP_TEST_COMMON_PASSWORDS') ?: __DIR__ . '/../../internal/auth/password/common-passwords.txt'),
            $clock ?? self::clock(),
            new Renderer(),
            $wakeMail,
        ));
        $request = (new ServerRequestFactory())->createServerRequest('POST', '/api/v1/auth/password-reset/confirm')
            ->withHeader('Content-Type', $contentType)
            ->withAttribute('requestId', 'password-reset-test');
        if ($stream !== null) {
            $request = $request->withBody($stream);
        }
        $request->getBody()->write($body);
        $request->getBody()->rewind();

        return $controller($request, new Response());
    }

    private function migrate(): void
    {
        if (self::$migrated) {
            return;
        }
        $migrations = $this->postgres
            ? (getenv('STUDIO_PHP_TEST_POSTGRES_MIGRATIONS') ?: __DIR__ . '/../../internal/store/postgres/migrations')
            : __DIR__ . '/../migrations/mysql';
        (new Migrator(self::dsn()))->up($migrations);
        self::$migrated = true;
    }

    private static function dsn(): string
    {
        self::assertNotNull(self::$dsn);

        return self::$dsn;
    }

    private function addAccount(string $account, string $email, bool $confirmed): void
    {
        $id = $this->id($account);
        $this->db->executeStatement("INSERT INTO persons (id, given_name, family_name) VALUES (?, 'Ada', 'Example')", [$id]);
        $this->db->executeStatement(
            "INSERT INTO accounts (id, person_id, email, role, confirmed_at) VALUES (?, ?, ?, 'admin', ?)",
            [$id, $id, $email, $confirmed ? '2026-03-25 10:15:27' : null],
        );
    }

    private function addSettings(): void
    {
        $duration = $this->postgres ? "'0 seconds'" : '0';
        $expiry = $this->postgres ? "'12 hours'" : '43200000000';
        $this->db->executeStatement(
            'INSERT INTO settings (school_name, timezone, default_locale, currency, receipt_prefix, release_weekday, release_time, '
            . 'release_weeks_ahead, default_visible_days, booking_cutoff, cancellation_cutoff, waitlist_invite_expiry) '
            . "VALUES ('Beispielstudio', 'UTC', 'en', 'EUR', 'R', 0, '18:00:00', 2, 14, $duration, $duration, $expiry)",
        );
    }

    private function addCredential(): void
    {
        $this->db->executeStatement(
            'INSERT INTO account_credentials (account_id, password_hash, changed_at) VALUES (?, ?, ?)',
            [$this->id(self::ACCOUNT), 'fixture-previous-hash', '2026-03-25 10:15:27'],
        );
    }

    private function addToken(
        string $token,
        string $purpose = 'reset-password',
        string $email = self::EMAIL,
        string $expires = '2026-03-27 10:00:00',
        string $account = self::ACCOUNT,
    ): void {
        $bytes = $this->postgres ? "decode(?, 'hex')" : 'UNHEX(?)';
        $this->db->executeStatement('INSERT INTO account_tokens (token_hash, account_id, purpose, email, created_at, expires_at) '
            . "VALUES ($bytes, ?, ?, ?, ?, ?)", [hash('sha256', $token), $this->id($account), $purpose, $email, '2026-03-25 10:00:00', $expires]);
    }

    private function addSession(int $number, string $account, ?string $actor, string $created, string $expires): void
    {
        $id = $this->id('00000000-0000-4000-8000-00000000000' . $number);
        $bytes = $this->postgres ? "decode(?, 'hex')" : 'UNHEX(?)';
        $this->db->executeStatement(
            'INSERT INTO sessions (id, account_id, impersonator_id, token_hash, created_at, last_seen_at, expires_at) '
            . "VALUES (?, ?, ?, $bytes, ?, ?, ?)",
            [$id, $account, $actor, hash('sha256', 'session' . $number), $created . ' 00:00:00', $created . ' 00:00:00', $expires . ' 00:00:00'],
        );
    }

    private function makeOutboxInsertRetryOnce(): void
    {
        // Fail after the password, sessions, audit events and tokens were written.
        // The counters survive rollback so the next attempt can commit.
        if ($this->postgres) {
            $this->db->executeStatement('CREATE SEQUENCE fixture_reset_attempts');
            $this->db->executeStatement(<<<'SQL'
                CREATE FUNCTION fixture_reset_retry() RETURNS trigger LANGUAGE plpgsql AS $$
                BEGIN
                    IF nextval('fixture_reset_attempts') = 1 THEN
                        RAISE EXCEPTION 'fixture serialization failure' USING ERRCODE = '40001';
                    END IF;
                    RETURN NEW;
                END;
                $$
                SQL);
            $this->db->executeStatement('CREATE TRIGGER fixture_reset_retry BEFORE INSERT ON outbox_mails '
                . 'FOR EACH ROW EXECUTE FUNCTION fixture_reset_retry()');
        } else {
            $this->db->executeStatement('CREATE TABLE fixture_reset_attempts (attempts INT NOT NULL) ENGINE=MyISAM');
            $this->db->executeStatement('INSERT INTO fixture_reset_attempts VALUES (0)');
            $this->db->executeStatement(<<<'SQL'
                CREATE TRIGGER fixture_reset_retry BEFORE INSERT ON outbox_mails FOR EACH ROW
                BEGIN
                    UPDATE fixture_reset_attempts SET attempts = attempts + 1;
                    IF (SELECT attempts FROM fixture_reset_attempts) = 1 THEN
                        SIGNAL SQLSTATE '40001' SET MYSQL_ERRNO = 1213, MESSAGE_TEXT = 'fixture deadlock';
                    END IF;
                END
                SQL);
        }
    }

    private function id(string $uuid): string
    {
        if ($this->postgres) {
            return $uuid;
        }

        return pack('H*', str_replace('-', '', $uuid));
    }

    private function countRows(string $table): int
    {
        $count = $this->db->fetchOne('SELECT COUNT(*) FROM ' . $table);
        self::assertIsNumeric($count);

        return (int) $count;
    }

    /** @param list<string> $parameters */
    private function timestamp(string $query, array $parameters = []): int
    {
        $value = $this->db->fetchOne($query, $parameters);
        self::assertIsString($value);

        return (new DateTimeImmutable($value . ($this->postgres ? '' : ' UTC')))->getTimestamp();
    }

    private static function clock(): ClockInterface
    {
        return new class implements ClockInterface {
            public function now(): DateTimeImmutable
            {
                return new DateTimeImmutable('2026-03-26T11:15:27.123456+01:00');
            }
        };
    }
}
