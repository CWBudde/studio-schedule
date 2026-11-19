<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use DateTimeImmutable;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
use Psr\Clock\ClockInterface;
use Psr\Http\Message\ResponseInterface;
use Psr\Log\NullLogger;
use RuntimeException;
use Slim\Psr7\Factory\ServerRequestFactory;
use Slim\Psr7\Factory\StreamFactory;
use StudioSchedule\Bootstrap;
use StudioSchedule\Console\JobsCommand;
use StudioSchedule\Service\BackgroundJobs;
use StudioSchedule\Service\JobResult;
use StudioSchedule\Service\JobRunner;
use StudioSchedule\Service\PasswordHash;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\JobLocks;
use StudioSchedule\Store\JobRunStore;
use StudioSchedule\Store\MetricsStore;
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

            $this->assertMetrics($app, $dsn, $migrations);

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

    private function assertMetrics(string $app, #[\SensitiveParameter] string $dsn, string $migrations): void
    {
        $path = '/dance/metrics';
        $token = 'fictional-monitoring-token';
        self::assertSame(404, $this->request($app, $dsn, 'GET', $path)->getStatusCode());
        $denied = $this->request($app, $dsn, 'GET', $path, query: ['token' => $token], metricsToken: $token);
        self::assertSame(401, $denied->getStatusCode());
        self::assertSame('Bearer', $denied->getHeaderLine('WWW-Authenticate'));
        self::assertStringNotContainsString($token, (string) $denied->getBody());

        $authorization = 'Bearer ' . $token;
        self::assertSame(401, $this->request($app, $dsn, 'GET', $path, metricsToken: $token, authorization: 'Bearer wrong')->getStatusCode());
        self::assertSame(404, $this->request($app, $dsn, 'GET', '/metrics', metricsToken: $token, authorization: $authorization)->getStatusCode());
        for ($i = 0; $i < 2; ++$i) {
            self::assertSame(200, $this->request($app, $dsn, 'GET', '/dance/api/v1/health', metricsToken: $token)->getStatusCode());
        }
        self::assertSame(404, $this->request($app, $dsn, 'GET', '/dance/ical/fictional-private-feed.ics', metricsToken: $token)->getStatusCode());
        self::assertSame(404, $this->request($app, $dsn, 'GET', '/dance/not-a-route-fictional', metricsToken: $token)->getStatusCode());

        $id = '018f0000-0000-7000-8000-000000000001';
        foreach (['disable', 'enable'] as $action) {
            self::assertSame(401, $this->request($app, $dsn, 'POST', "/dance/api/v1/admin/accounts/$id/$action", metricsToken: $token)->getStatusCode());
        }
        self::assertSame(401, $this->request($app, $dsn, 'PUT', "/dance/api/v1/locations/$id/room-order", metricsToken: $token)->getStatusCode());

        $output = fopen('php://memory', 'w+');
        self::assertIsResource($output);
        try {
            $jobs = new JobsCommand($migrations, $migrations);
            $environment = static fn(string $key): string => $key === 'METRICS_TOKEN' ? $token : '';
            self::assertSame([0, 0], [
                $jobs->run($dsn, $environment, $output, $output),
                $jobs->run($dsn, $environment, $output, $output),
            ]);
        } finally {
            fclose($output);
        }

        $database = Database::connect($dsn);
        try {
            $logger = new NullLogger();
            $runner = new JobRunner(new JobLocks($dsn), new JobRunStore($database), $this->clockAt(new DateTimeImmutable('2040-01-01T00:00:00Z')), $logger, new MetricsStore($dsn, $logger));
            self::assertSame(JobResult::Failed, $runner->runNow(BackgroundJobs::DELIVER_MAIL, static function (): void {
                throw new RuntimeException('fictional background failure details');
            }));
        } finally {
            $database->close();
        }

        $scrape = $this->request($app, $dsn, 'GET', $path, metricsToken: $token, authorization: $authorization);
        self::assertSame(200, $scrape->getStatusCode());
        self::assertSame('text/plain; version=0.0.4; charset=utf-8', $scrape->getHeaderLine('Content-Type'));
        self::assertSame('no-store', $scrape->getHeaderLine('Cache-Control'));
        $body = (string) $scrape->getBody();
        self::assertStringContainsString('studio_schedule_http_requests_total{method="GET",route="/api/v1/health",status="200"} 2', $body);
        self::assertStringContainsString('studio_schedule_http_request_duration_seconds_bucket{method="GET",route="/api/v1/health",le="+Inf"} 2', $body);
        self::assertStringContainsString('studio_schedule_http_requests_total{method="GET",route="/ical/{token}.ics",status="404"} 1', $body);
        self::assertStringContainsString('studio_schedule_http_requests_total{method="GET",route="other",status="404"}', $body);
        self::assertStringContainsString('route="/api/v1/admin/accounts/{accountId}/disable",status="401"} 1', $body);
        self::assertStringContainsString('route="/api/v1/admin/accounts/{accountId}/enable",status="401"} 1', $body);
        self::assertStringContainsString('route="/api/v1/locations/{locationId}/room-order",status="401"} 1', $body);
        self::assertStringNotContainsString($id, $body);
        self::assertStringContainsString('studio_schedule_job_runs_total{job="purge-sessions",result="success"} 1', $body);
        self::assertStringContainsString('studio_schedule_job_runs_total{job="deliver-mail",result="failure"} 1', $body);
        self::assertStringNotContainsString('studio_schedule_job_last_success_timestamp_seconds{job="deliver-mail"}', $body);
        self::assertStringNotContainsString('fictional background failure details', $body);
        self::assertStringContainsString('studio_schedule_job_duration_seconds_count{job="purge-sessions"} 1', $body);
        self::assertStringContainsString('studio_schedule_job_last_success_timestamp_seconds{job="purge-sessions"} ', $body);
        self::assertStringContainsString('studio_schedule_outbox_mails{status="pending"} 0', $body);
        self::assertStringContainsString('studio_schedule_outbox_mails{status="failed"} 0', $body);
        self::assertStringContainsString('studio_schedule_build_info{version="', $body);
        self::assertStringNotContainsString('route="/metrics"', $body);
        self::assertStringNotContainsString('fictional-private-feed', $body);
        self::assertStringNotContainsString('not-a-route-fictional', $body);
        self::assertStringNotContainsString($token, $body);

        $head = $this->request($app, $dsn, 'HEAD', $path, metricsToken: $token, authorization: $authorization);
        self::assertSame(200, $head->getStatusCode());
        self::assertSame('', (string) $head->getBody());
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
        string $metricsToken = '',
        string $authorization = '',
    ): ResponseInterface {
        $request = (new ServerRequestFactory())->createServerRequest($method, 'https://school.example.test' . $path)
            ->withParsedBody($body)
            ->withQueryParams($query);
        if ($authorization !== '') {
            $request = $request->withHeader('Authorization', $authorization);
        }
        if ($json !== []) {
            $request = $request->withHeader('Content-Type', 'application/json')
                ->withParsedBody(null)
                ->withBody((new StreamFactory())->createStream(json_encode($json, JSON_THROW_ON_ERROR)));
        }
        $environment = [
            'SETUP_TOKEN' => self::ACCESS_TOKEN,
            'DATABASE_URL' => $dsn,
            'METRICS_TOKEN' => $metricsToken,
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
