<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use UnexpectedValueException;

final class MigrationSql
{
    /**
     * Goose SQL uses one statement terminator at the end of a line. Compound
     * statements such as trigger bodies are delimited by StatementBegin and
     * StatementEnd. Only the Up section is needed for installation and upgrades.
     *
     * @return non-empty-list<string>
     */
    public static function up(string $file): array
    {
        $lines = @file($file, FILE_IGNORE_NEW_LINES);
        if ($lines === false) {
            throw new UnexpectedValueException('Could not read migration ' . basename($file));
        }

        $statements = [];
        $statement = '';
        $up = false;
        $compound = false;
        foreach ($lines as $line) {
            $trimmed = trim($line);
            if (str_starts_with($trimmed, '-- +goose ')) {
                switch ($trimmed) {
                    case '-- +goose Up':
                        if (!$up) {
                            $up = true;
                            continue 2;
                        }
                        break;
                    case '-- +goose Down':
                        if ($up && !$compound && $statement === '') {
                            break 2;
                        }
                        break;
                    case '-- +goose StatementBegin':
                        if ($up && !$compound && $statement === '') {
                            $compound = true;
                            continue 2;
                        }
                        break;
                    case '-- +goose StatementEnd':
                        if ($compound && $statement !== '') {
                            $statements[] = $statement;
                            $statement = '';
                            $compound = false;
                            continue 2;
                        }
                        break;
                }
                throw self::invalid($file);
            }

            if ($statement === '' && ($trimmed === '' || str_starts_with($trimmed, '--'))) {
                continue;
            }
            if (!$up) {
                throw self::invalid($file);
            }

            $statement .= $line . "\n";
            // Goose stops scanning for a terminator at the first comment token.
            $parts = preg_split('/(?:^|\s)--/', $trimmed, 2);
            if (!$compound && str_ends_with(rtrim($parts[0] ?? ''), ';')) {
                $statements[] = $statement;
                $statement = '';
            }
        }

        if ($compound || $statement !== '' || $statements === []) {
            throw self::invalid($file);
        }
        return $statements;
    }

    private static function invalid(string $file): UnexpectedValueException
    {
        return new UnexpectedValueException('Invalid Up section in migration ' . basename($file));
    }
}
