<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use UnexpectedValueException;

final class MigrationSql
{
    /**
     * Goose SQL uses one statement terminator at the end of a line. Compound
     * statements such as trigger bodies are delimited by StatementBegin and
     * StatementEnd.
     *
     * @return non-empty-list<string>
     */
    public static function up(string $file): array
    {
        return self::read($file, 'Up');
    }

    /** @return non-empty-list<string> */
    public static function down(string $file): array
    {
        return self::read($file, 'Down');
    }

    /**
     * @param 'Up'|'Down' $wanted
     * @return non-empty-list<string>
     */
    private static function read(string $file, string $wanted): array
    {
        $lines = @file($file, FILE_IGNORE_NEW_LINES);
        if ($lines === false) {
            throw new UnexpectedValueException('Could not read migration ' . basename($file));
        }

        $statements = [];
        $statement = '';
        $section = '';
        $compound = false;
        foreach ($lines as $line) {
            $trimmed = trim($line);
            if (str_starts_with($trimmed, '-- +goose ')) {
                switch ($trimmed) {
                    case '-- +goose Up':
                        if ($section === '') {
                            $section = 'Up';
                            continue 2;
                        }
                        break;
                    case '-- +goose Down':
                        if ($section === 'Up' && !$compound && $statement === '') {
                            if ($wanted === 'Up') {
                                break 2;
                            }
                            $section = 'Down';
                            continue 2;
                        }
                        break;
                    case '-- +goose StatementBegin':
                        if ($section !== '' && !$compound && $statement === '') {
                            $compound = true;
                            continue 2;
                        }
                        break;
                    case '-- +goose StatementEnd':
                        if ($compound && $statement !== '') {
                            if ($section === $wanted) {
                                $statements[] = $statement;
                            }
                            $statement = '';
                            $compound = false;
                            continue 2;
                        }
                        break;
                }
                throw self::invalid($file, $wanted);
            }

            if ($statement === '' && ($trimmed === '' || str_starts_with($trimmed, '--'))) {
                continue;
            }
            if ($section === '') {
                throw self::invalid($file, $wanted);
            }

            $statement .= $line . "\n";
            // Goose stops scanning for a terminator at the first comment token.
            $parts = preg_split('/(?:^|\s)--/', $trimmed, 2);
            if (!$compound && str_ends_with(rtrim($parts[0] ?? ''), ';')) {
                if ($section === $wanted) {
                    $statements[] = $statement;
                }
                $statement = '';
            }
        }

        if ($compound || $statement !== '' || $statements === []) {
            throw self::invalid($file, $wanted);
        }
        return $statements;
    }

    private static function invalid(string $file, string $wanted): UnexpectedValueException
    {
        return new UnexpectedValueException("Invalid $wanted section in migration " . basename($file));
    }
}
