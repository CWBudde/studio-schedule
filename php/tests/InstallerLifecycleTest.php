<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use DateTimeImmutable;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
use Psr\Clock\ClockInterface;
use Psr\Http\Message\ResponseInterface;
use Slim\Psr7\Factory\ServerRequestFactory;
use Slim\Psr7\Factory\StreamFactory;
use StudioSchedule\Bootstrap;
use StudioSchedule\Service\PasswordHash;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\Schema;

final class InstallerLifecycleTest extends TestCase
{
    private const ACCESS_TOKEN = 'fictional-installer-token';
    private const PASSWORD = 'Linden street nears the evening tram';

    public function testMountedInstallerMigratesSetsAUsableAdminAndAuthorizesUpgrades(): void
    {
        $dsn = getenv('STUDIO_PHP_TEST_INSTALLER_URL');
        if ($dsn === false) {
            self::markTestSkipped('Requires the fresh installer database supplied by just php-test');
        }

        $database = Database::connect($dsn);
        $schema = new Schema($database);
        self::assertSame([], $schema->relations(), 'The install test owns an empty fixture database');
        $app = sys_get_temp_dir() . '/studio-installer-' . bin2hex(random_bytes(6));
        $postgres = $database->getDatabasePlatform() instanceof PostgreSQLPlatform;
        $migrations = $postgres ? $app . '/postgres' : $app . '/migrations/mysql';
        self::assertTrue(mkdir($migrations, 0700, true));
        $sources = $postgres
            ? (getenv('STUDIO_PHP_TEST_POSTGRES_MIGRATIONS') ?: __DIR__ . '/../../internal/store/postgres/migrations')
            : __DIR__ . '/../migrations/mysql';
        $files = Schema::migrationFiles($sources);
        self::assertNotEmpty($files);
        foreach ($files as $file) {
            self::assertTrue(copy($file, $migrations . '/' . basename($file)));
        }

        try {
            self::assertSame(200, $this->request($app, $dsn, 'GET', '/dance/healthz')->getStatusCode());
            self::assertSame(503, $this->request($app, $dsn, 'GET', '/dance/readyz')->getStatusCode());

            $page = $this->request($app, $dsn, 'GET', '/dance/setup', query: ['token' => self::ACCESS_TOKEN]);
            self::assertSame(200, $page->getStatusCode(), (string) $page->getBody());
            self::assertStringContainsString('action="/dance/setup"', (string) $page->getBody());
            self::assertStringContainsString('Set up school', (string) $page->getBody());

            $fields = [
                'action' => 'install',
                'token' => self::ACCESS_TOKEN,
                'schoolName' => 'Fictional Dance School',
                'timezone' => 'Europe/Berlin',
                'locale' => 'de',
                'currency' => 'EUR',
                'email' => 'first-admin@example.invalid',
                'givenName' => 'Ada',
                'familyName' => 'Muster',
            ];
            $invalid = $this->request($app, $dsn, 'POST', '/dance/setup', array_replace($fields, ['timezone' => 'invalid']));
            self::assertSame(422, $invalid->getStatusCode());
            self::assertSame([], $schema->relations());

            $install = $this->request($app, $dsn, 'POST', '/dance/setup', $fields);
            self::assertSame(201, $install->getStatusCode(), (string) $install->getBody());
            self::assertSame(array_keys($files), $schema->appliedVersions());
            self::assertSame('Fictional Dance School', $database->fetchOne('SELECT school_name FROM settings'));
            self::assertSame('admin', $database->fetchOne('SELECT role FROM accounts'));
            self::assertNull($database->fetchOne('SELECT confirmed_at FROM accounts'));
            self::assertMatchesRegularExpression(
                '#https://school\.example\.test/dance/reset-password\?token=[A-Za-z0-9_-]{43}#',
                (string) $install->getBody(),
            );
            self::assertSame(1, preg_match('/reset-password\?token=([A-Za-z0-9_-]{43})/', (string) $install->getBody(), $link));
            self::assertArrayHasKey(1, $link);
            $token = $link[1] ?? throw new \LogicException('Expected the installer reset token');

            $repeat = $this->request($app, $dsn, 'POST', '/dance/setup', $fields);
            self::assertSame(409, $repeat->getStatusCode());
            self::assertContains($database->fetchOne('SELECT COUNT(*) FROM accounts'), [1, '1']);

            $reset = $this->request($app, $dsn, 'POST', '/dance/api/v1/auth/password-reset/confirm', json: [
                'token' => $token,
                'password' => self::PASSWORD,
            ]);
            self::assertSame(204, $reset->getStatusCode(), (string) $reset->getBody());
            $used = $this->request($app, $dsn, 'POST', '/dance/api/v1/auth/password-reset/confirm', json: [
                'token' => $token,
                'password' => self::PASSWORD,
            ]);
            self::assertSame(410, $used->getStatusCode(), (string) $used->getBody());
            $hash = $database->fetchOne('SELECT password_hash FROM account_credentials');
            self::assertIsString($hash);
            self::assertTrue((new PasswordHash())->verify($hash, self::PASSWORD));

            $copyPage = $this->request($app, $dsn, 'GET', '/dance/setup', query: ['token' => self::ACCESS_TOKEN]);
            self::assertSame(200, $copyPage->getStatusCode());
            self::assertStringContainsString('name="targetDatabaseUrl"', (string) $copyPage->getBody());
            $copy = ['action' => 'copy', 'token' => self::ACCESS_TOKEN, 'email' => 'FIRST-ADMIN@example.invalid'];
            $refused = $this->request($app, $dsn, 'POST', '/dance/setup', $copy + [
                'password' => 'wrong password', 'targetDatabaseUrl' => 'invalid://fixture:copy-secret@example.invalid',
            ]);
            self::assertSame(403, $refused->getStatusCode());
            self::assertStringNotContainsString('copy-secret', (string) $refused->getBody());
            $missing = $this->request($app, $dsn, 'POST', '/dance/setup', $copy + ['password' => self::PASSWORD]);
            self::assertSame(422, $missing->getStatusCode());
            $invalidTarget = $this->request($app, $dsn, 'POST', '/dance/setup', $copy + [
                'password' => self::PASSWORD, 'targetDatabaseUrl' => 'invalid://fixture:copy-secret@example.invalid',
            ]);
            self::assertSame(422, $invalidTarget->getStatusCode());
            self::assertStringNotContainsString('copy-secret', (string) $invalidTarget->getBody());

            file_put_contents($migrations . '/99000_installer_probe.sql', "-- +goose Up\nCREATE TABLE installer_probe (id INTEGER PRIMARY KEY);\n");
            $pending = $this->request($app, $dsn, 'GET', '/dance/setup', query: ['token' => self::ACCESS_TOKEN]);
            self::assertSame(200, $pending->getStatusCode(), (string) $pending->getBody());
            self::assertStringContainsString('99000_installer_probe.sql', (string) $pending->getBody());
            self::assertStringNotContainsString('name="targetDatabaseUrl"', (string) $pending->getBody());
            $pendingCopy = $this->request($app, $dsn, 'POST', '/dance/setup', $copy + ['password' => self::PASSWORD]);
            self::assertSame(409, $pendingCopy->getStatusCode());
            $upgrade = ['action' => 'upgrade', 'token' => self::ACCESS_TOKEN, 'email' => 'FIRST-ADMIN@example.invalid'];
            $bad = $this->request($app, $dsn, 'POST', '/dance/setup', $upgrade + ['password' => 'wrong password']);
            self::assertSame(403, $bad->getStatusCode(), (string) $bad->getBody());
            self::assertSame(array_keys($files), $schema->appliedVersions());

            if (!$postgres) {
                self::assertStringContainsString('name="backupVerified"', (string) $pending->getBody());
                $unverified = $this->request($app, $dsn, 'POST', '/dance/setup', $upgrade + ['password' => self::PASSWORD]);
                self::assertSame(422, $unverified->getStatusCode(), (string) $unverified->getBody());
                self::assertSame(array_keys($files), $schema->appliedVersions());
            }
            $upgraded = $this->request($app, $dsn, 'POST', '/dance/setup', $upgrade + [
                'password' => self::PASSWORD,
                'backupVerified' => 'yes',
            ]);
            self::assertSame(200, $upgraded->getStatusCode(), (string) $upgraded->getBody());
            self::assertContains(99000, $schema->appliedVersions());
            self::assertContains('installer_probe', $schema->relations());
            $ready = $this->request($app, $dsn, 'GET', '/dance/readyz');
            self::assertSame(200, $ready->getStatusCode(), (string) $ready->getBody());
            $start = new DateTimeImmutable('2040-01-01T00:00:00Z');
            $probe = $this->request($app, $dsn, 'GET', '/dance/readyz', clock: $this->clockAt($start));
            self::assertSame(200, $probe->getStatusCode());
            self::assertSame('no-store', $probe->getHeaderLine('Cache-Control'));

            $usesApcu = function_exists('apcu_enabled') && apcu_enabled();
            $versionHash = $database->fetchOne('SELECT version_hash FROM http_readiness_cache');
            self::assertIsString($versionHash);
            self::assertSame($usesApcu ? 0 : 64, strlen($versionHash));

            $database->executeStatement('INSERT INTO goose_db_version (version_id, is_applied) VALUES (99002, TRUE)');
            try {
                $cached = $this->request($app, $dsn, 'HEAD', '/dance/readyz', clock: $this->clockAt($start->modify('+1999999 microseconds')));
                self::assertSame(200, $cached->getStatusCode());
                self::assertSame('', (string) $cached->getBody());

                $maintenance = $this->request($app, $dsn, 'GET', '/dance/api/v1/health');
                self::assertSame(503, $maintenance->getStatusCode());
                self::assertStringContainsString('urn:studio-schedule:maintenance', (string) $maintenance->getBody());

                $elapsed = $this->request($app, $dsn, 'GET', '/dance/readyz', clock: $this->clockAt($start->modify('+2 seconds')));
                self::assertSame(503, $elapsed->getStatusCode());
                self::assertSame('no-store', $elapsed->getHeaderLine('Cache-Control'));
            } finally {
                $database->executeStatement('DELETE FROM goose_db_version WHERE version_id = 99002');
            }
            $cachedFailure = $this->request($app, $dsn, 'HEAD', '/dance/readyz', clock: $this->clockAt($start->modify('+3 seconds')));
            self::assertSame(503, $cachedFailure->getStatusCode());
            self::assertSame('', (string) $cachedFailure->getBody());
            self::assertSame(200, $this->request($app, $dsn, 'GET', '/dance/readyz', clock: $this->clockAt($start->modify('+4 seconds')))->getStatusCode());

            $futureMigration = $migrations . '/99003_future_probe.sql';
            touch($futureMigration);
            try {
                $newRelease = $this->request($app, $dsn, 'GET', '/dance/readyz', clock: $this->clockAt($start->modify('+4 seconds')));
                self::assertSame(503, $newRelease->getStatusCode());
            } finally {
                unlink($futureMigration);
            }

            if (!$postgres) {
                file_put_contents(
                    $migrations . '/99001_interrupted_probe.sql',
                    "-- +goose Up\nCREATE TABLE interrupted_probe (id INTEGER PRIMARY KEY);\nINSERT INTO missing_fixture_table VALUES (1);\n",
                );
                $failed = $this->request($app, $dsn, 'POST', '/dance/setup', $upgrade + [
                    'password' => self::PASSWORD,
                    'backupVerified' => 'yes',
                ]);
                self::assertSame(503, $failed->getStatusCode(), (string) $failed->getBody());
                self::assertStringContainsString('verified pre-upgrade backup', (string) $failed->getBody());
                self::assertSame(503, $this->request($app, $dsn, 'GET', '/dance/readyz')->getStatusCode());
                $recovery = $this->request($app, $dsn, 'GET', '/dance/setup', query: ['token' => self::ACCESS_TOKEN]);
                self::assertSame(503, $recovery->getStatusCode());
                self::assertStringContainsString('verified pre-upgrade backup', (string) $recovery->getBody());
            }
        } finally {
            $database->close();
            foreach (glob($migrations . '/*.sql') ?: [] as $file) {
                unlink($file);
            }
            rmdir($migrations);
            if (!$postgres) {
                rmdir($app . '/migrations');
            }
            rmdir($app);
        }
    }

    private function clockAt(DateTimeImmutable $instant): ClockInterface
    {
        $clock = $this->createStub(ClockInterface::class);
        $clock->method('now')->willReturn($instant);

        return $clock;
    }

    /**
     * @param array<string,string> $body
     * @param array<string,string> $query
     * @param array<string,string> $json
     */
    private function request(
        string $app,
        #[\SensitiveParameter]
        string $dsn,
        string $method,
        string $path,
        array $body = [],
        array $query = [],
        array $json = [],
        ?ClockInterface $clock = null,
    ): ResponseInterface {
        $request = (new ServerRequestFactory())->createServerRequest($method, 'https://school.example.test' . $path)
            ->withParsedBody($body)
            ->withQueryParams($query);
        if ($json !== []) {
            $request = $request->withHeader('Content-Type', 'application/json')
                ->withParsedBody(null)
                ->withBody((new StreamFactory())->createStream(json_encode($json, JSON_THROW_ON_ERROR)));
        }
        $environment = [
            'SETUP_TOKEN' => self::ACCESS_TOKEN,
            'DATABASE_URL' => $dsn,
            'BASE_URL' => 'https://school.example.test/dance',
            'LOG_LEVEL' => 'error',
        ];

        return Bootstrap::handle(
            static fn() => $request,
            $app,
            $app . '/public',
            $app . '/postgres',
            static fn(string $key): ?string => $environment[$key] ?? null,
            getenv('STUDIO_PHP_TEST_COMMON_PASSWORDS') ?: __DIR__ . '/../../internal/auth/password/common-passwords.txt',
            $clock,
        );
    }
}
