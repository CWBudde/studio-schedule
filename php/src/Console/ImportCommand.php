<?php

declare(strict_types=1);

namespace StudioSchedule\Console;

use InvalidArgumentException;
use SensitiveParameter;
use StudioSchedule\Service\MemberCsv;
use StudioSchedule\Service\MemberImportBatch;
use StudioSchedule\Service\MemberImportHeaders;
use StudioSchedule\Service\MemberImportMapping;
use StudioSchedule\Service\MemberImportProblem;
use StudioSchedule\Service\MemberImports;
use StudioSchedule\Service\SystemClock;
use StudioSchedule\Store\AdminAccountsStore;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\MemberImportStore;
use StudioSchedule\Store\MigrationRecoveryRequired;
use Throwable;

/**
 * @phpstan-import-type ImportRow from MemberImportBatch
 */
final readonly class ImportCommand
{
    public function __construct(private CurrentSchema $schema) {}

    /**
     * @param list<string> $maps
     * @param list<string> $notes
     * @param resource $output
     * @param resource $errors
     */
    public function run(
        #[SensitiveParameter]
        string $dsn,
        #[SensitiveParameter]
        string $as,
        string $file,
        array $maps,
        array $notes,
        bool $apply,
        bool $json,
        $output,
        $errors,
    ): int {
        $database = null;
        try {
            if ($dsn === '') {
                throw new CommandError('no database URL: set DATABASE_URL or --database-url', 2);
            }
            if ($as === '') {
                throw new CommandError('studio-schedule import acts as an account: give --as <email> or set STUDIO_AS', 2);
            }
            try {
                $database = Database::connect($dsn);
            } catch (InvalidArgumentException) {
                throw new CommandError('DATABASE_URL must name PostgreSQL, MySQL or MariaDB', 2);
            }
            $this->schema->requireCurrent($database);
            $account = (new AdminAccountsStore($database))->byEmail($as);
            if ($account === null) {
                throw new CommandError("--as $as: not found: no account has this email", 3);
            }
            if (!$account['confirmed'] || $account['disabled']) {
                throw new CommandError("--as $as: not found: the account is not confirmed or is disabled", 3);
            }
            if (!in_array($account['role'], ['office', 'admin'], true)) {
                throw new CommandError("--as $as: forbidden: the account's role {$account['role']} is below office, which this command needs", 4);
            }

            $bytes = self::readCsv($file);
            $fields = $maps === [] ? null : self::mappedColumns($maps);
            try {
                $csv = MemberCsv::parse($bytes);
            } catch (InvalidArgumentException $failure) {
                throw new MemberImportProblem('content', $failure->getMessage());
            }
            $fields ??= MemberImportHeaders::suggest($csv->headers);
            $mapping = MemberImportMapping::resolve($csv->headers, $fields, $notes);
            $result = (new MemberImports(new MemberImportStore($database), new SystemClock()))
                ->run(basename($file), $csv, $mapping, $apply, $account['id']);
            $counts = $result['counts'];

            if ($json) {
                self::write($output, self::json($result));
            } else {
                self::write($output, self::table($result['rows'], stream_isatty($output)));
                $verb = $apply ? 'imported' : 'would import';
                self::write($output, "$verb {$counts['rows']} rows: {$counts['created']} created, {$counts['updated']} updated, "
                    . "{$counts['unchanged']} unchanged, {$counts['failed']} failed; guardians: {$counts['guardiansCreated']} created, "
                    . "{$counts['guardiansUpdated']} updated, {$counts['linksAdded']} links added\n");
                if (!$apply) {
                    self::write($output, "dry run: nothing was written; run again with --apply to import\n");
                }
            }
            if ($counts['failed'] === 0) {
                return 0;
            }
            if (!$json) {
                fwrite($errors, "error: rows failed: {$counts['failed']} of {$counts['rows']} (see the lines marked error)\n");
            }

            return 1;
        } catch (MemberImportProblem $failure) {
            $location = match ($failure->field) {
                'mapping.notes' => '--notes',
                'mapping.columns' => '--map',
                'content', 'fileName' => '<file.csv>',
                default => '--map ' . substr($failure->field, strlen('mapping.columns.')),
            };
            self::report($errors, $json, 422, $failure->getMessage(), $location);

            return 2;
        } catch (CommandError $failure) {
            $status = match ($failure->status) {
                2 => 400, 3 => 404, 4 => 403,
                default => 500,
            };
            self::report($errors, $json, $status, $failure->getMessage());

            return $failure->status;
        } catch (MigrationRecoveryRequired $failure) {
            self::report($errors, $json, 500, $failure->getMessage());

            return 1;
        } catch (Throwable) {
            self::report($errors, $json, 500, 'database operation failed');

            return 1;
        } finally {
            $database?->close();
        }
    }

    private static function readCsv(string $file): string
    {
        // A relative local filename must never be interpreted as a PHP stream wrapper.
        $path = str_starts_with($file, DIRECTORY_SEPARATOR) ? $file : '.' . DIRECTORY_SEPARATOR . $file;
        $bytes = $file === '' || str_contains($file, "\0") ? false : @file_get_contents($path, length: MemberCsv::MAX_BYTES + 1);
        if ($bytes === false) {
            throw new CommandError('could not read <file.csv>', 2);
        }
        return $bytes;
    }

    /**
     * @param list<string> $maps
     * @return array<string,string>
     */
    private static function mappedColumns(array $maps): array
    {
        $fields = [];
        foreach ($maps as $map) {
            [$field, $header] = explode('=', $map, 2) + [1 => null];
            if ($header === null || !in_array($field, MemberImportMapping::FIELDS, true)) {
                throw new CommandError('invalid --map: expected field=Header with a field of ' . implode(', ', MemberImportMapping::FIELDS), 2);
            }
            $fields[$field] = $header;
        }

        return $fields;
    }

    /** @param list<ImportRow> $rows */
    private static function table(array $rows, bool $terminal): string
    {
        $records = $terminal ? [['Line', 'Action', 'Member no.', 'Name', 'Details']] : [];
        foreach ($rows as $row) {
            $record = ['line ' . $row['line'], $row['action'], $row['memberNo'],
                $row['givenName'] . ' ' . $row['familyName'], self::detail($row)];
            $records[] = array_map(static fn(string $cell): string => $cell === '' ? '-' : $cell, $record);
        }

        $widths = [0, 0, 0, 0];
        foreach ($records as $record) {
            foreach ($widths as $i => $width) {
                $widths[$i] = max($width, mb_strlen($record[$i], 'UTF-8'));
            }
        }

        $text = '';
        foreach ($records as $record) {
            foreach ($widths as $i => $width) {
                $text .= $record[$i] . str_repeat(' ', $width - mb_strlen($record[$i], 'UTF-8') + 2);
            }
            $text .= $record[4] . "\n";
        }

        return $text;
    }

    /** @param ImportRow $row */
    private static function detail(array $row): string
    {
        $details = $row['issues'] === []
            ? $row['changes']
            : array_map(static fn(array $issue): string => $issue['field'] . ': ' . $issue['code'], $row['issues']);
        $summary = implode(', ', $details);
        $parts = $summary === '' ? [] : [$summary];
        foreach ($row['guardians'] as $guardian) {
            $parts[] = 'guardian ' . $guardian['givenName'] . ' ' . $guardian['familyName'] . ': ' . $guardian['action']
                . ($guardian['linked'] ? ', linked' : '');
        }
        $duplicates = [];
        foreach ($row['duplicates'] as $duplicate) {
            $name = $duplicate['givenName'] . ' ' . $duplicate['familyName'];
            if ($duplicates === [] || $duplicates[array_key_last($duplicates)] !== $name) {
                $duplicates[] = $name;
            }
        }
        if ($duplicates !== []) {
            $parts[] = 'possible duplicate of ' . implode(', ', $duplicates);
        }

        return implode('; ', $parts);
    }

    /** @param resource $errors */
    public static function report($errors, bool $json, int $status, string $message, ?string $location = null): void
    {
        $detail = $location === null ? $message : "$location: $message";
        if (!$json) {
            fwrite($errors, "error: $detail\n");

            return;
        }
        $problem = [
            'title' => match ($status) {
                400 => 'Bad Request', 403 => 'Forbidden', 404 => 'Not Found', 422 => 'Unprocessable Entity',
                default => 'Internal Server Error',
            },
            'status' => $status, 'detail' => $detail,
        ];
        if ($location !== null) {
            $problem['errors'] = [['message' => $message, 'location' => $location]];
        }
        fwrite($errors, self::json($problem));
    }

    /** @param array<string,mixed> $document */
    private static function json(array $document): string
    {
        return json_encode($document, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_PRETTY_PRINT | JSON_THROW_ON_ERROR) . "\n";
    }

    /** @param resource $output */
    private static function write($output, string $text): void
    {
        if (fwrite($output, $text) !== strlen($text)) {
            throw new CommandError('could not write the import report');
        }
    }
}
