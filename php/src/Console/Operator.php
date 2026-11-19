<?php

declare(strict_types=1);

namespace StudioSchedule\Console;

use Doctrine\DBAL\Exception as DatabaseError;
use InvalidArgumentException;
use StudioSchedule\Config;
use StudioSchedule\Push\VapidKeys;
use StudioSchedule\Service\AlreadyInitialized;
use StudioSchedule\Service\InvalidSetupInput;
use StudioSchedule\Service\SchoolSetup;
use StudioSchedule\Service\SetupInput;
use StudioSchedule\Service\SystemClock;
use StudioSchedule\Store\CopyError;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\DatabaseCopy;
use StudioSchedule\Store\IntegrityStore;
use StudioSchedule\Store\MigrationRecoveryRequired;
use StudioSchedule\Store\SetupStore;
use Throwable;

final readonly class Operator
{
    /** Go's prompt, optional default, and setup validation field for each flag. */
    private const array INIT_FIELDS = [
        'school-name' => ['School name', null, 'schoolName'],
        'timezone' => ['Time zone (IANA, e.g. Europe/Berlin)', null, 'timezone'],
        'locale' => ['Default language (en or de)', 'en', 'defaultLocale'],
        'currency' => ['Currency (ISO 4217)', 'EUR', 'currency'],
        'admin-email' => ['Email address of the first admin', null, 'email'],
        'admin-given-name' => ['Given name of the first admin', null, 'givenName'],
        'admin-family-name' => ['Family name of the first admin', null, 'familyName'],
    ];
    private const string HELP = "Usage: studio-schedule [--database-url URL] <command> [flags]\n\n"
        . "  init          Set up a new school and print the first admin's setup link\n"
        . "  healthcheck   Check the local server's liveness endpoint\n"
        . "  doctor        Check the configuration, database, mail, push keys and PDF renderer\n"
        . "  vapid-keys    Print a new VAPID key pair for web push\n"
        . "  migrate       Apply, roll back or list database migrations\n"
        . "  db check      Check the data for inconsistencies\n"
        . "  import        Import members from a CSV file (dry run unless --apply)\n"
        . "  jobs:run      Run due background jobs\n"
        . "  db:copy       Copy the data to an empty database on another engine\n";

    public function __construct(
        private string $appDirectory,
        private ?string $postgresMigrations = null,
    ) {}

    /**
     * @param list<string> $arguments
     * @param resource $input
     * @param resource $output
     * @param resource $errors
     * @param (callable(string):?string)|null $environment
     */
    public function run(
        #[\SensitiveParameter]
        array $arguments,
        $input,
        $output,
        $errors,
        ?callable $environment = null,
    ): int {
        $command = '';
        $flags = [];
        try {
            [$help, $only, $importFile, $maps, $notes] = self::parse($arguments, $command, $flags);
            if ($help || in_array($command, ['', 'migrate', 'db'], true)) {
                fwrite($output, self::help($command));

                return 0;
            }

            if ($command === 'vapid-keys') {
                $keys = VapidKeys::generate();
                $lines = 'VAPID_PUBLIC_KEY=' . $keys->publicKey() . "\nVAPID_PRIVATE_KEY=" . $keys->privateKey() . "\n";

                return fwrite($output, $lines) === strlen($lines) ? 0 : 1;
            }
            if ($command === 'doctor') {
                if (isset($flags['as'])) {
                    throw new CommandError('--as: studio-schedule doctor acts as no account', 2);
                }

                return (new Doctor($this->appDirectory, $this->postgresDirectory()))->run($flags, $environment, self::address(...), $output);
            }

            $get = Config::lookup($this->appDirectory . '/config/.env', $environment);
            if (str_starts_with($command, 'migrate ')) {
                MigrationCommand::run(
                    substr($command, strlen('migrate ')),
                    SetupInput::trim($flags['database-url'] ?? $get('DATABASE_URL')),
                    $this->postgresDirectory(),
                    $this->appDirectory . '/migrations/mysql',
                    ($flags['backup-verified'] ?? 'false') === 'true',
                    $output,
                );

                return 0;
            }

            if ($command === 'import') {
                assert($importFile !== null);

                return (new ImportCommand($this->currentSchema()))->run(
                    SetupInput::trim($flags['database-url'] ?? $get('DATABASE_URL')),
                    SetupInput::trim($flags['as'] ?? $get('STUDIO_AS')),
                    $importFile,
                    $maps,
                    $notes,
                    ($flags['apply'] ?? 'false') === 'true',
                    ($flags['output'] ?? 'text') === 'json',
                    $output,
                    $errors,
                );
            }

            if ($command === 'db check') {
                return (new DbCheck($this->currentSchema()))->run(
                    SetupInput::trim($flags['database-url'] ?? $get('DATABASE_URL')),
                    $only,
                    $flags['output'] ?? 'text',
                    $output,
                    $errors,
                    isset($flags['as']),
                );
            }

            if ($command === 'jobs:run') {
                return (new JobsCommand($this->postgresDirectory(), $this->appDirectory . '/migrations/mysql'))->run(
                    SetupInput::trim($flags['database-url'] ?? $get('DATABASE_URL')),
                    $get,
                    $output,
                    $errors,
                );
            }

            if ($command === 'db:copy') {
                $target = SetupInput::trim($flags['target-database-url'] ?? '');
                if ($target === '') {
                    throw new CommandError('db:copy requires --target-database-url', 2);
                }
                $source = SetupInput::trim($flags['database-url'] ?? $get('DATABASE_URL'));
                if ($source === '') {
                    throw new CommandError('no source database URL. Set DATABASE_URL or use --database-url.', 2);
                }
                $copy = new DatabaseCopy($this->postgresDirectory(), $this->appDirectory . '/migrations/mysql');
                $rows = $copy->copy($source, $target);
                fwrite($output, "Copied $rows rows. Database integrity checks passed.\n");

                return 0;
            }

            $baseSetting = isset($flags['base-url']) ? '--base-url' : 'BASE_URL';
            $baseInput = SetupInput::trim($flags['base-url'] ?? $get('BASE_URL'));
            try {
                [$baseURL, $basePath] = Config::parseBaseURL($baseInput);
            } catch (InvalidArgumentException $error) {
                throw new CommandError($baseSetting . ': ' . $error->getMessage(), 2);
            }
            $addrSetting = isset($flags['addr']) ? '--addr' : 'HTTP_ADDR';
            $environmentAddr = $get('HTTP_ADDR');
            $addr = $flags['addr'] ?? ($environmentAddr === '' ? ':8080' : $environmentAddr);
            [$host, $port] = self::address(SetupInput::trim($addr), $addrSetting);
            if ($command === 'healthcheck') {
                Healthcheck::check("http://$host:$port$basePath/healthz");
            } else {
                $databaseURL = SetupInput::trim($flags['database-url'] ?? $get('DATABASE_URL'));
                $appURL = $baseURL === '' ? "http://localhost:$port/" : $baseURL . '/';
                $this->initialize($databaseURL, $appURL, $flags, $input, $output, $errors);
            }

            return 0;
        } catch (CommandError $error) {
            if ($command === 'import' && ($flags['output'] ?? '') === 'json') {
                ImportCommand::report($errors, true, 400, $error->getMessage());
            } else {
                fwrite($errors, 'error: ' . $error->getMessage() . "\n");
            }

            return $error->status;
        } catch (MigrationRecoveryRequired|CopyError $error) {
            fwrite($errors, 'error: ' . $error->getMessage() . "\n");

            return 1;
        } catch (DatabaseError) {
            // Driver errors may contain credentials, bound values, or internal SQL.
            fwrite($errors, "error: database operation failed\n");

            return 1;
        } catch (Throwable) {
            fwrite($errors, "error: operator command failed\n");

            return 1;
        }
    }

    /**
     * @param list<string> $arguments
     * @param array<string,string> $flags
     * @param-out array<string,string> $flags
     * @return array{bool,list<string>,?string,list<string>,list<string>}
     */
    private static function parse(#[\SensitiveParameter] array $arguments, string &$command, #[\SensitiveParameter] array &$flags): array
    {
        $help = false;
        $only = $maps = $notes = [];
        $importFiles = [];
        $options = true;
        for ($index = 0; $index < count($arguments); $index++) {
            $arg = $arguments[$index];
            if ($options && ($arg === '--help' || $arg === '-h')) {
                $help = true;
                continue;
            }
            if ($options && $arg === '--') {
                $options = false;
                continue;
            }
            if (!$options || !str_starts_with($arg, '-')) {
                if ($command === 'import') {
                    $importFiles[] = $arg;
                    continue;
                }
                if ($command === 'migrate') {
                    if (!in_array($arg, ['up', 'down', 'status'], true)) {
                        throw new CommandError('unknown migration command', 2);
                    }
                    $command .= ' ' . $arg;
                    continue;
                }
                if ($command === 'db') {
                    if ($arg !== 'check') {
                        throw new CommandError('unknown database command', 2);
                    }
                    $command = 'db check';
                    continue;
                }
                if ($command !== '') {
                    throw new CommandError('unexpected argument', 2);
                }
                if (!in_array($arg, ['init', 'healthcheck', 'doctor', 'vapid-keys', 'migrate', 'db', 'db:copy', 'jobs:run', 'import'], true)) {
                    throw new CommandError('unknown command', 2);
                }
                $command = $arg;
                continue;
            }

            $parts = explode('=', $arg, 2);
            $name = $parts[0];
            $value = $parts[1] ?? null;
            if ($name === '--backup-verified' && in_array($command, ['migrate up', 'migrate down'], true)) {
                if ($value !== null && $value !== 'true' && $value !== 'false') {
                    throw new CommandError('--backup-verified accepts true or false', 2);
                }
                $flags['backup-verified'] = $value ?? 'true';
                continue;
            }
            if ($name === '--apply' && $command === 'import') {
                $boolean = $value ?? 'true';
                $flags['apply'] = match ($boolean) {
                    '1', 't', 'T', 'TRUE', 'true', 'True' => 'true',
                    '0', 'f', 'F', 'FALSE', 'false', 'False' => 'false',
                    default => throw new CommandError('--apply accepts true or false', 2),
                };
                continue;
            }
            $known = match ($name) {
                '--database-url', '--output', '-o' => true,
                '--base-url' => in_array($command, ['init', 'healthcheck', 'doctor'], true),
                '--target-database-url' => $command === 'db:copy',
                '--addr' => in_array($command, ['healthcheck', 'doctor'], true),
                '--log-format', '--log-level' => $command === 'doctor',
                '--as' => in_array($command, ['', 'db check', 'doctor', 'import'], true),
                '--map', '--notes' => $command === 'import',
                '--only' => $command === 'db check',
                default => $command === 'init' && str_starts_with($name, '--') && isset(self::INIT_FIELDS[substr($name, 2)]),
            };
            if (!$known) {
                throw new CommandError('unknown flag', 2);
            }
            if ($value === null) {
                $value = $arguments[++$index] ?? null;
                if ($value === null) {
                    throw new CommandError("$name needs a value", 2);
                }
            }
            if ($name === '--only') {
                if ($value !== '') {
                    foreach (str_getcsv($value, ',', '"', '') as $check) {
                        $only[] = $check ?? '';
                    }
                }
                continue;
            }
            if ($name === '--map') {
                $maps[] = $value;
                continue;
            }
            if ($name === '--notes') {
                $notes[] = $value;
                continue;
            }
            $flags[$name === '-o' ? 'output' : substr($name, 2)] = $value;
        }

        $format = $flags['output'] ?? 'text';
        $canPrintJson = in_array($command, ['db check', 'doctor', 'import'], true);
        if ($canPrintJson && !in_array($format, ['text', 'json'], true)) {
            throw new CommandError('output format must be text or json', 2);
        }
        if (!$canPrintJson && $format !== 'text') {
            throw new CommandError('this command prints text only', 2);
        }

        if ($command === 'import' && !$help && count($importFiles) !== 1) {
            throw new CommandError('accepts 1 arg(s), received ' . count($importFiles), 2);
        }
        if (isset($flags['as']) && !in_array($command, ['', 'db check', 'doctor', 'import'], true)) {
            throw new CommandError('unknown flag', 2);
        }

        return [$help, $only, $importFiles[0] ?? null, $maps, $notes];
    }

    private static function help(string $command): string
    {
        return match ($command) {
            'init' => "Usage: studio-schedule init [--database-url URL] [--base-url URL]\n"
                . '  ' . implode('  ', array_map(static fn(string $name): string => "--$name VALUE", array_keys(self::INIT_FIELDS))) . "\n"
                . "Values omitted from the command line are prompted for.\n",
            'healthcheck' => "Usage: studio-schedule healthcheck [--addr HOST:PORT] [--base-url URL]\n",
            'doctor' => "Usage: studio-schedule doctor [--database-url URL] [--base-url URL] [--addr HOST:PORT] [--output text|json]\n"
                . "  --log-format text|json  --log-level debug|info|warn|error\n",
            'vapid-keys' => "Usage: studio-schedule vapid-keys\n",
            'migrate' => "Usage: studio-schedule migrate <up|down|status> [--database-url URL]\n"
                . "MySQL and MariaDB upgrades and rollbacks require --backup-verified after verifying a full backup.\n",
            'migrate up', 'migrate down' => "Usage: studio-schedule $command [--database-url URL] [--backup-verified]\n",
            'migrate status' => "Usage: studio-schedule migrate status [--database-url URL]\n",
            'db', 'db check' => "Usage: studio-schedule db check [--database-url URL] [--only CHECKS] [--output text|json]\n"
                . '  Checks: ' . implode(', ', IntegrityStore::CHECKS) . "\n",
            'db:copy' => "Usage: studio-schedule db:copy [--database-url URL] --target-database-url URL\n"
                . "Stop writes to the source and migrate the empty target to the same release before copying.\n",
            'import' => "Usage: studio-schedule import [--database-url URL] [--as EMAIL] [--output text|json] <file.csv>\n"
                . "  --apply  --map FIELD=HEADER (repeatable)  --notes HEADER (repeatable)\n",
            'jobs:run' => "Usage: studio-schedule jobs:run [--database-url URL]\n",
            default => self::HELP,
        };
    }

    /** @return array{string,string} The host formatted for an HTTP URL and the listen port. */
    private static function address(string $addr, string $setting): array
    {
        try {
            return Config::parseHTTPAddress($addr, $setting);
        } catch (InvalidArgumentException $error) {
            throw new CommandError($error->getMessage(), 2);
        }
    }

    /**
     * @param array<string,string> $flags
     * @param resource $input
     * @param resource $output
     * @param resource $errors
     */
    private function initialize(
        #[\SensitiveParameter]
        string $databaseURL,
        string $appURL,
        #[\SensitiveParameter]
        array $flags,
        $input,
        $output,
        $errors,
    ): void {
        if ($databaseURL === '') {
            throw new CommandError('no database URL: set DATABASE_URL or --database-url', 2);
        }
        try {
            $database = Database::connect($databaseURL);
        } catch (InvalidArgumentException) {
            throw new CommandError('DATABASE_URL must name PostgreSQL, MySQL or MariaDB', 2);
        }
        try {
            $this->currentSchema()->requireCurrent($database);

            $values = [];
            foreach (self::INIT_FIELDS as $name => [$prompt, $fallback]) {
                $values[] = $flags[$name] ?? self::prompt($input, $errors, $name, $prompt, $fallback);
            }
            $data = new SetupInput(...$values);
            try {
                $validated = $data->validated();
                $result = (new SchoolSetup(new SetupStore($database), new SystemClock()))->init($validated);
            } catch (AlreadyInitialized) {
                throw new CommandError('the school is set up already: init only runs on a new database (change the settings in the app, as an admin)');
            } catch (InvalidSetupInput $error) {
                $field = array_search($error->field, array_column(self::INIT_FIELDS, 2), true);
                $flag = $field === false ? $error->field : array_keys(self::INIT_FIELDS)[$field];
                throw new CommandError("invalid --$flag: " . $error->getMessage());
            }

            $name = json_encode($data->schoolName, JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES | JSON_THROW_ON_ERROR);
            $email = $validated->adminEmail;
            $until = $result->expiresAt->format('Y-m-d H:i T');
            $link = $result->passwordLink($appURL);
            fwrite(
                $output,
                "The school $name is set up, with $email as its admin.\n"
                . "Open this link before $until to set the admin's password (it works once; keep it secret):\n\n  $link\n\n"
                . "After that, \"forgot password\" on the sign-in page sends a new link, once mail is configured.\n",
            );
        } finally {
            $database->close();
        }
    }

    private function currentSchema(): CurrentSchema
    {
        return new CurrentSchema($this->postgresDirectory(), $this->appDirectory . '/migrations/mysql');
    }

    private function postgresDirectory(): string
    {
        if ($this->postgresMigrations !== null) {
            return $this->postgresMigrations;
        }
        $packaged = $this->appDirectory . '/migrations/postgres';

        return is_dir($packaged) ? $packaged : dirname($this->appDirectory) . '/internal/store/postgres/migrations';
    }

    /**
     * @param resource $input
     * @param resource $errors
     */
    private static function prompt($input, $errors, string $name, string $prompt, ?string $fallback): string
    {
        if ($fallback !== null) {
            $prompt .= " [$fallback]";
        }
        fwrite($errors, "$prompt: ");
        $line = fgets($input);
        $value = $line === false ? '' : SetupInput::trim($line);
        if ($value === '' && $fallback === null) {
            throw new CommandError("missing --$name");
        }

        return $value === '' ? $fallback : $value;
    }
}
