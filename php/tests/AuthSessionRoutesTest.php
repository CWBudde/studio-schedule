<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use DateTimeImmutable;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
use Psr\Clock\ClockInterface;
use Psr\Http\Message\ResponseInterface;
use Slim\Psr7\Factory\ServerRequestFactory;
use StudioSchedule\Bootstrap;
use StudioSchedule\Http\AfterResponse;
use StudioSchedule\Service\PasswordHash;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\Migrator;
use StudioSchedule\Store\Schema;
use StudioSchedule\Store\SessionStore;

final class AuthSessionRoutesTest extends TestCase
{
    private const NOW = '2026-12-04 12:00:00';
    private const BASE = 'https://school.example/dance';
    private const MEMBER = '018f0000-0000-7000-8000-000000000001';
    private const ADMIN = '018f0000-0000-7000-8000-000000000002';
    private const PASSWORD = "fictional Azur 🌙\0clé";
    private const GO_HASH = '$argon2id$v=19$m=19456,t=2,p=1$Zml4ZWQtdGVzdC1zYWx0IQ$8KTamOodG3oPmJE3MOLbJ7a/OFFb2HZ7u7tkLRZq9Hg';

    private Connection $db;
    private string $dsn;
    private string $pgPath;
    private bool $postgres;

    protected function setUp(): void
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($dsn === false) {
            self::markTestSkipped('Requires the disposable database from just php-test');
        }
        $this->dsn = $dsn;
        $this->db = Database::connect($dsn);
        $this->postgres = $this->db->getDatabasePlatform() instanceof PostgreSQLPlatform;
        self::assertSame([], (new Schema($this->db))->relations(), 'Requires an empty disposable database');
        $this->pgPath = getenv('STUDIO_PHP_TEST_POSTGRES_MIGRATIONS') ?: dirname(__DIR__, 2) . '/internal/store/postgres/migrations';
        (new Migrator($dsn))->up($this->postgres ? $this->pgPath : dirname(__DIR__) . '/migrations/mysql');
    }

    protected function tearDown(): void
    {
        if (!isset($this->db)) {
            return;
        }
        if (!$this->postgres) {
            $this->db->executeStatement('SET FOREIGN_KEY_CHECKS = 0');
        }
        foreach ($this->db->createSchemaManager()->listTableNames() as $table) {
            $this->db->executeStatement('DROP TABLE IF EXISTS ' . $this->db->quoteSingleIdentifier($table) . ($this->postgres ? ' CASCADE' : ''));
        }
        if ($this->postgres) {
            $this->db->executeStatement('DROP FUNCTION IF EXISTS receipts_immutable()');
        }
        $this->db->close();
    }

    public function testGoCredentialSignsInReplacesOldSessionAndSignsOut(): void
    {
        $this->account(self::MEMBER, 'Member@Example.test', self::NOW, 'member');
        $this->account(self::ADMIN, 'admin@example.test', self::NOW, 'admin');
        $incoming = 'old-impersonation-cookie';
        $this->session($incoming, self::ADMIN);
        $signedIn = $this->login(' MEMBER@example.test ', cookie: $incoming);
        self::assertSame(200, $signedIn->getStatusCode(), (string) $signedIn->getBody());
        self::assertSame([
            'accountId' => self::MEMBER, 'personId' => self::MEMBER, 'email' => 'Member@Example.test',
            'role' => 'member', 'givenName' => 'Ari', 'familyName' => 'Fable', 'locale' => 'de', 'teaches' => false,
        ], json_decode((string) $signedIn->getBody(), true, flags: JSON_THROW_ON_ERROR));

        $cookie = self::lastCookie($signedIn);
        self::assertMatchesRegularExpression(
            '/^studio_session=[A-Za-z0-9_-]{43}; Path=\/dance; Expires=Fri, 18 Dec 2026 12:00:00 GMT; HttpOnly; Secure; SameSite=Lax$/D',
            $cookie,
        );
        $token = substr(explode(';', $cookie)[0], strlen('studio_session='));
        $hex = $this->postgres ? "encode(token_hash, 'hex')" : 'LOWER(HEX(token_hash))';
        self::assertSame([hash('sha256', $token)], $this->db->fetchFirstColumn("SELECT $hex FROM sessions"));
        self::assertSame(['impersonation-ended'], $this->db->fetchFirstColumn('SELECT action FROM audit_events'));

        $profile = $this->call('GET', '/auth/me', token: $token);
        self::assertSame(200, $profile->getStatusCode());
        self::assertSame((string) $signedIn->getBody(), (string) $profile->getBody());
        self::assertSame(401, $this->call('GET', '/auth/me', token: $incoming)->getStatusCode());

        $loggedOut = $this->call('POST', '/auth/logout', token: $token);
        self::assertSame(204, $loggedOut->getStatusCode());
        self::assertSame('', (string) $loggedOut->getBody());
        self::assertSame('studio_session=; Path=/dance; Expires=Thu, 01 Jan 1970 00:00:00 GMT; Max-Age=0; HttpOnly; Secure; SameSite=Lax', self::lastCookie($loggedOut));
        self::assertSame([], $this->db->fetchFirstColumn('SELECT id FROM sessions'));
        self::assertSame(204, $this->call('POST', '/auth/logout', token: $token)->getStatusCode());
        self::assertSame(204, $this->call('POST', '/auth/logout')->getStatusCode());
        self::assertSame(401, $this->call('GET', '/auth/me', token: $token)->getStatusCode());
    }

    public function testEmailAndAddressLimitsAndCookiePrecedence(): void
    {
        $this->account(self::MEMBER, 'member@example.test', self::NOW, 'member');
        $this->account(self::ADMIN, 'unconfirmed@example.test', null, 'member');
        $invalid = $this->login('unconfirmed@example.test', 'wrong');
        self::assertSame(401, $invalid->getStatusCode());
        $problem = json_decode((string) $invalid->getBody(), true, flags: JSON_THROW_ON_ERROR);
        self::assertIsArray($problem);
        self::assertSame('invalid email or password', $problem['detail'] ?? null);
        self::assertSame(403, $this->login('UNCONFIRMED@example.test')->getStatusCode());

        for ($i = 0; $i < 5; $i++) {
            $spelling = $i % 2 === 0 ? ' Member@example.test ' : 'MEMBER@example.test';
            self::assertSame(401, $this->login($spelling, 'wrong')->getStatusCode());
        }
        $locked = $this->login('member@example.test', peer: '198.51.100.2');
        self::assertSame(429, $locked->getStatusCode());
        self::assertSame('900', $locked->getHeaderLine('Retry-After'));

        $later = $this->login('Member@example.test', cookie: 'unknown-cookie', now: '2026-12-04 12:15:00', base: 'https://school.example');
        self::assertSame(200, $later->getStatusCode(), (string) $later->getBody());
        $cookies = $later->getHeader('Set-Cookie');
        self::assertCount(2, $cookies);
        self::assertStringStartsWith('__Host-studio_session=; Path=/;', $cookies[0]);
        self::assertMatchesRegularExpression('/^__Host-studio_session=[A-Za-z0-9_-]{43}; Path=\/;/', $cookies[1]);

        for ($i = 0; $i < 20; $i++) {
            self::assertSame(401, $this->login("unknown-$i@example.test", peer: '198.51.100.50')->getStatusCode());
        }
        $blocked = $this->login('member@example.test', peer: '198.51.100.50', headers: ['X-Forwarded-For' => '192.0.2.3']);
        self::assertSame(429, $blocked->getStatusCode());
        self::assertSame('900', $blocked->getHeaderLine('Retry-After'));
        self::assertSame(200, $this->login('member@example.test', peer: '198.51.100.51')->getStatusCode());
    }

    public function testRejectsMalformedLoginInputsAndRehashesAnOlderCredential(): void
    {
        $this->account(self::MEMBER, 'member@example.test', self::NOW, 'member');
        $old = password_hash(self::PASSWORD, 'argon2id', ['memory_cost' => 16384, 'time_cost' => 2, 'threads' => 1]);
        $this->db->executeStatement('UPDATE account_credentials SET password_hash = ?', [$old]);
        $changedAt = $this->db->fetchOne('SELECT changed_at FROM account_credentials');
        $today = $this->login('member@example.test');
        self::assertSame(200, $today->getStatusCode(), (string) $today->getBody());
        $new = $this->db->fetchOne('SELECT password_hash FROM account_credentials');
        self::assertIsString($new);
        self::assertNotSame($old, $new);
        self::assertStringContainsString('m=19456,t=2,p=1', $new);
        self::assertTrue((new PasswordHash())->verify($new, self::PASSWORD));
        self::assertSame($changedAt, $this->db->fetchOne('SELECT changed_at FROM account_credentials'));

        self::assertSame(400, $this->call('POST', '/auth/login')->getStatusCode());
        self::assertSame(400, $this->call('POST', '/auth/login', '{')->getStatusCode());
        self::assertSame(415, $this->call('POST', '/auth/login', '{}', headers: ['Content-Type' => 'text/plain'])->getStatusCode());
        self::assertSame(422, $this->call('POST', '/auth/login', '{"email":"a","other":true}')->getStatusCode());
        self::assertSame(422, $this->login('member@example.test', str_repeat('🌙', 257))->getStatusCode());
        self::assertSame(422, $this->call('POST', '/auth/login', '{"email":false,"password":""}')->getStatusCode());
        self::assertSame(413, $this->call('POST', '/auth/login', str_repeat('X', 1024 * 1024 + 1))->getStatusCode());
        self::assertSame(403, $this->call('POST', '/auth/logout', headers: ['Sec-Fetch-Site' => 'cross-site'])->getStatusCode());

        for ($failure = 0; $failure < 4; $failure++) {
            self::assertSame(401, $this->login('member@example.test', 'wrong')->getStatusCode());
        }
        self::assertSame(200, $this->login('member@example.test')->getStatusCode());
        self::assertSame(401, $this->login('member@example.test', 'wrong')->getStatusCode());
        self::assertSame(200, $this->login('member@example.test')->getStatusCode(), 'A successful login resets the email failure counter');

        $this->db->executeStatement('UPDATE accounts SET disabled_at = ?', [self::NOW]);
        self::assertSame(401, $this->login('member@example.test')->getStatusCode());
    }

    public function testAConcurrentPasswordChangeCannotRestoreTheOldCredentialOrOpenASession(): void
    {
        $email = 'member@example.test';
        $this->account(self::MEMBER, $email, self::NOW, 'member');
        $sessions = new SessionStore($this->db);
        $observed = $sessions->credentialsForEmail($email);
        self::assertNotNull($observed);
        self::assertTrue((new PasswordHash())->verify($observed['hash'], self::PASSWORD));

        $replacement = (new PasswordHash())->hash('a different fictional password');
        $this->db->executeStatement('UPDATE account_credentials SET password_hash = ?', [$replacement]);

        $signedIn = $sessions->createForCredentials(
            self::MEMBER,
            $email,
            $observed['hash'],
            (new PasswordHash())->hash(self::PASSWORD),
            hash('sha256', 'fictional stale sign-in token'),
            new DateTimeImmutable(self::NOW . ' UTC'),
            new DateTimeImmutable('2026-12-18 12:00:00 UTC'),
        );
        self::assertNull($signedIn);
        self::assertSame($replacement, $this->db->fetchOne('SELECT password_hash FROM account_credentials'));
        self::assertSame([], $this->db->fetchFirstColumn('SELECT id FROM sessions'));
    }

    public function testPendingRegistrationRotatesTheLinkAndConfirmationReplacesTheSession(): void
    {
        $interval = $this->postgres ? "'0 seconds'" : '0';
        $expiry = $this->postgres ? "'12 hours'" : '43200000000';
        $this->db->executeStatement(
            'INSERT INTO settings (school_name, timezone, default_locale, currency, receipt_prefix, release_weekday, release_time, '
            . 'release_weeks_ahead, default_visible_days, booking_cutoff, cancellation_cutoff, waitlist_invite_expiry) '
            . "VALUES ('Example Studio', 'UTC', 'de', 'EUR', 'R', 0, '18:00:00', 2, 14, $interval, $interval, $expiry)",
        );
        $this->account(self::ADMIN, 'admin@example.test', self::NOW, 'admin');
        $this->account(self::MEMBER, 'member@example.test', self::NOW, 'member');
        $invited = '018f0000-0000-7000-8000-000000000003';
        $this->account($invited, 'invited@example.test', null, 'member');
        $this->db->executeStatement('DELETE FROM account_credentials WHERE account_id = ?', [$this->id($invited)]);
        $email = 'New@Example.test';
        $password = 'fictional renewal passphrase';
        $registration = ['givenName' => 'Ari', 'familyName' => 'Fable', 'email' => $email, 'password' => self::PASSWORD, 'locale' => null];
        $after = new AfterResponse();
        $wakes = 0;
        $finish = static function () use (&$wakes): bool {
            ++$wakes;
            return false;
        };
        $register = fn(array $fields): ResponseInterface => $this->call('POST', '/auth/register', json_encode($fields, JSON_THROW_ON_ERROR), afterResponse: $after);
        foreach (['admin@example.test', 'invited@example.test'] as $existing) {
            $response = $register(array_replace($registration, ['email' => $existing]));
            self::assertSame(202, $response->getStatusCode());
            self::assertSame('', (string) $response->getBody());
        }
        self::assertSame([], $this->db->fetchFirstColumn('SELECT recipient FROM outbox_mails'));
        $after->run($finish);
        self::assertSame(0, $wakes);

        self::assertSame(202, $register($registration)->getStatusCode());
        $after->run($finish);
        self::assertSame(1, $wakes);
        $first = $this->confirmationToken();
        $id = $this->db->fetchOne('SELECT id FROM accounts WHERE email = ?', [$email]);
        self::assertIsString($id);
        self::assertSame(403, $this->login($email)->getStatusCode());

        $registration['email'] = ' ' . strtolower($email) . ' ';
        $registration['givenName'] = 'Mara';
        $registration['password'] = $password;
        $registration['locale'] = 'en-GB';
        self::assertSame(202, $register($registration)->getStatusCode());
        $after->run($finish);
        self::assertSame(2, $wakes);
        $second = $this->confirmationToken($first);
        self::assertNotSame($first, $second);
        self::assertSame([$id], $this->db->fetchFirstColumn('SELECT id FROM accounts WHERE email = ?', [$email]));

        $incoming = 'old-impersonation-cookie';
        $this->session($incoming, self::ADMIN);
        $confirm = fn(string $link, string $phrase): ResponseInterface => $this->call(
            'POST',
            '/auth/confirm',
            json_encode(['token' => $link, 'password' => $phrase], JSON_THROW_ON_ERROR),
            token: $incoming,
        );
        self::assertSame(410, $confirm($first, $password)->getStatusCode());
        self::assertSame(401, $confirm($second, self::PASSWORD)->getStatusCode());
        self::assertSame(200, $this->call('GET', '/auth/me', token: $incoming)->getStatusCode());

        $response = $confirm($second, $password);
        self::assertSame(200, $response->getStatusCode(), (string) $response->getBody());
        $profile = json_decode((string) $response->getBody(), true, flags: JSON_THROW_ON_ERROR);
        self::assertIsArray($profile);
        self::assertSame($email, $profile['email'] ?? null);
        self::assertSame('Mara', $profile['givenName'] ?? null);
        self::assertSame('en', $profile['locale'] ?? null);
        self::assertStringStartsWith('studio_session=', self::lastCookie($response));
        self::assertStringContainsString('; Path=/dance;', self::lastCookie($response));
        self::assertSame(410, $confirm($second, $password)->getStatusCode());
        self::assertSame(401, $this->call('GET', '/auth/me', token: $incoming)->getStatusCode());
        self::assertSame(200, $this->login(strtolower($email), $password)->getStatusCode());
    }

    public function testRegistrationValidatesInputsAndSharesThePasswordRecoveryMailLimit(): void
    {
        $this->account(self::ADMIN, 'admin@example.test', self::NOW, 'admin');
        $base = ['givenName' => 'Ari', 'familyName' => 'Fable', 'email' => 'admin@example.test', 'password' => self::PASSWORD];
        $register = fn(array $fields): ResponseInterface => $this->call('POST', '/auth/register', json_encode($fields, JSON_THROW_ON_ERROR));
        foreach ([
            ['email' => 'not-an-email'],
            ['email' => "\nadmin@example.test"],
            ['email' => "\u{00a0}admin@example.test"],
            ['givenName' => []],
            ['givenName' => ' '],
            ['extra' => true],
            ['password' => 'password'],
        ] as $invalid) {
            $response = $register(array_replace($base, $invalid));
            self::assertSame(422, $response->getStatusCode(), (string) $response->getBody());
            self::assertStringNotContainsString(self::PASSWORD, (string) $response->getBody());
        }
        $missingName = $base;
        unset($missingName['familyName']);
        self::assertSame(422, $register($missingName)->getStatusCode());
        self::assertSame(422, $this->call('POST', '/auth/confirm', '{"token":null,"password":"fictional"}')->getStatusCode());
        self::assertSame(503, $this->call('POST', '/auth/register', json_encode($base, JSON_THROW_ON_ERROR), base: '')->getStatusCode());

        $reset = $this->call('POST', '/auth/password-reset', '{"email":"missing@example.test"}');
        self::assertSame(202, $reset->getStatusCode());
        for ($attempt = 0; $attempt < 2; $attempt++) {
            self::assertSame(202, $register($base)->getStatusCode());
        }
        $blocked = $this->call('POST', '/auth/password-reset', '{"email":"missing@example.test"}');
        self::assertSame(429, $blocked->getStatusCode());
        self::assertSame('3600', $blocked->getHeaderLine('Retry-After'));
        self::assertSame(429, $register($base)->getStatusCode());
    }

    public function testImpersonationChangesTheEffectiveRoleAndRotatesBothCookies(): void
    {
        $inactive = '018f0000-0000-7000-8000-000000000003';
        $missing = '018f0000-0000-7000-8000-000000000004';
        $this->account(self::ADMIN, 'admin@example.test', self::NOW, 'admin');
        $this->account(self::MEMBER, 'member@example.test', self::NOW, 'member');
        $this->account($inactive, 'inactive@example.test', null, 'member');

        $cookieToken = static fn(ResponseInterface $response): string => substr(explode(';', self::lastCookie($response))[0], strlen('studio_session='));
        $start = fn(string $target, string $token = ''): ResponseInterface => $this->call('POST', '/admin/accounts/' . $target . '/impersonate', token: $token);
        $stop = fn(string $token): ResponseInterface => $this->call('POST', '/auth/impersonation/stop', token: $token);
        $member = $cookieToken($this->login('member@example.test'));
        $admin = $cookieToken($this->login('admin@example.test'));
        self::assertSame(401, $start('invalid')->getStatusCode());
        self::assertSame(403, $start(self::MEMBER, $member)->getStatusCode());
        self::assertSame(409, $stop($admin)->getStatusCode());
        self::assertSame(422, $start('invalid', $admin)->getStatusCode());
        self::assertSame(403, $start(self::ADMIN, $admin)->getStatusCode());
        self::assertSame(404, $start($missing, $admin)->getStatusCode());
        self::assertSame(409, $start($inactive, $admin)->getStatusCode());

        $assumed = $start(self::MEMBER, $admin);
        self::assertSame(200, $assumed->getStatusCode(), (string) $assumed->getBody());
        self::assertStringContainsString('Path=/dance; Expires=Fri, 04 Dec 2026 13:00:00 GMT', self::lastCookie($assumed));
        $assumedToken = $cookieToken($assumed);
        self::assertNotSame($admin, $assumedToken);
        self::assertSame(401, $this->call('GET', '/auth/me', token: $admin)->getStatusCode());
        self::assertSame(200, $this->call('GET', '/auth/me', token: $member)->getStatusCode());
        self::assertSame(403, $this->call('GET', '/admin/accounts', token: $assumedToken)->getStatusCode());
        $credentialChange = $this->call('POST', '/auth/password', '{"currentPassword":"wrong fixture password","newPassword":"different fixture password"}', token: $assumedToken);
        self::assertSame(403, $credentialChange->getStatusCode());
        self::assertStringContainsString('not while signed in as another account', (string) $credentialChange->getBody());
        $emailChange = $this->call('POST', '/auth/email', '{"newEmail":"different@example.test","password":"wrong fixture password"}', token: $assumedToken);
        self::assertSame(403, $emailChange->getStatusCode());

        $restored = $stop($assumedToken);
        self::assertSame(200, $restored->getStatusCode(), (string) $restored->getBody());
        $restoredToken = $cookieToken($restored);
        self::assertNotSame($admin, $restoredToken);
        self::assertNotSame($assumedToken, $restoredToken);
        self::assertSame(200, $this->call('GET', '/admin/accounts', token: $restoredToken)->getStatusCode());
        self::assertSame(401, $stop($assumedToken)->getStatusCode());
        self::assertSame(['impersonation-ended', 'impersonation-started'], $this->db->fetchFirstColumn('SELECT action FROM audit_events ORDER BY action'));
    }

    public function testCredentialChangesShareTheLoginAndAuthMailLimits(): void
    {
        $this->account(self::ADMIN, 'admin@example.test', self::NOW, 'admin');
        $this->account(self::MEMBER, 'member@example.test', self::NOW, 'member');
        $signedIn = $this->login('admin@example.test');
        self::assertSame(200, $signedIn->getStatusCode());
        $token = substr(explode(';', self::lastCookie($signedIn))[0], strlen('studio_session='));
        for ($attempt = 0; $attempt < 2; $attempt++) {
            $locale = $this->call('PUT', '/auth/locale', '{"locale":"en"}', token: $token);
            self::assertSame(204, $locale->getStatusCode());
        }
        self::assertSame(422, $this->call('PUT', '/auth/locale', '{"locale":"fr"}', token: $token)->getStatusCode());
        self::assertSame(204, $this->call('PUT', '/auth/locale', '{}', token: $token)->getStatusCode());
        $email = ['newEmail' => 'member@example.test', 'password' => 'wrong fixture password'];
        $wrong = $this->call('POST', '/auth/email', json_encode($email, JSON_THROW_ON_ERROR), token: $token);
        self::assertSame(422, $wrong->getStatusCode());
        self::assertStringContainsString('body.password', (string) $wrong->getBody());
        $email['password'] = self::PASSWORD;
        $validEmailChange = json_encode($email, JSON_THROW_ON_ERROR);
        self::assertSame(202, $this->call('POST', '/auth/email', $validEmailChange, token: $token)->getStatusCode());

        for ($attempt = 0; $attempt < 4; $attempt++) {
            $reset = $this->call('POST', '/auth/password-reset', '{"email":"missing@example.test"}');
            self::assertSame(202, $reset->getStatusCode());
        }
        $mailBlocked = $this->call('POST', '/auth/email', $validEmailChange, token: $token);
        self::assertSame(429, $mailBlocked->getStatusCode());
        self::assertSame('3600', $mailBlocked->getHeaderLine('Retry-After'));

        $password = ['currentPassword' => 'wrong fixture password', 'newPassword' => 'different fixture password'];
        for ($attempt = 0; $attempt < 3; $attempt++) {
            $wrong = $this->call('POST', '/auth/password', json_encode($password, JSON_THROW_ON_ERROR), token: $token);
            self::assertSame(422, $wrong->getStatusCode());
            self::assertStringContainsString('body.currentPassword', (string) $wrong->getBody());
        }
        self::assertSame(401, $this->login('admin@example.test', 'wrong fixture password')->getStatusCode());
        $password['currentPassword'] = self::PASSWORD;
        $passwordBlocked = $this->call('POST', '/auth/password', json_encode($password, JSON_THROW_ON_ERROR), token: $token);
        self::assertSame(429, $passwordBlocked->getStatusCode());
        self::assertSame('900', $passwordBlocked->getHeaderLine('Retry-After'));
        $emailBlocked = $this->call('POST', '/auth/email', $validEmailChange, token: $token, peer: '198.51.100.2');
        self::assertSame(429, $emailBlocked->getStatusCode());
        self::assertSame('900', $emailBlocked->getHeaderLine('Retry-After'));
    }

    public function testAccountLifecycleRevokesSessionsAndRollsBackAProtectedSelfDeletion(): void
    {
        $this->account(self::ADMIN, 'admin@example.test', self::NOW, 'admin');
        $this->account(self::MEMBER, 'member@example.test', self::NOW, 'member');
        $duration = $this->postgres ? "'0 seconds'" : '0';
        $this->db->executeStatement(
            'INSERT INTO settings (school_name, timezone, default_locale, currency, receipt_prefix, release_weekday, release_time, '
            . 'release_weeks_ahead, default_visible_days, booking_cutoff, cancellation_cutoff) '
            . "VALUES ('Fixture Studio', 'UTC', 'en', 'EUR', 'R', 0, '18:00:00', 1, 2, $duration, $duration)",
        );

        $memberId = $this->id(self::MEMBER);
        $decode = $this->postgres ? "decode(?, 'hex')" : 'UNHEX(?)';
        $this->db->executeStatement(
            'INSERT INTO push_subscriptions (id, account_id, endpoint, p256dh, auth, created_at, last_used_at) '
            . "VALUES (?, ?, 'https://push.example.test/fixture', $decode, $decode, ?, ?)",
            [$memberId, $memberId, str_repeat('01', 65), str_repeat('02', 16), self::NOW, self::NOW],
        );
        $token = static function (ResponseInterface $reply): string {
            self::assertSame(200, $reply->getStatusCode(), (string) $reply->getBody());

            return substr(explode(';', self::lastCookie($reply))[0], strlen('studio_session='));
        };
        $admin = $token($this->login('admin@example.test'));
        $member = $token($this->login('member@example.test'));
        $path = '/admin/accounts/' . self::MEMBER;
        $password = json_encode(['password' => self::PASSWORD, 'locale' => 'en'], JSON_THROW_ON_ERROR);

        $own = $this->call('POST', '/admin/accounts/' . self::ADMIN . '/enable', token: $admin);
        self::assertSame(409, $own->getStatusCode());
        self::assertStringContainsString('own-account', (string) $own->getBody());
        $last = $this->call('DELETE', '/my/account', $password, token: $admin);
        self::assertSame(409, $last->getStatusCode());
        self::assertStringContainsString('last-admin', (string) $last->getBody());
        self::assertSame(401, $this->call('POST', $path . '/disable')->getStatusCode());
        self::assertSame(403, $this->call('POST', $path . '/disable', token: $member)->getStatusCode());
        self::assertSame(422, $this->call('DELETE', '/admin/accounts/bad-id', token: $admin)->getStatusCode());
        self::assertSame(404, $this->call('DELETE', '/admin/accounts/018f0000-0000-7000-8000-000000000099', token: $admin)->getStatusCode());
        self::assertSame(400, $this->call('PUT', $path . '/role', '{', token: $admin)->getStatusCode());
        self::assertSame(422, $this->call('PUT', $path . '/role', '{"role":"guest"}', token: $admin)->getStatusCode());

        for ($attempt = 0; $attempt < 2; ++$attempt) {
            self::assertSame(200, $this->call('PUT', $path . '/role', '{"role":"office"}', token: $admin)->getStatusCode());
            self::assertSame(200, $this->call('POST', $path . '/disable', token: $admin)->getStatusCode());
        }
        self::assertContains($this->db->fetchOne('SELECT COUNT(*) FROM push_subscriptions'), [0, '0']);
        self::assertSame(401, $this->call('GET', '/auth/me', token: $member)->getStatusCode());
        self::assertSame(200, $this->call('POST', $path . '/enable', token: $admin)->getStatusCode());
        $member = $token($this->login('member@example.test'));
        self::assertSame('office', $this->db->fetchOne('SELECT role FROM accounts WHERE id = ?', [$memberId]));
        self::assertSame(['account-disabled', 'account-enabled', 'account-role-changed'], $this->db->fetchFirstColumn('SELECT action FROM audit_events ORDER BY action'));

        $wrong = $this->call('DELETE', '/my/account', '{"password":"wrong fixture password"}', token: $member);
        self::assertSame(422, $wrong->getStatusCode());
        self::assertStringContainsString('body.password', (string) $wrong->getBody());

        $this->db->executeStatement(
            "INSERT INTO audit_events (id, occurred_at, actor_id, action) VALUES (?, ?, ?, 'mcp-tool-called')",
            [$memberId, self::NOW, $memberId],
        );
        $blocked = $this->call('DELETE', '/my/account', $password, token: $member);
        self::assertSame(409, $blocked->getStatusCode());
        self::assertStringContainsString('in-use', (string) $blocked->getBody());
        self::assertSame(200, $this->call('GET', '/auth/me', token: $member)->getStatusCode());
        self::assertContains($this->db->fetchOne('SELECT COUNT(*) FROM audit_events WHERE target_id = ?', [$memberId]), [3, '3']);
        self::assertContains($this->db->fetchOne('SELECT COUNT(*) FROM outbox_mails'), [0, '0']);

        $this->db->executeStatement('DELETE FROM audit_events WHERE id = ?', [$memberId]);
        $deleted = $this->call('DELETE', '/my/account', $password, token: $member);
        self::assertSame(204, $deleted->getStatusCode(), (string) $deleted->getBody());
        self::assertStringContainsString('Path=/dance;', self::lastCookie($deleted));
        self::assertStringContainsString('Max-Age=0', self::lastCookie($deleted));
        self::assertContains($this->db->fetchOne('SELECT COUNT(*) FROM accounts WHERE id = ?', [$memberId]), [0, '0']);
        self::assertContains($this->db->fetchOne('SELECT COUNT(*) FROM persons WHERE id = ?', [$memberId]), [1, '1']);
        self::assertContains($this->db->fetchOne('SELECT COUNT(*) FROM audit_events WHERE target_person_id = ?', [$memberId]), [4, '4']);
        self::assertSame('Dein Konto bei Fixture Studio wurde gelöscht', $this->db->fetchOne("SELECT subject FROM outbox_mails WHERE template = 'account-deleted'"));
    }

    public function testOverlappingAdminChangesPreserveTheLastConfirmedAdmin(): void
    {
        $this->account(self::ADMIN, 'admin@example.test', self::NOW, 'admin');
        $this->account(self::MEMBER, 'second@example.test', self::NOW, 'admin');
        $this->account('018f0000-0000-7000-8000-000000000003', 'invited@example.test', null, 'admin');

        $worker = <<<'PHP'
            require 'vendor/autoload.php';
            $db = StudioSchedule\Store\Database::connect(getenv('STUDIO_PHP_TEST_DATABASE_URL'));
            $accounts = new StudioSchedule\Store\AdminAccountsStore($db);
            echo $accounts->write(function () use ($db, $accounts, $argv) {
                // Establish a snapshot before either transaction changes an admin.
                $db->fetchOne("SELECT COUNT(*) FROM accounts WHERE role = 'admin'");
                echo "ready\n";
                if (fgets(STDIN) !== "go\n") {
                    throw new RuntimeException('Unexpected synchronization signal');
                }
                $target = $accounts->lock($argv[1]);
                if ($target['lastAdmin']) {
                    return 'last-admin';
                }
                $accounts->setDisabled($target['id'], new DateTimeImmutable('2026-12-04T12:00:00Z'));
                return 'disabled';
            });
            PHP;
        $workers = [];
        try {
            foreach ([self::ADMIN, self::MEMBER] as $target) {
                $process = proc_open(
                    [PHP_BINARY, '-r', $worker, '--', $target],
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
                $results[] = stream_get_contents($pipes[1]);
                self::assertSame('', stream_get_contents($pipes[2]));
            }
            sort($results);
            self::assertSame(['disabled', 'last-admin'], $results);
            self::assertContains($this->db->fetchOne("SELECT COUNT(*) FROM accounts WHERE role = 'admin' AND confirmed_at IS NOT NULL AND disabled_at IS NULL"), [1, '1']);
        } finally {
            foreach ($workers as [$process, $pipes]) {
                foreach ($pipes as $pipe) {
                    fclose($pipe);
                }
                proc_close($process);
            }
        }
    }

    private function confirmationToken(string $excluding = ''): string
    {
        foreach ($this->db->fetchFirstColumn("SELECT body_text FROM outbox_mails WHERE template = 'confirm-email'") as $body) {
            self::assertIsString($body);
            if (preg_match('~' . preg_quote(self::BASE, '~') . '/confirm\?token=([A-Za-z0-9_-]{43})~', $body, $match) !== 1) {
                self::fail('Confirmation email did not contain a link under the configured base URL');
            }
            $token = $match[1];
            if ($token !== $excluding) {
                return $token;
            }
        }
        self::fail('No new confirmation email for the registered account');
    }

    /** @param array<string,string> $headers */
    private function login(
        string $email,
        string $password = self::PASSWORD,
        string $cookie = '',
        string $peer = '198.51.100.1',
        string $now = self::NOW,
        string $base = self::BASE,
        array $headers = [],
    ): ResponseInterface {
        return $this->call('POST', '/auth/login', json_encode(['email' => $email, 'password' => $password], JSON_THROW_ON_ERROR), $cookie, $peer, $now, $base, $headers);
    }

    /** @param array<string,string> $headers */
    private function call(
        string $method,
        string $path,
        string $body = '',
        string $token = '',
        string $peer = '198.51.100.1',
        string $now = self::NOW,
        string $base = self::BASE,
        array $headers = [],
        ?AfterResponse $afterResponse = null,
    ): ResponseInterface {
        $request = (new ServerRequestFactory())->createServerRequest($method, $base . '/api/v1' . $path, ['REMOTE_ADDR' => $peer]);
        $request = $request->withHeader('Content-Type', 'application/json');
        foreach ($headers as $name => $value) {
            $request = $request->withHeader($name, $value);
        }
        if ($token !== '') {
            $request = $request->withCookieParams([($base === 'https://school.example' ? '__Host-studio_session' : 'studio_session') => $token]);
        }
        $request->getBody()->write($body);
        $request->getBody()->rewind();
        $env = ['BASE_URL' => $base, 'DATABASE_URL' => $this->dsn, 'LOG_LEVEL' => 'error'];
        $clock = new class ($now) implements ClockInterface {
            public function __construct(private readonly string $now) {}

            public function now(): DateTimeImmutable
            {
                return new DateTimeImmutable($this->now . ' UTC');
            }
        };

        return Bootstrap::handle(
            static fn() => $request,
            dirname(__DIR__),
            '/missing',
            $this->pgPath,
            static fn(string $key): ?string => $env[$key] ?? null,
            commonPasswordFile: getenv('STUDIO_PHP_TEST_COMMON_PASSWORDS') ?: null,
            clock: $clock,
            afterResponse: $afterResponse,
        );
    }

    private function account(string $id, string $email, ?string $confirmed, string $role): void
    {
        $encoded = $this->id($id);
        $this->db->executeStatement("INSERT INTO persons (id, given_name, family_name) VALUES (?, 'Ari', 'Fable')", [$encoded]);
        $this->db->executeStatement(
            "INSERT INTO accounts (id, person_id, email, role, confirmed_at, locale) VALUES (?, ?, ?, ?, ?, 'de')",
            [$encoded, $encoded, $email, $role, $confirmed],
        );
        $this->db->executeStatement(
            'INSERT INTO account_credentials (account_id, password_hash, changed_at) VALUES (?, ?, ?)',
            [$encoded, self::GO_HASH, '2026-12-01 12:00:00'],
        );
    }

    private function session(string $token, string $admin): void
    {
        $digest = $this->postgres ? "decode(?, 'hex')" : 'UNHEX(?)';
        $this->db->executeStatement(
            'INSERT INTO sessions (id, account_id, token_hash, created_at, last_seen_at, expires_at, impersonator_id) '
            . "VALUES (?, ?, $digest, ?, ?, ?, ?)",
            [$this->id(self::MEMBER), $this->id(self::MEMBER), hash('sha256', $token), self::NOW, self::NOW, '2026-12-04 12:30:00', $this->id($admin)],
        );
    }

    private function id(string $uuid): string
    {
        return $this->postgres ? $uuid : pack('H*', str_replace('-', '', $uuid));
    }

    private static function lastCookie(ResponseInterface $response): string
    {
        $cookies = $response->getHeader('Set-Cookie');
        $cookie = end($cookies);
        self::assertIsString($cookie);

        return $cookie;
    }
}
