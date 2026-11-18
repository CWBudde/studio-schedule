<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Exception as DatabaseError;
use Doctrine\DBAL\ParameterType;
use Doctrine\DBAL\Platforms\AbstractMySQLPlatform;
use Doctrine\DBAL\Platforms\MySQLPlatform;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use Generator;
use PDO;
use RuntimeException;
use SensitiveParameter;
use Throwable;

final class CopyRows
{
    /**
     * The caller holds the source snapshot and target write transaction.
     *
     * @param array<string,string> $columns PostgreSQL data types by column name
     * @param array{local:string,key:string}|null $selfReference
     */
    public static function copy(
        Connection $source,
        Connection $target,
        string $table,
        array $columns,
        ?array $selfReference = null,
    ): int {
        $sourcePlatform = $source->getDatabasePlatform();
        $targetPlatform = $target->getDatabasePlatform();
        $postgresSource = $sourcePlatform instanceof PostgreSQLPlatform;
        if (
            !($postgresSource && $targetPlatform instanceof AbstractMySQLPlatform)
            && !($sourcePlatform instanceof AbstractMySQLPlatform && $targetPlatform instanceof PostgreSQLPlatform)
        ) {
            throw new CopyError('Copy rows requires PostgreSQL and MySQL or MariaDB');
        }

        try {
            $sourceTable = $source->quoteSingleIdentifier($table);
            $targetTable = $target->quoteSingleIdentifier($table);
            $selectColumns = $insertColumns = $parameters = [];
            foreach ($columns as $column => $type) {
                $selectColumns[] = self::readExpression('r.' . $source->quoteSingleIdentifier($column), $type, $postgresSource);
                $insertColumns[] = $target->quoteSingleIdentifier($column);
                $parameters[] = self::writeExpression($type, $postgresSource);
            }
            $select = 'SELECT ' . implode(', ', $selectColumns) . " FROM $sourceTable AS r";
            if ($selfReference !== null) {
                $key = $source->quoteSingleIdentifier($selfReference['key']);
                $parent = $source->quoteSingleIdentifier($selfReference['local']);
                $select = "WITH RECURSIVE copy_order (node, depth) AS (
                    SELECT $key, 0 FROM $sourceTable WHERE $parent IS NULL
                    UNION ALL
                    SELECT child.$key, predecessor.depth + 1
                    FROM $sourceTable AS child JOIN copy_order AS predecessor ON child.$parent = predecessor.node
                ) $select JOIN copy_order ON r.$key = copy_order.node ORDER BY copy_order.depth";
            }

            $insert = $target->prepare("INSERT INTO $targetTable (" . implode(', ', $insertColumns)
                . ') VALUES (' . implode(', ', $parameters) . ')');
            $types = array_values($columns);
            $batchSize = in_array('bytea', $types, true) ? 1 : 64;
            $checkNativeJson = $targetPlatform instanceof MySQLPlatform;
            $count = 0;
            $orderedTable = $selfReference === null ? null : $sourceTable;
            foreach (self::readRows($source, $select, $postgresSource, $batchSize, $orderedTable) as $row) {
                foreach ($types as $index => $type) {
                    $value = self::writeValue($row[$index], $type, $postgresSource);
                    if ($checkNativeJson && $type === 'jsonb' && $value !== null) {
                        self::checkNativeJson($source, $target, $table, $value);
                    }
                    $binding = match (true) {
                        $value === null => ParameterType::NULL,
                        $type === 'bytea' && $postgresSource => ParameterType::BINARY,
                        default => ParameterType::STRING,
                    };
                    $insert->bindValue($index + 1, $value, $binding);
                }
                if ((int) $insert->executeStatement() !== 1) {
                    throw new RuntimeException('Copy insert must write one row');
                }
                $count++;
            }

            return $count;
        } catch (CopyError $error) {
            throw $error;
        } catch (Throwable) {
            // Database errors can contain credentials or rejected record values.
            throw new CopyError("Could not copy rows from $table");
        }
    }

    private static function checkNativeJson(
        Connection $source,
        Connection $target,
        string $table,
        #[SensitiveParameter]
        string $value,
    ): void {
        // MySQL may round a JSON number to a double when it inserts the value.
        // Compare the serialized result using PostgreSQL's exact JSONB equality.
        try {
            $encoded = $target->fetchOne('SELECT CAST(? AS JSON)', [$value]);
            if (is_string($encoded) && $source->fetchOne('SELECT ?::jsonb = ?::jsonb', [$value, $encoded]) === true) {
                return;
            }
        } catch (DatabaseError) {
            // A MySQL parser error can include the JSON text.
        }

        throw new CopyError("MySQL cannot preserve a JSON value in $table");
    }

    private static function readExpression(string $column, string $type, bool $postgres): string
    {
        return match ($type) {
            'uuid' => $postgres ? "REPLACE(CAST($column AS text), '-', '')" : "LOWER(HEX($column))",
            'bytea' => $postgres ? "ENCODE($column, 'hex')" : $column,
            'boolean' => $postgres ? "CAST($column AS integer)" : $column,
            'timestamp with time zone' => $postgres
                ? "CASE WHEN ISFINITE($column) THEN TO_CHAR($column AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS.US AD')
                    WHEN $column IS NOT NULL THEN 'invalid' END"
                : $column,
            'interval' => $postgres ? self::intervalMicros($column) : $column,
            'json', 'jsonb' => $postgres ? "CAST($column AS text)" : $column,
            'text', 'character varying', 'date', 'time without time zone', 'smallint', 'integer', 'bigint' => $column,
            default => throw new RuntimeException('Unsupported copy column type'),
        };
    }

    private static function writeExpression(string $type, bool $postgresSource): string
    {
        if ($postgresSource) {
            return $type === 'uuid' ? 'UNHEX(?)' : '?';
        }

        return match ($type) {
            'bytea' => "DECODE(?, 'hex')",
            'timestamp with time zone' => "CAST(? AS timestamp) AT TIME ZONE 'UTC'",
            'uuid', 'boolean', 'interval', 'json', 'jsonb' => "CAST(? AS $type)",
            default => '?',
        };
    }

    private static function writeValue(#[SensitiveParameter] mixed $raw, string $type, bool $postgresSource): ?string
    {
        if ($raw === null) {
            return null;
        }
        if (!is_string($raw) && !is_int($raw)) {
            throw new RuntimeException('Unexpected copy column value');
        }
        $value = (string) $raw;

        return match ($type) {
            'uuid' => self::validated($value, preg_match('/\A[0-9a-f]{32}\z/', $value) === 1),
            'boolean' => self::validated($value, $value === '0' || $value === '1'),
            'bytea' => $postgresSource ? self::unhex($value) : bin2hex($value),
            'interval' => $postgresSource ? $value : self::writeInterval($value),
            'date' => self::validated($value, self::isDate($value)),
            'time without time zone' => self::validated(
                $value,
                preg_match('/\A(?:(?:[01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](?:\.[0-9]{1,6})?|24:00:00(?:\.0{1,6})?)\z/', $value) === 1,
            ),
            'timestamp with time zone' => self::writeTimestamp($value, $postgresSource),
            'text', 'character varying' => self::validated($value, !str_contains($value, "\0") && mb_check_encoding($value, 'UTF-8')),
            default => $value,
        };
    }

    private static function validated(#[SensitiveParameter] string $value, bool $valid): string
    {
        return $valid ? $value : throw new RuntimeException('Invalid copy column value');
    }

    private static function unhex(#[SensitiveParameter] string $value): string
    {
        $binary = hex2bin($value);

        return $binary !== false ? $binary : throw new RuntimeException('Invalid copy binary value');
    }

    private static function isDate(#[SensitiveParameter] string $value): bool
    {
        return preg_match('/\A([0-9]{4})-([0-9]{2})-([0-9]{2})\z/', $value, $parts) === 1
            && checkdate((int) $parts[2], (int) $parts[3], (int) $parts[1]);
    }

    private static function writeTimestamp(#[SensitiveParameter] string $value, bool $postgresSource): string
    {
        if ($postgresSource) {
            if (!str_ends_with($value, ' AD')) {
                throw new RuntimeException('Invalid copy timestamp');
            }
            $value = substr($value, 0, -3);
        }

        return self::validated(
            $value,
            self::isDate(substr($value, 0, 10))
                && preg_match('/\A (?:[01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](?:\.[0-9]{1,6})?\z/', substr($value, 10)) === 1,
        );
    }

    private static function intervalMicros(string $column): string
    {
        // The Go store treats all months, including the months in a year, as 30 days.
        $months = "(EXTRACT(YEAR FROM $column) * 12 + EXTRACT(MONTH FROM $column))";
        $days = "($months * 30 + EXTRACT(DAY FROM $column))";
        $hours = "($days * 24 + EXTRACT(HOUR FROM $column))";
        $minutes = "($hours * 60 + EXTRACT(MINUTE FROM $column))";
        $seconds = "($minutes * 60 + EXTRACT(SECOND FROM $column))";

        return "CAST($seconds * 1000000 AS bigint)";
    }

    private static function writeInterval(#[SensitiveParameter] string $value): string
    {
        $microseconds = filter_var($value, FILTER_VALIDATE_INT);
        if ($microseconds === false) {
            throw new RuntimeException('Invalid copy duration');
        }

        $seconds = intdiv($microseconds, 1000000);
        $remainder = $microseconds % 1000000;

        return "$seconds seconds $remainder microseconds";
    }

    /** @return Generator<int,list<mixed>> */
    private static function readRows(
        Connection $source,
        string $select,
        bool $postgres,
        int $batchSize,
        ?string $orderedTable,
    ): Generator {
        if ($postgres) {
            $source->executeStatement("DECLARE studio_copy_rows NO SCROLL CURSOR FOR $select");
            do {
                $page = $source->fetchAllNumeric("FETCH FORWARD $batchSize FROM studio_copy_rows");
                yield from $page;
            } while (count($page) === $batchSize);
            $source->executeStatement('CLOSE studio_copy_rows');

            return;
        }

        $pdo = $source->getNativeConnection();
        if (!$pdo instanceof PDO) {
            throw new RuntimeException('Copy rows requires a PDO connection');
        }
        $buffering = (bool) $pdo->getAttribute(PDO::MYSQL_ATTR_USE_BUFFERED_QUERY);
        $depthVariable = $source->getDatabasePlatform() instanceof MySQLPlatform
            ? 'cte_max_recursion_depth'
            : 'max_recursive_iterations';
        $restoreDepth = null;
        try {
            if ($orderedTable !== null) {
                $currentDepth = filter_var($source->fetchOne("SELECT @@SESSION.$depthVariable"), FILTER_VALIDATE_INT);
                // A chain cannot exceed the number of rows. The last recursive
                // iteration confirms that the chain has no further children.
                $rows = filter_var($source->fetchOne("SELECT COUNT(*) FROM $orderedTable"), FILTER_VALIDATE_INT);
                if ($currentDepth === false || $rows === false) {
                    throw new RuntimeException('Expected numeric copy recursion limits');
                }
                $requiredDepth = min($rows, 4294967294) + 1;
                if ($requiredDepth > $currentDepth) {
                    $source->executeStatement("SET SESSION $depthVariable = $requiredDepth");
                    $restoreDepth = $currentDepth;
                }
            }
            $pdo->setAttribute(PDO::MYSQL_ATTR_USE_BUFFERED_QUERY, false);
            $result = $source->executeQuery($select);
            try {
                yield from $result->iterateNumeric();
            } finally {
                $result->free();
            }
        } finally {
            $pdo->setAttribute(PDO::MYSQL_ATTR_USE_BUFFERED_QUERY, $buffering);
            if ($restoreDepth !== null) {
                $source->executeStatement("SET SESSION $depthVariable = $restoreDepth");
            }
        }
    }
}
