<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
use StudioSchedule\Console\Operator;
use StudioSchedule\Push\VapidKeys;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\Migrator;
use StudioSchedule\Store\Schema;

final class OperatorTest extends TestCase
{
    public function testEntrypointShowsTheTwoOperatorCommands(): void
    {
        $pipes = [];
        $process = proc_open([PHP_BINARY, dirname(__DIR__) . '/bin/studio-schedule', '--help'], [1 => ['pipe', 'w'], 2 => ['pipe', 'w']], $pipes);
        self::assertIsResource($process);
        $out = stream_get_contents($pipes[1]);
        $error = stream_get_contents($pipes[2]);
        fclose($pipes[1]);
        fclose($pipes[2]);

        self::assertSame(0, proc_close($process), (string) $error);
        self::assertStringContainsString('init', (string) $out);
        self::assertStringContainsString('healthcheck', (string) $out);
        self::assertStringContainsString('db:copy', (string) $out);
        self::assertStringContainsString('jobs:run', (string) $out);
        self::assertStringContainsString('doctor', (string) $out);
        self::assertStringContainsString('vapid-keys', (string) $out);
        self::assertStringContainsString('import', (string) $out);
        self::assertSame('', $error);
    }

    public function testHealthcheckUsesTheMountedPathAndFlagPrecedence(): void
    {
        [$code, $out, $error] = $this->healthcheck('200 OK', 'head-only');
        self::assertSame(0, $code, $error);
        self::assertSame('', $out);
        self::assertSame('', $error);
    }

    public function testHealthcheckDoesNotFollowRedirects(): void
    {
        [$code, $out, $error] = $this->healthcheck('302 Found');
        self::assertSame(1, $code);
        self::assertSame('', $out);
        self::assertStringContainsString('/dance/schedule/healthz answered 302', $error);
    }

    public function testHealthcheckTimesOutWhenTheServerDoesNotSendHeaders(): void
    {
        [$code, $out, $error] = $this->healthcheck('200 OK', 'no-headers');
        self::assertSame(1, $code);
        self::assertSame('', $out);
        self::assertStringContainsString('health check:', $error);
    }

    public function testHealthcheckReportsUnavailableAndConnectionFailures(): void
    {
        self::assertStringContainsString('answered 503', $this->healthcheck('503 Service Unavailable')[2]);
        $result = self::execute(['healthcheck'], ['HTTP_ADDR' => '127.0.0.1:1']);
        self::assertSame(1, $result[0]);
        self::assertStringContainsString('health check:', $result[2]);
    }

    public function testConfigurationAndArgumentErrorsUseTheUsageExitCode(): void
    {
        foreach ([
            [['unknown'], [], 'unknown command'],
            [['init', 'extra'], [], 'unexpected argument'],
            [['init', '--something'], [], 'unknown flag'],
            [['init', '--admin-email'], [], '--admin-email needs a value'],
            [['init'], [], 'no database URL'],
            [['jobs:run'], [], 'no database URL'],
            [['doctor', '--as=private@example.test'], [], 'acts as no account'],
            [['doctor', '--output=xml'], [], 'output format must be text or json'],
            [['vapid-keys', 'extra'], [], 'unexpected argument'],
            [['db:copy'], [], '--target-database-url'],
            [['db:copy', '--target-database-url=mysql://fixture:secret@db/target'], [], 'no source database URL'],
            [['init', '--database-url='], ['DATABASE_URL' => 'postgresql://fixture:secret@db/studio'], 'no database URL'],
            [['healthcheck'], ['HTTP_ADDR' => 'bad'], 'HTTP_ADDR'],
            [['healthcheck'], ['HTTP_ADDR' => '0'], 'HTTP_ADDR'],
            [['healthcheck', '--addr=127.0.0.1:65536'], [], '--addr'],
            [['healthcheck', '--addr=127.0.0.1/wrong:8080'], [], '--addr'],
            [['healthcheck', '--addr=[example.com]:8080'], [], '--addr'],
            [['healthcheck'], ['BASE_URL' => 'https://db:secret@school.example'], 'BASE_URL'],
        ] as [$args, $env, $contains]) {
            [$code, $out, $error] = self::execute($args, $env);
            self::assertSame(2, $code, $error);
            self::assertSame('', $out);
            self::assertStringContainsString($contains, $error);
            self::assertStringNotContainsString('secret', $error);
        }
    }

    public function testImportReportsUsageErrorsInTheRequestedFormat(): void
    {
        foreach ([
            [[], 'accepts 1 arg(s), received 0'],
            [['members.csv', 'extra.csv'], 'accepts 1 arg(s), received 2'],
            [['members.csv', '--unknown'], 'unknown flag'],
            [['members.csv', '--apply=invalid'], '--apply accepts true or false'],
            [['members.csv'], 'no database URL: set DATABASE_URL or --database-url'],
        ] as [$arguments, $detail]) {
            [$code, $out, $error] = self::execute(['--output=json', 'import', ...$arguments]);
            self::assertSame(2, $code);
            self::assertSame('', $out);
            self::assertSame(
                ['title' => 'Bad Request', 'status' => 400, 'detail' => $detail],
                json_decode($error, true, flags: JSON_THROW_ON_ERROR),
            );
        }
    }

    public function testVapidKeysWritesOneMatchingPairInEnvironmentFileFormat(): void
    {
        [$code, $output, $error] = self::execute(['vapid-keys'], ['DATABASE_URL' => 'unsupported://fixture:secret@localhost/studio']);
        self::assertSame(0, $code, $error);
        self::assertSame('', $error);
        if (preg_match('/\AVAPID_PUBLIC_KEY=([A-Za-z0-9_-]{87})\nVAPID_PRIVATE_KEY=([A-Za-z0-9_-]{43})\n\z/', $output, $match) !== 1) {
            self::fail('Expected two VAPID environment assignments');
        }
        $pair = VapidKeys::parse($match[1], $match[2]);
        self::assertSame($match[1], $pair->publicKey());
        self::assertSame($match[2], $pair->privateKey());
    }

    public function testDoctorReportsOrderedChecksWithOpaqueConfigAndConnectionErrors(): void
    {
        [$report, $code] = self::doctor(['LOG_FORMAT' => 'fixture-secret']);
        self::assertSame(1, $code);
        self::assertSame([
            'config' => 'fail', 'database' => 'skip', 'schema' => 'skip', 'settings' => 'skip',
            'smtp' => 'skip', 'vapid' => 'skip', 'base-url' => 'skip', 'receipts' => 'ok',
        ], array_column($report, 'result', 'name'));
        self::assertStringContainsString('LOG_FORMAT', $report[0]['detail']);
        self::assertStringNotContainsString('fixture-secret', json_encode($report, JSON_THROW_ON_ERROR));

        [$report, $code] = self::doctor([
            'DATABASE_URL' => 'postgresql://fixture:fixture-secret@127.0.0.1:1/studio?connect_timeout=1',
            'BASE_URL' => 'http://public.example.test/dance',
        ], ['--base-url=http://[::1]:8080/dance']);
        self::assertSame(1, $code);
        self::assertSame([
            'config' => 'ok', 'database' => 'fail', 'schema' => 'skip', 'settings' => 'skip',
            'smtp' => 'skip', 'vapid' => 'skip', 'base-url' => 'warn', 'receipts' => 'ok',
        ], array_column($report, 'result', 'name'));
        self::assertStringNotContainsString('fixture-secret', json_encode($report, JSON_THROW_ON_ERROR));
    }

    public function testDoctorUsesTheEffectiveBaseUrlAndValidatesThePushPairWithoutDisclosingIt(): void
    {
        $keys = VapidKeys::generate();
        $environment = [
            'VAPID_PUBLIC_KEY' => $keys->publicKey(), 'VAPID_PRIVATE_KEY' => $keys->privateKey(),
            'VAPID_SUBJECT' => 'mailto:admin@example.test', 'BASE_URL' => 'http://public.example.test',
        ];
        [$report, $code] = self::doctor($environment);
        self::assertSame(1, $code);
        self::assertStringContainsString('no database', $report[1]['detail']);
        self::assertSame('ok', $report[5]['result']);
        self::assertSame('fail', $report[6]['result']);
        self::assertStringNotContainsString($keys->privateKey(), json_encode($report, JSON_THROW_ON_ERROR));

        [$report] = self::doctor($environment, ['--base-url=', '--addr=:9876']);
        self::assertSame('warn', $report[6]['result']);
        self::assertStringContainsString('http://localhost:9876/', $report[6]['detail']);

        [$report] = self::doctor(['VAPID_PRIVATE_KEY' => 'fixture-secret'] + $environment);
        self::assertSame('fail', $report[0]['result']);
        self::assertStringContainsString('matching pair', $report[0]['detail']);
        self::assertStringNotContainsString('fixture-secret', json_encode($report, JSON_THROW_ON_ERROR));

        [$report] = self::doctor(['VAPID_SUBJECT' => ''] + $environment);
        self::assertSame('fail', $report[0]['result']);
        self::assertStringContainsString('VAPID_SUBJECT', $report[0]['detail']);
    }

    public function testInitWritesAOneTimeSetupLinkOnAllSupportedDatabases(): void
    {
        $dsn = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($dsn === false) {
            self::markTestSkipped('Requires the disposable database provisioned by just php-test');
        }
        $database = Database::connect($dsn);
        $schema = new Schema($database);
        self::assertSame([], $schema->relations(), 'CLI setup test requires an empty disposable database');
        $postgres = $database->getDatabasePlatform() instanceof PostgreSQLPlatform;
        $postgresMigrations = getenv('STUDIO_PHP_TEST_POSTGRES_MIGRATIONS') ?: dirname(__DIR__, 2) . '/internal/store/postgres/migrations';
        $migrations = $postgres ? $postgresMigrations : dirname(__DIR__) . '/migrations/mysql';
        $env = ['DATABASE_URL' => $dsn, 'BASE_URL' => 'https://wrong.example', 'LOG_LEVEL' => 'invalid'];

        try {
            $doctorEnv = ['DATABASE_URL' => $dsn, 'BASE_URL' => 'https://schedule.example.org/dance'];
            [$report, $doctorCode] = self::doctor($doctorEnv, postgresMigrations: $postgresMigrations);
            self::assertSame(1, $doctorCode);
            self::assertSame('ok', $report[1]['result']);
            self::assertSame('fail', $report[2]['result']);
            self::assertSame('skip', $report[3]['result']);
            $maintenance = self::execute(['jobs:run'], ['SMTP_HOST' => '', 'DATABASE_URL' => $dsn], postgresMigrations: $postgresMigrations);
            self::assertSame(1, $maintenance[0]);
            self::assertStringContainsString('database schema is not up to date', $maintenance[2]);
            $result = self::execute(['init'], $env, postgresMigrations: $postgresMigrations);
            self::assertSame(1, $result[0]);
            self::assertStringContainsString('database schema is not up to date', $result[2]);
            self::assertSame([], $schema->relations());

            (new Migrator($dsn))->up($migrations);
            [$report, $doctorCode] = self::doctor($doctorEnv, postgresMigrations: $postgresMigrations);
            self::assertSame(1, $doctorCode);
            self::assertSame('ok', $report[2]['result']);
            self::assertSame('fail', $report[3]['result']);
            self::assertStringContainsString('run init', $report[3]['detail']);
            $result = self::execute(['init'], $env, stdin: "Fictional Dance Hall\n", postgresMigrations: $postgresMigrations);
            self::assertSame(1, $result[0]);
            self::assertStringContainsString('missing --timezone', $result[2]);

            $result = self::execute([
                'init', '--school-name', 'Fictional Dance Hall', '--timezone=Nowhere/Town', '--locale=en', '--currency=EUR',
                '--admin-email=ada@example.test', '--admin-given-name=Ada', '--admin-family-name=Example',
            ], $env, postgresMigrations: $postgresMigrations);
            self::assertSame(1, $result[0]);
            self::assertStringContainsString('invalid --timezone', $result[2]);

            $result = self::execute([
                '--database-url', $dsn, 'init', '--base-url=https://school.example/dance/schedule/',
                '--admin-email', 'ada@example.test',
            ], ['DATABASE_URL' => 'unsupported://user:secret@other.invalid'] + $env, "\n", $postgresMigrations);
            self::assertSame(1, $result[0]);
            self::assertStringContainsString('missing --school-name', $result[2]);
            self::assertContains($database->fetchOne('SELECT COUNT(*) FROM settings'), [0, '0']);

            $args = [
                '--database-url', $dsn, 'init', '--base-url=https://school.example/dance/schedule/',
                '--admin-email', 'ada@example.test',
            ];
            $input = "Fictional Dance Hall\nEurope/Berlin\n\n\nAda\nExample";
            [$code, $out, $error] = self::execute($args, ['DATABASE_URL' => 'unsupported://user:secret@other.invalid'] + $env, $input, $postgresMigrations);
            self::assertSame(0, $code, $error);
            self::assertStringContainsString('School name: Time zone', $error);
            self::assertStringContainsString('[en]', $error);
            self::assertStringContainsString('[EUR]', $error);
            self::assertStringContainsString('The school "Fictional Dance Hall" is set up, with ada@example.test as its admin.', $out);
            self::assertSame(1, preg_match('~https://school\.example/dance/schedule/reset-password\?token=([A-Za-z0-9_-]{43})~', $out, $match));
            $token = $match[1] ?? '';
            self::assertNotSame('', $token);
            self::assertStringNotContainsString($token, $error);
            self::assertSame('Fictional Dance Hall', $database->fetchOne('SELECT school_name FROM settings'));
            self::assertSame('en', $database->fetchOne('SELECT default_locale FROM settings'));
            self::assertSame('EUR', $database->fetchOne('SELECT currency FROM settings'));
            self::assertSame('admin', $database->fetchOne('SELECT role FROM accounts'));
            self::assertSame('ada@example.test', $database->fetchOne('SELECT email FROM accounts'));
            [$report, $doctorCode] = self::doctor($doctorEnv, postgresMigrations: $postgresMigrations);
            self::assertSame(0, $doctorCode);
            self::assertSame([
                'config' => 'ok', 'database' => 'ok', 'schema' => 'ok', 'settings' => 'ok',
                'smtp' => 'skip', 'vapid' => 'skip', 'base-url' => 'ok', 'receipts' => 'ok',
            ], array_column($report, 'result', 'name'));
            self::assertSame('Europe/Berlin, EUR', $report[3]['detail']);
            self::assertSame('https://schedule.example.org/dance', $report[6]['detail']);
            [$doctorCode, $doctorText, $doctorError] = self::execute(['doctor'], $doctorEnv, postgresMigrations: $postgresMigrations);
            self::assertSame(0, $doctorCode);
            self::assertSame('', $doctorError);
            self::assertStringStartsWith("config    ok    valid\ndatabase  ok    connected\n", $doctorText);

            $database->executeStatement("UPDATE settings SET timezone = 'Mars/Olympus_Mons'");
            [$report, $doctorCode] = self::doctor($doctorEnv, postgresMigrations: $postgresMigrations);
            self::assertSame(1, $doctorCode);
            self::assertSame('fail', $report[3]['result']);
            self::assertStringContainsString('unknown timezone', $report[3]['detail']);
            $database->executeStatement("UPDATE settings SET timezone = 'Europe/Berlin'");

            $database->insert('goose_db_version', ['version_id' => 99999, 'is_applied' => true]);
            [$report, $doctorCode] = self::doctor($doctorEnv, postgresMigrations: $postgresMigrations);
            self::assertSame(1, $doctorCode);
            self::assertStringContainsString('schema is newer', $report[2]['detail']);
            $database->delete('goose_db_version', ['version_id' => 99999]);
            if (!$postgres) {
                $database->insert('goose_db_version', ['version_id' => Schema::UPGRADE_IN_PROGRESS, 'is_applied' => true]);
                [$report, $doctorCode] = self::doctor($doctorEnv, postgresMigrations: $postgresMigrations);
                self::assertSame(1, $doctorCode);
                self::assertStringContainsString('verified pre-upgrade backup', $report[2]['detail']);
                self::assertSame('skip', $report[3]['result']);
                $database->delete('goose_db_version', ['version_id' => Schema::UPGRADE_IN_PROGRESS]);
            }
            self::assertSame(hash('sha256', $token), $database->fetchOne($postgres
                ? "SELECT encode(token_hash, 'hex') FROM account_tokens"
                : 'SELECT LOWER(HEX(token_hash)) FROM account_tokens'));

            $again = self::execute($args, $env, $input, $postgresMigrations);
            self::assertSame(1, $again[0]);
            self::assertSame('', $again[1]);
            self::assertStringContainsString('the school is set up already', $again[2]);
            self::assertContains($database->fetchOne('SELECT COUNT(*) FROM accounts'), [1, '1']);

            $database->executeStatement("UPDATE account_tokens SET created_at = '2000-01-01 00:00:00', expires_at = '2000-01-01 01:00:00'");
            $jobEnv = ['SMTP_HOST' => 'localhost', 'SMTP_FROM' => 'cron@example.test', 'SMTP_TLS' => 'none', 'DATABASE_URL' => $dsn];
            [$jobCode, $jobOutput, $jobError] = self::execute(['jobs:run'], ['SMTP_HOST' => '', 'SMTP_FROM' => ''] + $jobEnv, postgresMigrations: $postgresMigrations);
            self::assertSame(0, $jobCode, $jobError);
            self::assertStringContainsString('generate-lessons ok', $jobOutput);
            self::assertStringContainsString('purge-sessions', $jobOutput);
            self::assertStringContainsString('purge-mail ok', $jobOutput);
            self::assertStringContainsString('purge-personal-data ok', $jobOutput);
            self::assertStringNotContainsString('deliver-mail', $jobOutput);
            self::assertStringNotContainsString('deliver-push', $jobOutput);
            self::assertSame('', $jobError);
            self::assertSame([], $database->fetchFirstColumn('SELECT account_id FROM account_tokens'));
            self::assertSame(['ok'], $database->fetchFirstColumn("SELECT result FROM job_runs WHERE job = 'purge-sessions'"));
            [$jobCode, $jobOutput, $jobError] = self::execute(['jobs:run'], $jobEnv, postgresMigrations: $postgresMigrations);
            self::assertSame(0, $jobCode, $jobError);
            self::assertSame("deliver-mail ok\n", $jobOutput);
            [$jobCode, $jobOutput, $jobError] = self::execute(['jobs:run'], $jobEnv, postgresMigrations: $postgresMigrations);
            self::assertSame(0, $jobCode, $jobError);
            self::assertSame('', $jobOutput);
            self::assertSame(['ok'], $database->fetchFirstColumn("SELECT result FROM job_runs WHERE job = 'purge-sessions'"));
            self::assertSame(['ok'], $database->fetchFirstColumn("SELECT result FROM job_runs WHERE job = 'deliver-mail'"));
            $keys = VapidKeys::generate();
            $pushEnv = ['DATABASE_URL' => $dsn, 'VAPID_PUBLIC_KEY' => $keys->publicKey(), 'VAPID_SUBJECT' => 'mailto:admin@example.test'];
            [$jobCode, $jobOutput, $jobError] = self::execute(['jobs:run'], $pushEnv + ['VAPID_PRIVATE_KEY' => 'fixture-secret'], postgresMigrations: $postgresMigrations);
            self::assertSame(2, $jobCode);
            self::assertSame('', $jobOutput);
            self::assertStringContainsString('VAPID_PRIVATE_KEY', $jobError);
            self::assertStringNotContainsString('fixture-secret', $jobError);
            [$jobCode, $jobOutput, $jobError] = self::execute(['jobs:run'], $pushEnv + ['VAPID_PRIVATE_KEY' => $keys->privateKey()], postgresMigrations: $postgresMigrations);
            self::assertSame(0, $jobCode, $jobError);
            self::assertSame("deliver-push ok\n", $jobOutput);
            self::assertSame(['ok'], $database->fetchFirstColumn("SELECT result FROM job_runs WHERE job = 'deliver-push'"));
            self::assertMemberImportCommand($database, $dsn, $postgresMigrations);
        } finally {
            if (!$postgres) {
                $database->executeStatement('SET FOREIGN_KEY_CHECKS = 0');
            }
            foreach ($database->createSchemaManager()->listTableNames() as $table) {
                $database->executeStatement('DROP TABLE IF EXISTS ' . $database->quoteSingleIdentifier($table) . ($postgres ? ' CASCADE' : ''));
            }
            if ($postgres) {
                $database->executeStatement('DROP FUNCTION IF EXISTS receipts_immutable()');
            }
            $database->close();
        }
    }

    /** @return array{int,string,string} */
    private function healthcheck(string $status, string $mode = ''): array
    {
        $server = <<<'SERVER'
            $server = stream_socket_server('tcp://127.0.0.1:0');
            echo stream_socket_get_name($server, false), "\n";
            $client = stream_socket_accept($server, 8);
            $line = fgets($client);
            while (($header = fgets($client)) !== false && $header !== "\r\n") {
            }
            if ($argv[2] === 'no-headers') {
                sleep(5);
            }
            $status = $line === "GET /dance/schedule/healthz HTTP/1.1\r\n" ? $argv[1] : '404 Not Found';
            fwrite($client, "HTTP/1.1 $status\r\nLocation: /redirect\r\nConnection: close\r\nContent-Length: 1000\r\n\r\n");
            if ($argv[2] === 'head-only') {
                sleep(5);
            }
            SERVER;
        $pipes = [];
        $process = proc_open([PHP_BINARY, '-r', $server, $status, $mode], [1 => ['pipe', 'w'], 2 => ['pipe', 'w']], $pipes);
        self::assertIsResource($process);
        try {
            stream_set_timeout($pipes[1], 5);
            $address = fgets($pipes[1]);
            self::assertIsString($address);
            $port = substr(trim($address), strlen('127.0.0.1:'));

            return self::execute(
                ['healthcheck', '--addr=[::]:' . $port, '--base-url', 'https://school.example/dance/schedule/'],
                ['HTTP_ADDR' => '127.0.0.1:1', 'BASE_URL' => 'https://wrong.example/wrong', 'LOG_LEVEL' => 'loud'],
            );
        } finally {
            proc_terminate($process);
            fclose($pipes[1]);
            fclose($pipes[2]);
            proc_close($process);
        }
    }

    private static function assertMemberImportCommand(Connection $database, #[\SensitiveParameter] string $dsn, string $migrations): void
    {
        $file = tempnam(sys_get_temp_dir(), 'studio-import-');
        self::assertIsString($file);
        try {
            $csv = "Mitgliedsnummer;Vorname;Nachname;Geburtsdatum;Kurs;Gruppe\n"
                . "M90;René;Bauer;2012-02-03;Walzer;A\n"
                . "M91;Emil;Bauer;31.02.2015;Tango;B\n";
            self::assertSame(strlen($csv), file_put_contents($file, $csv));
            $environment = ['DATABASE_URL' => $dsn, 'STUDIO_AS' => 'ada@example.test'];

            [$code, $out, $error] = self::execute(['import', $file], ['DATABASE_URL' => $dsn], postgresMigrations: $migrations);
            self::assertSame(2, $code);
            self::assertSame('', $out);
            self::assertStringContainsString('give --as <email> or set STUDIO_AS', $error);
            [$code, $out, $error] = self::execute(['import', $file, '-o', 'json'], $environment, postgresMigrations: $migrations);
            self::assertSame(3, $code);
            self::assertSame('', $out);
            self::assertStringContainsString('"status": 404', $error);
            self::assertStringContainsString('not confirmed or is disabled', $error);

            $database->executeStatement("UPDATE accounts SET confirmed_at = '2026-11-01 09:00:00', role = 'member'");
            [$code, , $error] = self::execute(['import', '--apply', $file, '--output=json'], $environment, postgresMigrations: $migrations);
            self::assertSame(4, $code);
            self::assertStringContainsString('"status": 403', $error);
            $database->executeStatement("UPDATE accounts SET role = 'office'");
            [$code, , $error] = self::execute(['import', $file, '--as=absent@example.test'], $environment, postgresMigrations: $migrations);
            self::assertSame(3, $code);
            self::assertStringContainsString('no account has this email', $error);

            [$code, $out, $error] = self::execute(['import', '--notes', 'Kurs', $file], $environment, postgresMigrations: $migrations);
            self::assertSame(1, $code, $error);
            self::assertStringContainsString('line 2  create  M90  René Bauer', $out);
            self::assertStringContainsString('would import 2 rows: 1 created, 0 updated, 0 unchanged, 1 failed', $out);
            self::assertStringContainsString('dry run: nothing was written; run again with --apply to import', $out);
            self::assertSame("error: rows failed: 1 of 2 (see the lines marked error)\n", $error);
            self::assertContains($database->fetchOne('SELECT COUNT(*) FROM persons WHERE member_no IS NOT NULL'), [0, '0']);
            self::assertContains($database->fetchOne('SELECT COUNT(*) FROM import_runs'), [0, '0']);

            $mapping = [
                '--map=memberNo=Mitgliedsnummer', '--map', 'givenName=Vorname', '--map=familyName=Nachname',
                '--map=birthDate=Geburtsdatum', '--notes=Kurs', '--notes', 'Gruppe',
            ];
            [$code, $out, $error] = self::execute(
                ['--as', ' ADA@example.test ', '--output=json', 'import', ...$mapping, '--apply', '--', $file],
                ['DATABASE_URL' => $dsn, 'STUDIO_AS' => 'absent@example.test'],
                postgresMigrations: $migrations,
            );
            self::assertSame(1, $code, $error);
            self::assertSame('', $error);
            self::assertStringContainsString('"applied": true', $out);
            self::assertStringContainsString('"runId":', $out);
            self::assertStringContainsString('"created": 1', $out);
            self::assertStringContainsString('"failed": 1', $out);
            self::assertSame("Kurs: Walzer\nGruppe: A", $database->fetchOne("SELECT notes FROM persons WHERE member_no = 'M90'"));
            self::assertSame('ada@example.test', $database->fetchOne(
                'SELECT a.email FROM accounts a JOIN import_runs r ON r.actor_id = a.id',
            ));
            self::assertSame(basename($file), $database->fetchOne('SELECT file_name FROM import_runs'));

            [$code, $out, $error] = self::execute(
                ['import', $file, '--map=memberNo=Unknown', '--output=json'],
                $environment,
                postgresMigrations: $migrations,
            );
            self::assertSame(2, $code);
            self::assertSame('', $out);
            self::assertStringContainsString('"status": 422', $error);
            self::assertStringContainsString('"location": "--map memberNo"', $error);

            $validCsv = "Mitgliedsnummer;Vorname;Nachname;Geburtsdatum\nM90;René;Bauer;2012-02-03\n";
            self::assertSame(strlen($validCsv), file_put_contents($file, $validCsv));
            [$code, $out, $error] = self::execute(
                ['import', $file, '--apply=false', '--output=json'],
                $environment,
                postgresMigrations: $migrations,
            );
            self::assertSame(0, $code, $error);
            self::assertSame('', $error);
            self::assertStringContainsString('"applied": false', $out);
            self::assertStringContainsString('"unchanged": 1', $out);

            self::assertSame(0, file_put_contents($file, ''));
            [$code, $out, $error] = self::execute(
                ['import', $file, '--map=invalid', '--output=json'],
                $environment,
                postgresMigrations: $migrations,
            );
            self::assertSame(2, $code);
            self::assertSame('', $out);
            self::assertStringContainsString('"status": 400', $error);
            self::assertStringContainsString('invalid --map', $error);
            self::assertContains($database->fetchOne('SELECT COUNT(*) FROM import_runs'), [1, '1']);
        } finally {
            unlink($file);
        }
    }

    /**
     * @param list<string> $args
     * @param array<string,string> $environment
     * @return array{int,string,string}
     */
    private static function execute(array $args, array $environment = [], string $stdin = '', ?string $postgresMigrations = null): array
    {
        $in = fopen('php://memory', 'r+');
        $out = fopen('php://memory', 'r+');
        $error = fopen('php://memory', 'r+');
        self::assertIsResource($in);
        self::assertIsResource($out);
        self::assertIsResource($error);
        try {
            fwrite($in, $stdin);
            rewind($in);
            $cli = new Operator(dirname(__DIR__), $postgresMigrations ?? '/missing/postgres');
            $code = $cli->run($args, $in, $out, $error, static fn(string $key): ?string => $environment[$key] ?? null);
            rewind($out);
            rewind($error);

            return [$code, (string) stream_get_contents($out), (string) stream_get_contents($error)];
        } finally {
            fclose($in);
            fclose($out);
            fclose($error);
        }
    }

    /**
     * @param array<string,string> $environment
     * @param list<string> $arguments
     * @return array{list<array{name:string,result:string,detail:string}>, int}
     */
    private static function doctor(array $environment, array $arguments = [], ?string $postgresMigrations = null): array
    {
        [$code, $output, $error] = self::execute(['doctor', '--output=json', ...$arguments], $environment, postgresMigrations: $postgresMigrations);
        self::assertSame('', $error, $output);
        /** @var array{ok:bool,checks:list<array{name:string,result:string,detail:string}>} $report */
        $report = json_decode($output, true, flags: JSON_THROW_ON_ERROR);
        self::assertSame($code === 0, $report['ok']);

        return [$report['checks'], $code];
    }
}
