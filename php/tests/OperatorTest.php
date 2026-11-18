<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use PHPUnit\Framework\TestCase;
use StudioSchedule\Console\Operator;
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
            $maintenance = self::execute(['jobs:run'], ['SMTP_HOST' => '', 'DATABASE_URL' => $dsn], postgresMigrations: $postgresMigrations);
            self::assertSame(1, $maintenance[0]);
            self::assertStringContainsString('database schema is not up to date', $maintenance[2]);
            $result = self::execute(['init'], $env, postgresMigrations: $postgresMigrations);
            self::assertSame(1, $result[0]);
            self::assertStringContainsString('database schema is not up to date', $result[2]);
            self::assertSame([], $schema->relations());

            (new Migrator($dsn))->up($migrations);
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
            [$jobCode, $jobOutput, $jobError] = self::execute(['jobs:run'], $jobEnv, postgresMigrations: $postgresMigrations);
            self::assertSame(0, $jobCode, $jobError);
            self::assertStringContainsString('purge-sessions', $jobOutput);
            self::assertStringContainsString('deliver-mail', $jobOutput);
            self::assertSame('', $jobError);
            self::assertSame([], $database->fetchFirstColumn('SELECT account_id FROM account_tokens'));
            self::assertSame(['ok'], $database->fetchFirstColumn("SELECT result FROM job_runs WHERE job = 'purge-sessions'"));
            [$jobCode, $jobOutput, $jobError] = self::execute(['jobs:run'], $jobEnv, postgresMigrations: $postgresMigrations);
            self::assertSame(0, $jobCode, $jobError);
            self::assertSame('', $jobOutput);
            self::assertSame(['ok'], $database->fetchFirstColumn("SELECT result FROM job_runs WHERE job = 'purge-sessions'"));
            self::assertSame(['ok'], $database->fetchFirstColumn("SELECT result FROM job_runs WHERE job = 'deliver-mail'"));
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
}
