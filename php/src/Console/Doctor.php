<?php

declare(strict_types=1);

namespace StudioSchedule\Console;

use Closure;
use DateTimeZone;
use Doctrine\DBAL\Connection;
use InvalidArgumentException;
use SensitiveParameter;
use StudioSchedule\Config;
use StudioSchedule\Domain\Currency;
use StudioSchedule\Mail\DeliveryFailure;
use StudioSchedule\Mail\SmtpMailer;
use StudioSchedule\Push\Sender;
use StudioSchedule\Push\VapidKeys;
use StudioSchedule\Receipt\Document;
use StudioSchedule\Receipt\Renderer;
use StudioSchedule\Service\InstallationEnvironment;
use StudioSchedule\Service\SetupInput;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\MigrationRecoveryRequired;
use StudioSchedule\Store\PublicSettingsStore;
use StudioSchedule\Store\Schema;
use Throwable;

/** @phpstan-type Check array{name:string,result:'ok'|'fail'|'warn'|'skip',detail:string} */
final readonly class Doctor
{
    private const array CONFIG_FLAGS = [
        'BASE_URL' => 'base-url', 'DATABASE_URL' => 'database-url', 'HTTP_ADDR' => 'addr',
        'LOG_FORMAT' => 'log-format', 'LOG_LEVEL' => 'log-level',
    ];

    public function __construct(private string $appDirectory, private string $postgresMigrations) {}

    /**
     * @param array<string,string> $flags
     * @param (callable(string):?string)|null $environment
     * @param Closure(string,string):array{string,string} $parseAddress
     * @param resource $output
     */
    public function run(#[SensitiveParameter] array $flags, ?callable $environment, Closure $parseAddress, $output): int
    {
        try {
            [$config, $port, $smtp, $smtpHost, $smtpUsername, $vapid] = $this->configuration($flags, $environment, $parseAddress);
            $checks = [
                self::check('config', 'ok', 'valid'),
                ...$this->database($config->databaseURL),
                self::smtp($smtp, $smtpHost, $smtpUsername),
                $vapid
                    ? self::check('vapid', 'ok', 'keys parse and match')
                    : self::check('vapid', 'skip', 'the VAPID_* variables are not set: web push is off'),
                self::baseURL($config, $port),
            ];
        } catch (InvalidArgumentException|CommandError $error) {
            $checks = [self::check('config', 'fail', $error->getMessage())];
            foreach (['database', 'schema', 'settings', 'smtp', 'vapid', 'base-url'] as $name) {
                $checks[] = self::check($name, 'skip', 'the configuration is invalid');
            }
        }
        $checks[] = self::receipts();
        $ok = !in_array('fail', array_column($checks, 'result'), true);
        if (($flags['output'] ?? 'text') === 'json') {
            fwrite($output, json_encode(['ok' => $ok, 'checks' => $checks], JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_INVALID_UTF8_SUBSTITUTE | JSON_THROW_ON_ERROR) . "\n");
        } else {
            self::printText($output, $checks);
        }

        return $ok ? 0 : 1;
    }

    /**
     * @param array<string,string> $flags
     * @param (callable(string):?string)|null $environment
     * @param Closure(string,string):array{string,string} $parseAddress
     * @return array{Config,string,?SmtpMailer,string,string,bool}
     */
    private function configuration(#[SensitiveParameter] array $flags, ?callable $environment, Closure $parseAddress): array
    {
        try {
            $lookup = Config::lookup($this->appDirectory . '/config/.env', $environment);
        } catch (Throwable) {
            // A dotenv parser error can include an entire line with a credential.
            throw new InvalidArgumentException('could not read the PHP configuration file');
        }
        $get = static function (string $key) use ($flags, $lookup): string {
            $flag = self::CONFIG_FLAGS[$key] ?? null;
            return $flag !== null && isset($flags[$flag]) ? SetupInput::trim($flags[$flag]) : $lookup($key);
        };
        $config = Config::fromLookup($get);
        if ($config->databaseURL !== '' && Database::driverForURL($config->databaseURL) === null) {
            throw new InvalidArgumentException('DATABASE_URL must name PostgreSQL, MySQL or MariaDB');
        }
        $address = $get('HTTP_ADDR');
        [, $port] = $parseAddress($address === '' && !isset($flags['addr']) ? ':8080' : $address, isset($flags['addr']) ? '--addr' : 'HTTP_ADDR');

        $smtpHost = $get('SMTP_HOST');
        $smtpUsername = $get('SMTP_USERNAME');
        if ($smtpHost === '' && ($get('SMTP_FROM') !== '' || $smtpUsername !== '' || $get('SMTP_PASSWORD') !== '')) {
            throw new InvalidArgumentException('SMTP_HOST is required when other SMTP credentials are set');
        }
        $smtp = SmtpMailer::fromEnvironment($get);

        return [$config, $port, $smtp, $smtpHost, $smtpUsername, self::vapid($get)];
    }

    /** @param Closure(string):string $get */
    private static function vapid(Closure $get): bool
    {
        $public = $get('VAPID_PUBLIC_KEY');
        $private = $get('VAPID_PRIVATE_KEY');
        $subject = $get('VAPID_SUBJECT');
        if ($public === '' && $private === '' && $subject === '') {
            return false;
        }
        if ($public === '' || $private === '' || $subject === '') {
            throw new InvalidArgumentException('Set VAPID_PUBLIC_KEY, VAPID_PRIVATE_KEY and VAPID_SUBJECT together');
        }
        try {
            VapidKeys::parse($public, $private);
        } catch (InvalidArgumentException) {
            throw new InvalidArgumentException('VAPID_PUBLIC_KEY and VAPID_PRIVATE_KEY must be a matching pair');
        }
        Sender::validateSubject($subject);

        return true;
    }

    /** @return list<Check> */
    private function database(#[SensitiveParameter] string $dsn): array
    {
        $driver = Database::driverForURL($dsn);
        if ($driver === null) {
            return [
                self::check('database', 'fail', 'no database: set DATABASE_URL or --database-url'),
                self::check('schema', 'skip', 'no database'),
                self::check('settings', 'skip', 'no database'),
            ];
        }

        $database = null;
        try {
            $database = Database::connect($dsn, dedicated: true, timeoutSeconds: 5);
            $version = Database::serverVersion($database, $driver);
            $problem = InstallationEnvironment::databaseProblem($driver, $version);
            if ($problem !== null) {
                return [
                    self::check('database', 'fail', $problem),
                    self::check('schema', 'skip', 'the database version is unsupported'),
                    self::check('settings', 'skip', 'the database version is unsupported'),
                ];
            }

            $schema = $this->schema($database, $driver === 'pdo_pgsql');
            return [
                self::check('database', 'ok', 'connected'),
                $schema,
                $schema['result'] === 'ok'
                    ? self::settings($database)
                    : self::check('settings', 'skip', 'the schema does not match'),
            ];
        } catch (Throwable) {
            return [
                self::check('database', 'fail', 'could not connect to the database'),
                self::check('schema', 'skip', 'the database is unreachable'),
                self::check('settings', 'skip', 'the database is unreachable'),
            ];
        } finally {
            $database?->close();
        }
    }

    /** @return Check */
    private function schema(Connection $database, bool $postgres): array
    {
        try {
            $expected = Schema::migrationVersions($postgres ? $this->postgresMigrations : $this->appDirectory . '/migrations/mysql');
            if ($expected === []) {
                return self::check('schema', 'fail', 'the release has no migration files');
            }
            $schema = new Schema($database);
            $applied = in_array('goose_db_version', $schema->relations(), true) ? $schema->appliedVersions() : [];
            if (array_diff($applied, $expected) !== []) {
                return self::check('schema', 'fail', 'the database schema is newer than this binary: run a newer studio-schedule');
            }
            if ($applied !== $expected) {
                return self::check('schema', 'fail', 'the database schema is not up to date: run studio-schedule migrate up');
            }
        } catch (MigrationRecoveryRequired $error) {
            return self::check('schema', 'fail', $error->getMessage());
        } catch (Throwable) {
            return self::check('schema', 'fail', 'could not verify the database migration history');
        }

        return self::check('schema', 'ok', 'up to date');
    }

    /** @return Check */
    private static function settings(Connection $database): array
    {
        try {
            $settings = (new PublicSettingsStore($database))->settings();
        } catch (Throwable) {
            return self::check('settings', 'fail', 'could not read the school settings');
        }
        if ($settings === null) {
            return self::check('settings', 'fail', 'no school settings: run init');
        }
        $schoolName = SetupInput::trim($settings['school_name']);
        if ($schoolName === '' || mb_strlen($settings['school_name'], 'UTF-8') > 200) {
            return self::check('settings', 'fail', 'school name must have 1 to 200 characters');
        }
        if (mb_strlen($settings['legal_notice'] ?? '', 'UTF-8') > 2000 || !in_array($settings['default_locale'], ['en', 'de'], true)) {
            return self::check('settings', 'fail', 'check the legal notice and default locale');
        }
        if (Currency::exponent($settings['currency']) === null) {
            return self::check('settings', 'fail', 'currency must be an ISO 4217 code');
        }
        $timezone = $settings['timezone'];
        if (!in_array($timezone, DateTimeZone::listIdentifiers(DateTimeZone::ALL_WITH_BC), true)) {
            return self::check('settings', 'fail', 'unknown timezone ' . $timezone);
        }

        return self::check('settings', 'ok', $timezone . ', ' . $settings['currency']);
    }

    /** @return Check */
    private static function smtp(?SmtpMailer $mail, string $host, string $username): array
    {
        if ($mail === null) {
            return self::check('smtp', 'skip', 'SMTP_HOST is not set: mails stay in the outbox');
        }
        try {
            $mail->verify();
        } catch (DeliveryFailure $error) {
            return self::check('smtp', 'fail', $error->getMessage());
        }

        return self::check('smtp', 'ok', $username === '' ? "connected to $host (no login configured)" : "logged in to $host as $username");
    }

    /** @return Check */
    private static function baseURL(Config $config, string $port): array
    {
        $url = $config->baseURL;
        if ($url === '') {
            return self::check('base-url', 'warn', "BASE_URL is not set: links point to http://localhost:$port/ (local development only)");
        }
        if ($config->secure) {
            return self::check('base-url', 'ok', $url);
        }
        if (in_array(parse_url($url, PHP_URL_HOST), ['localhost', '127.0.0.1', '[::1]'], true)) {
            return self::check('base-url', 'warn', "$url is plain http (local development only)");
        }

        return self::check('base-url', 'fail', "$url is plain http: session cookies are not Secure. Use https.");
    }

    /** @return Check */
    private static function receipts(): array
    {
        try {
            $sample = Document::fromSnapshot([
                'number' => 'SAMPLE-2026-0001', 'kind' => 'sale', 'issue_date' => '2026-09-22', 'locale' => 'en',
                'issuer_name' => 'Example Dance School', 'issuer_address' => "1 Example Street\n12345 Example Town",
                'issuer_tax_id' => '00/000/00000', 'issuer_email' => 'office@example.org', 'issuer_phone' => null,
                'tax_mode' => 'standard', 'tax_note' => null, 'footer' => null,
                'recipient_name' => 'Erika Example', 'recipient_address' => null, 'payment_method' => 'cash',
                'currency' => 'EUR', 'gross' => 10000, 'reason' => null, 'original_number' => null, 'original_date' => null,
            ], [[
                'position' => 1, 'description' => '10-class pass', 'quantity' => 1, 'unit_price' => 10000, 'vat_rate_bp' => 1900,
                'net' => 8403, 'vat' => 1597, 'gross' => 10000,
            ]]);
            $bytes = (new Renderer())->render($sample);
            if (str_starts_with($bytes, '%PDF-')) {
                return self::check('receipts', 'ok', 'a sample receipt renders as PDF');
            }
        } catch (Throwable) {
            return self::check('receipts', 'fail', 'could not render the sample receipt');
        }

        return self::check('receipts', 'fail', 'the renderer did not produce a PDF');
    }

    /**
     * @param 'ok'|'fail'|'warn'|'skip' $result
     * @return Check
     */
    private static function check(string $name, string $result, string $detail): array
    {
        $detail = (string) preg_replace('/[\x00-\x1f\x7f]/', ' ', trim($detail));

        return ['name' => $name, 'result' => $result, 'detail' => $detail];
    }

    /**
     * @param resource $output
     * @param list<Check> $checks
     */
    private static function printText($output, array $checks): void
    {
        $terminal = stream_isatty($output);
        if ($terminal) {
            fwrite($output, "Check     Result  Detail\n");
        }
        $resultWidth = $terminal ? 8 : 6;
        foreach ($checks as $check) {
            fwrite($output, str_pad($check['name'], 10) . str_pad($check['result'], $resultWidth) . $check['detail'] . "\n");
        }
    }
}
