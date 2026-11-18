<?php

declare(strict_types=1);

namespace StudioSchedule\Console;

use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use InvalidArgumentException;
use SensitiveParameter;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\IntegrityStore;
use StudioSchedule\Store\MigrationRecoveryRequired;
use StudioSchedule\Store\Schema;
use Throwable;

/** @phpstan-import-type Finding from IntegrityStore */
final readonly class DbCheck
{
    public function __construct(private string $postgresMigrations, private string $mysqlMigrations) {}

    /**
     * @param list<string> $only
     * @param resource $out
     * @param resource $errors
     */
    public function run(
        #[SensitiveParameter]
        string $dsn,
        array $only,
        string $format,
        $out,
        $errors,
        bool $asSpecified = false,
    ): int {
        $json = $format === 'json';
        if ($asSpecified) {
            return self::error($errors, $json, 400, '--as: studio-schedule db check acts as no account');
        }
        try {
            $checks = IntegrityStore::select($only);
        } catch (InvalidArgumentException $error) {
            return self::error($errors, $json, 422, '--only: ' . $error->getMessage(), $error->getMessage());
        }
        if ($dsn === '') {
            return self::error($errors, $json, 400, 'no database URL: set DATABASE_URL or --database-url');
        }

        $database = null;
        try {
            try {
                $database = Database::connect($dsn);
            } catch (InvalidArgumentException) {
                return self::error($errors, $json, 400, 'DATABASE_URL must name PostgreSQL, MySQL or MariaDB');
            }
            $findings = Database::inReadSnapshot($database, function () use ($database, $checks): array {
                $this->checkSchema($database);

                return (new IntegrityStore($database))->findings($checks);
            });
        } catch (MigrationRecoveryRequired|CommandError $error) {
            return self::error($errors, $json, 500, $error->getMessage());
        } catch (Throwable) {
            return self::error($errors, $json, 500, 'database operation failed');
        } finally {
            $database?->close();
        }

        if ($json) {
            self::writeJson($out, ['ok' => $findings === [], 'checks' => $checks, 'findings' => $findings]);

            return $findings === [] ? 0 : 1;
        }
        if ($findings === []) {
            fwrite($out, 'no findings: ' . implode(', ', $checks) . "\n");

            return 0;
        }

        self::writeFindings($out, $findings);
        $count = count($findings);
        fwrite($errors, $count === 1 ? "error: 1 finding\n" : "error: $count findings\n");

        return 1;
    }

    private function checkSchema(Connection $database): void
    {
        $expected = Schema::migrationVersions(
            $database->getDatabasePlatform() instanceof PostgreSQLPlatform ? $this->postgresMigrations : $this->mysqlMigrations,
        );
        if ($expected === []) {
            throw new CommandError('the release has no migration files');
        }
        $schema = new Schema($database);
        $applied = in_array('goose_db_version', $schema->relations(), true) ? $schema->appliedVersions() : [];
        if (array_diff($applied, $expected) !== []) {
            throw new CommandError('the database schema is newer than this binary: run a newer studio-schedule');
        }
        if ($applied !== $expected) {
            throw new CommandError('the database schema is not up to date: run studio-schedule migrate up');
        }
    }

    /**
     * @param resource $out
     * @param list<Finding> $findings
     */
    private static function writeFindings($out, array $findings): void
    {
        $widths = [0, 0, 0];
        $terminal = stream_isatty($out);
        $rows = array_map(static fn(array $finding): array => array_values($finding), $findings);
        if ($terminal) {
            array_unshift($rows, ['Check', 'Entity', 'ID', 'Message']);
        }
        foreach ($rows as [$check, $entity, $id]) {
            $widths = [max($widths[0], strlen($check)), max($widths[1], strlen($entity)), max($widths[2], strlen($id))];
        }
        foreach ($rows as $index => [$check, $entity, $id, $message]) {
            $line = str_pad($check, $widths[0] + 2) . str_pad($entity, $widths[1] + 2) . str_pad($id, $widths[2] + 2) . $message;
            if ($terminal && $index === 0) {
                $labels = [$check, $entity, $id, $message];
                $bold = array_map(static fn(string $label): string => "\033[1m" . $label . "\033[0m", $labels);
                $line = strtr($line, array_combine($labels, $bold));
            }
            fwrite($out, $line . "\n");
        }
    }

    /** @param resource $errors */
    private static function error($errors, bool $json, int $status, string $detail, ?string $fieldMessage = null): int
    {
        if ($json) {
            $problem = [
                'title' => match ($status) {
                    400 => 'Bad Request',
                    422 => 'Unprocessable Entity',
                    default => 'Internal Server Error',
                },
                'status' => $status,
                'detail' => $detail,
            ];
            if ($fieldMessage !== null) {
                $problem['errors'] = [['message' => $fieldMessage, 'location' => '--only']];
            }
            self::writeJson($errors, $problem);
        } else {
            fwrite($errors, "error: $detail\n");
        }

        return $status >= 500 ? 1 : 2;
    }

    /**
     * @param resource $out
     * @param array<string,mixed> $value
     */
    private static function writeJson($out, array $value): void
    {
        fwrite($out, json_encode($value, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_PRETTY_PRINT | JSON_THROW_ON_ERROR) . "\n");
    }
}
