<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
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

            file_put_contents($migrations . '/99000_installer_probe.sql', "-- +goose Up\nCREATE TABLE installer_probe (id INTEGER PRIMARY KEY);\n");
            $pending = $this->request($app, $dsn, 'GET', '/dance/setup', query: ['token' => self::ACCESS_TOKEN]);
            self::assertSame(200, $pending->getStatusCode(), (string) $pending->getBody());
            self::assertStringContainsString('99000_installer_probe.sql', (string) $pending->getBody());
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
        );
    }
}
