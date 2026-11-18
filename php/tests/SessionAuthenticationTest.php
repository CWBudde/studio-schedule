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
use StudioSchedule\Store\Database;
use StudioSchedule\Store\Migrator;
use StudioSchedule\Store\Schema;

final class SessionAuthenticationTest extends TestCase
{
    private const NOW = '2026-12-04 12:00:00';
    private const ADMIN = '018f0000-0000-7000-8000-000000000001';
    private const MEMBER = '018f0000-0000-7000-8000-000000000002';

    public function testSessionRolesImpersonationAndCookieRenewalThroughTheMountedApp(): void
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($dsn === false) {
            self::markTestSkipped('Requires the disposable database from just php-test');
        }
        $db = Database::connect($dsn);
        self::assertSame([], (new Schema($db))->relations(), 'Requires an empty disposable database');
        $postgres = $db->getDatabasePlatform() instanceof PostgreSQLPlatform;
        $pgPath = getenv('STUDIO_PHP_TEST_POSTGRES_MIGRATIONS') ?: dirname(__DIR__, 2) . '/internal/store/postgres/migrations';
        $path = $postgres ? $pgPath : dirname(__DIR__) . '/migrations/mysql';

        try {
            (new Migrator($dsn))->up($path);
            $this->person($db, $postgres, self::ADMIN, 'Ada', 'admin');
            $this->person($db, $postgres, self::MEMBER, 'Cleo', 'member');
            $db->executeStatement(
                "INSERT INTO staff (id, person_id, display_name, kind, active) VALUES (?, ?, 'Ada', 'instructor', TRUE)",
                [self::id(self::ADMIN, $postgres), self::id(self::ADMIN, $postgres)],
            );
            $this->session($db, $postgres, 1, self::ADMIN, 'stale-admin', '2026-11-05 12:00:00', '2026-12-03 12:00:00', '2026-12-15 12:00:00');
            $this->session($db, $postgres, 2, self::MEMBER, 'current-member', '2026-12-03 12:00:00', '2026-12-04 11:00:00', '2026-12-17 12:00:00');
            $this->session($db, $postgres, 3, self::MEMBER, 'acting-as-member', '2026-12-04 11:30:00', '2026-12-04 11:30:00', '2026-12-04 12:30:00', self::ADMIN);
            $this->session($db, $postgres, 4, self::MEMBER, 'expired-idle', '2026-11-20 12:00:00', '2026-11-20 12:00:00', self::NOW);
            $this->session($db, $postgres, 5, self::MEMBER, 'expired-absolute', '2026-11-04 12:00:00', '2026-12-04 11:30:00', '2026-12-07 12:00:00');


            $empty = $this->me($dsn, $pgPath, []);
            self::assertSame(401, $empty->getStatusCode());
            self::assertSame([], $empty->getHeader('Set-Cookie'));

            $member = $this->me($dsn, $pgPath, ['studio_session' => 'current-member']);
            self::assertSame(200, $member->getStatusCode(), (string) $member->getBody());
            self::assertSame([
                'accountId' => self::MEMBER,
                'personId' => self::MEMBER,
                'email' => 'member@example.test',
                'role' => 'member',
                'givenName' => 'Cleo',
                'familyName' => 'Example',
                'teaches' => false,
            ], json_decode((string) $member->getBody(), true, flags: JSON_THROW_ON_ERROR));
            self::assertSame('no-store', $member->getHeaderLine('Cache-Control'));
            self::assertSame([], $member->getHeader('Set-Cookie'));

            $admin = $this->me($dsn, $pgPath, ['studio_session' => 'stale-admin']);
            self::assertSame(200, $admin->getStatusCode());
            $profile = json_decode((string) $admin->getBody(), true, flags: JSON_THROW_ON_ERROR);
            self::assertIsArray($profile);
            self::assertSame('dev', $profile['version'] ?? null);
            self::assertSame('de', $profile['locale'] ?? null);
            self::assertTrue($profile['teaches'] ?? null);
            self::assertSame(
                'studio_session=stale-admin; Path=/dance; Expires=Sat, 05 Dec 2026 12:00:00 GMT; HttpOnly; Secure; SameSite=Lax',
                $admin->getHeaderLine('Set-Cookie'),
            );
            self::assertSame([], $this->me($dsn, $pgPath, ['studio_session' => 'stale-admin'])->getHeader('Set-Cookie'));

            $impersonated = $this->me($dsn, $pgPath, ['studio_session' => 'acting-as-member']);
            self::assertSame(200, $impersonated->getStatusCode());
            $acting = json_decode((string) $impersonated->getBody(), true, flags: JSON_THROW_ON_ERROR);
            self::assertIsArray($acting);
            self::assertSame('member', $acting['role'] ?? null);
            self::assertSame([
                'accountId' => self::ADMIN,
                'email' => 'admin@example.test',
                'givenName' => 'Ada',
                'familyName' => 'Example',
            ], $acting['impersonator'] ?? null);
            self::assertSame([], $impersonated->getHeader('Set-Cookie'));
            $db->executeStatement("UPDATE accounts SET role = 'office' WHERE id = ?", [self::id(self::ADMIN, $postgres)]);

            foreach (['expired-idle', 'expired-absolute', 'acting-as-member', 'unknown'] as $invalidToken) {
                $response = $this->me($dsn, $pgPath, ['studio_session' => $invalidToken]);
                self::assertSame(401, $response->getStatusCode());
                self::assertStringStartsWith('studio_session=; Path=/dance;', $response->getHeaderLine('Set-Cookie'));
            }

            $legacy = $this->me($dsn, $pgPath, ['studio_session' => 'current-member'], 'https://school.example');
            self::assertSame(401, $legacy->getStatusCode());
            self::assertStringStartsWith('studio_session=; Path=/;', $legacy->getHeaderLine('Set-Cookie'));
            $secure = $this->me($dsn, $pgPath, ['__Host-studio_session' => 'current-member'], 'https://school.example');
            self::assertSame(200, $secure->getStatusCode());

            $db->executeStatement('UPDATE accounts SET disabled_at = ? WHERE id = ?', [self::NOW, self::id(self::MEMBER, $postgres)]);
            self::assertSame(401, $this->me($dsn, $pgPath, ['studio_session' => 'current-member'])->getStatusCode());
        } finally {
            if (!$postgres) {
                $db->executeStatement('SET FOREIGN_KEY_CHECKS = 0');
            }
            foreach ($db->createSchemaManager()->listTableNames() as $table) {
                $db->executeStatement('DROP TABLE IF EXISTS ' . $db->quoteSingleIdentifier($table) . ($postgres ? ' CASCADE' : ''));
            }
            if ($postgres) {
                $db->executeStatement('DROP FUNCTION IF EXISTS receipts_immutable()');
            }
            $db->close();
        }
    }

    /** @param array<string,string> $cookies */
    private function me(string $dsn, string $pgPath, array $cookies, string $base = 'https://school.example/dance'): ResponseInterface
    {
        $make = static fn() => (new ServerRequestFactory())->createServerRequest('GET', $base . '/api/v1/auth/me')
            ->withCookieParams($cookies);
        $env = ['BASE_URL' => $base, 'DATABASE_URL' => $dsn, 'LOG_LEVEL' => 'error'];
        $clock = new class implements ClockInterface {
            public function now(): DateTimeImmutable
            {
                return new DateTimeImmutable('2026-12-04 12:00:00 UTC');
            }
        };

        return Bootstrap::handle($make, dirname(__DIR__), '/missing', $pgPath, static fn(string $key): ?string => $env[$key] ?? null, clock: $clock);
    }

    private function person(Connection $db, bool $postgres, string $id, string $name, string $role): void
    {
        $encoded = self::id($id, $postgres);
        $db->executeStatement("INSERT INTO persons (id, given_name, family_name) VALUES (?, ?, 'Example')", [$encoded, $name]);
        $db->executeStatement(
            'INSERT INTO accounts (id, person_id, email, role, confirmed_at, locale) VALUES (?, ?, ?, ?, ?, ?)',
            [$encoded, $encoded, $role . '@example.test', $role, self::NOW, $role === 'admin' ? 'de' : null],
        );
    }

    private function session(
        Connection $db,
        bool $postgres,
        int $id,
        string $account,
        string $token,
        string $created,
        string $lastSeen,
        string $expires,
        ?string $impersonator = null,
    ): void {
        $hash = $postgres ? "decode(?, 'hex')" : "UNHEX(?)";
        $db->executeStatement(
            'INSERT INTO sessions (id, account_id, token_hash, created_at, last_seen_at, expires_at, impersonator_id) '
            . "VALUES (?, ?, $hash, ?, ?, ?, ?)",
            [
                self::id(sprintf('018f0000-0000-7000-9000-%012d', $id), $postgres),
                self::id($account, $postgres),
                hash('sha256', $token),
                $created,
                $lastSeen,
                $expires,
                $impersonator === null ? null : self::id($impersonator, $postgres),
            ],
        );
    }

    private static function id(string $value, bool $postgres): string
    {
        return $postgres ? $value : pack('H*', str_replace('-', '', $value));
    }
}
