<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use DateTimeImmutable;
use Doctrine\DBAL\Connection;
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
use StudioSchedule\Http\AfterResponse;
use StudioSchedule\Service\BackgroundJobs;
use StudioSchedule\Service\JobResult;
use StudioSchedule\Service\JobRunner;
use StudioSchedule\Service\PasswordHash;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\JobLocks;
use StudioSchedule\Store\JobRunStore;
use StudioSchedule\Store\MetricsStore;
use StudioSchedule\Store\Schema;
use Symfony\Component\Uid\Uuid;

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
            $this->assertAnnouncements($app, $dsn, $database);

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

    private function assertAnnouncements(string $app, #[\SensitiveParameter] string $dsn, Connection $database): void
    {
        $now = new DateTimeImmutable('2041-01-01T09:00:00Z');
        $clock = $this->clockAt($now);
        $login = $this->request($app, $dsn, 'POST', '/dance/api/v1/auth/login', json: [
            'email' => 'first-admin@example.invalid', 'password' => self::PASSWORD,
        ], clock: $clock);
        self::assertSame(200, $login->getStatusCode(), (string) $login->getBody());
        self::assertSame(1, preg_match('/\bstudio_session=([^;]+)/', $login->getHeaderLine('Set-Cookie'), $cookie));
        self::assertArrayHasKey(1, $cookie);
        $cookies = ['studio_session' => $cookie[1] ?? throw new \LogicException('Expected the login cookie')];
        $path = '/dance/api/v1/admin/announcements';

        $posted = ['audience' => 'role', 'role' => 'admin', 'title' => 'Fixture notice', 'body' => 'Studio closed', 'email' => true];
        $after = new AfterResponse();
        $notice = $this->request($app, $dsn, 'POST', $path, json: $posted, clock: $clock, cookies: $cookies, afterResponse: $after);
        self::assertSame(201, $notice->getStatusCode(), (string) $notice->getBody());
        $mailCount = static fn(): mixed => $database->fetchOne("SELECT COUNT(*) FROM outbox_mails WHERE template = 'announcement'");
        self::assertContains($mailCount(), [0, '0']);
        $after->run(static fn(): bool => true);
        self::assertContains($mailCount(), [1, '1']);
        self::assertSame('first-admin@example.invalid', $database->fetchOne("SELECT recipient FROM outbox_mails WHERE template = 'announcement'"));
        $subject = $database->fetchOne("SELECT subject FROM outbox_mails WHERE template = 'announcement'");
        self::assertIsString($subject);
        self::assertStringContainsString('Fixture notice', $subject);
        $memberPath = '/dance/api/v1/announcements';
        $officeItems = $this->request($app, $dsn, 'GET', $path, clock: $clock, cookies: $cookies);
        self::assertSame(200, $officeItems->getStatusCode());
        self::assertStringContainsString('"emailRecipients":1', (string) $officeItems->getBody());
        $myItems = $this->request($app, $dsn, 'GET', $memberPath, clock: $clock, cookies: $cookies);
        self::assertSame(200, $myItems->getStatusCode());
        self::assertStringContainsString('"dismissed":false', (string) $myItems->getBody());

        $noticeBody = json_decode((string) $notice->getBody(), true, flags: JSON_THROW_ON_ERROR);
        self::assertIsArray($noticeBody);
        $id = $noticeBody['id'] ?? null;
        self::assertIsString($id);
        $dismiss = "$memberPath/$id/dismiss";
        self::assertSame([204, 204], [
            $this->request($app, $dsn, 'POST', $dismiss, clock: $clock, cookies: $cookies)->getStatusCode(),
            $this->request($app, $dsn, 'POST', $dismiss, clock: $clock, cookies: $cookies)->getStatusCode(),
        ]);
        self::assertSame(422, $this->request($app, $dsn, 'POST', "$memberPath/invalid-id/dismiss", clock: $clock, cookies: $cookies)->getStatusCode());
        $dismissed = $this->request($app, $dsn, 'GET', $memberPath, clock: $clock, cookies: $cookies);
        self::assertStringContainsString('"dismissed":true', (string) $dismissed->getBody());

        $preview = $this->request($app, $dsn, 'POST', "$path/recipients", json: ['Audience' => 'role', 'Role' => 'admin'], clock: $clock, cookies: $cookies);
        self::assertSame(200, $preview->getStatusCode());
        self::assertSame('{"recipients":1}', (string) $preview->getBody());
        $unknown = $this->request($app, $dsn, 'POST', "$path/recipients", json: ['audience' => 'everyone', 'typo' => true], clock: $clock, cookies: $cookies);
        self::assertSame(422, $unknown->getStatusCode());

        $edit = $posted;
        $edit['email'] = false;
        $edit['expiresAt'] = $now->modify('+1 hour')->format(DATE_RFC3339);
        $updated = $this->request($app, $dsn, 'PUT', "$path/$id", json: $edit, clock: $clock, cookies: $cookies);
        self::assertSame(200, $updated->getStatusCode());
        self::assertStringContainsString('"email":true', (string) $updated->getBody());
        self::assertSame(422, $this->request($app, $dsn, 'PUT', "$path/$id", json: $edit + ['Draft' => true], clock: $clock, cookies: $cookies)->getStatusCode());
        self::assertSame(409, $this->request($app, $dsn, 'PUT', "$path/$id", json: $edit, clock: $this->clockAt($now->modify('+2 hours')), cookies: $cookies)->getStatusCode());

        $postgres = $database->getDatabasePlatform() instanceof PostgreSQLPlatform;
        $confirmed = Database::sqlInstant($database, $now);
        for ($number = 1; $number <= 102; ++$number) {
            $person = Uuid::v7();
            $account = Uuid::v7();
            $personId = $postgres ? $person->toRfc4122() : $person->toBinary();
            $accountId = $postgres ? $account->toRfc4122() : $account->toBinary();
            $database->executeStatement("INSERT INTO persons (id, given_name, family_name) VALUES (?, 'Mail', 'Fixture')", [$personId]);
            // Go uses simple Unicode lowercase when persisting the mail cursor.
            $address = match ($number) {
                101 => 'zİ@example.invalid',
                102 => 'zi@example.invalid',
                default => sprintf('member%03d@example.invalid', $number),
            };
            $database->executeStatement(
                "INSERT INTO accounts (id, person_id, email, role, locale, confirmed_at) VALUES (?, ?, ?, 'member', 'en', ?)",
                [$accountId, $personId, $address, $confirmed],
            );
        }

        $posted['role'] = 'member';
        $first = $now->modify('+1 day');
        $posted['publishAt'] = $first->format(DATE_RFC3339);
        $future = $this->request($app, $dsn, 'POST', $path, json: $posted, clock: $clock, cookies: $cookies);
        self::assertSame(201, $future->getStatusCode(), (string) $future->getBody());
        $futureBody = json_decode((string) $future->getBody(), true, flags: JSON_THROW_ON_ERROR);
        self::assertIsArray($futureBody);
        self::assertArrayHasKey('id', $futureBody);
        $futureId = $futureBody['id'];
        self::assertIsString($futureId);
        $posted['publishAt'] = $first->modify('+2 hours')->format(DATE_RFC3339);
        foreach (['Second notice', 'Third notice'] as $title) {
            $posted['title'] = $title;
            self::assertSame(201, $this->request($app, $dsn, 'POST', $path, json: $posted, clock: $clock, cookies: $cookies)->getStatusCode());
        }
        self::assertContains($mailCount(), [1, '1']);
        $limited = $this->request($app, $dsn, 'POST', $path, json: $posted, clock: $clock, cookies: $cookies);
        self::assertSame(409, $limited->getStatusCode());
        self::assertStringContainsString('"reason":"mail-limit"', (string) $limited->getBody());

        $jobs = new BackgroundJobs($database, $dsn, $this->clockAt($first), new NullLogger());
        self::assertSame(JobResult::Ok, $jobs->queueAnnouncements());
        self::assertContains($mailCount(), [101, '101']);
        $key = $postgres ? $futureId : Uuid::fromString($futureId)->toBinary();
        self::assertSame('member100@example.invalid', $database->fetchOne('SELECT mail_cursor FROM announcements WHERE id = ?', [$key]));
        self::assertNull($database->fetchOne('SELECT emailed_at FROM announcements WHERE id = ?', [$key]));

        $jobs = new BackgroundJobs($database, $dsn, $this->clockAt($first->modify('+1 minute')), new NullLogger());
        self::assertSame(JobResult::Ok, $jobs->queueAnnouncements());
        self::assertSame(JobResult::Ok, $jobs->queueAnnouncements(immediate: true));
        self::assertContains($mailCount(), [102, '102']);
        self::assertContains($database->fetchOne("SELECT COUNT(DISTINCT recipient) FROM outbox_mails WHERE template = 'announcement'"), [102, '102']);
        self::assertContains($database->fetchOne('SELECT recipients FROM announcement_mailings WHERE announcement_id = ?', [$key]), [101, '101']);
        self::assertNull($database->fetchOne('SELECT mail_cursor FROM announcements WHERE id = ?', [$key]));

        $later = $this->clockAt($first);
        self::assertSame(204, $this->request($app, $dsn, 'DELETE', "$path/$futureId", clock: $later, cookies: $cookies)->getStatusCode());
        self::assertContains($database->fetchOne('SELECT recipients FROM announcement_mailings WHERE announcement_id IS NULL'), [101, '101']);
        self::assertSame(409, $this->request($app, $dsn, 'POST', $path, json: $posted, clock: $later, cookies: $cookies)->getStatusCode());
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
     * @param array<string,string|bool> $json
     * @param array<string,string> $cookies
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
        array $cookies = [],
        ?AfterResponse $afterResponse = null,
    ): ResponseInterface {
        $request = (new ServerRequestFactory())->createServerRequest($method, 'https://school.example.test' . $path)
            ->withParsedBody($body)
            ->withQueryParams($query)
            ->withCookieParams($cookies);
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
            $afterResponse,
        );
    }
}
