<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\MariaDBPlatform;

/**
 * Reads the migrated databases' catalogs. Keys, column checks and foreign
 * key actions are compared independently of each engine's DDL formatting.
 */
final class SchemaCatalog
{
    /** @var array<string, array<string, array<string, string>>> */
    public array $columns = [];

    /** @var array<string, array<string, string>> */
    public array $generated = [];

    /** @var array<string, array<string, array{unique:bool, members:list<string>, predicate:string, prefix:bool}>> */
    public array $indexes = [];

    /** @var list<array<string, string>> */
    public array $foreignKeys = [];

    /** @var array<string, list<string>> */
    public array $checks = [];

    public static function read(Connection $connection, bool $postgres): self
    {
        $catalog = new self();
        foreach (self::rows($connection, $postgres ? self::POSTGRES_COLUMNS : self::MYSQL_COLUMNS) as $row) {
            if ($row['generation'] === '') {
                $catalog->columns[$row['table_name']][$row['column_name']] = $row;
            } else {
                $catalog->generated[$row['table_name']][$row['column_name']] = $row['generation'];
            }
        }
        foreach (self::rows($connection, $postgres ? self::POSTGRES_INDEXES : self::MYSQL_INDEXES) as $row) {
            $table = $row['table_name'];
            $index = $row['index_name'];
            $catalog->indexes[$table][$index] ??= [
                'unique' => $row['unique_index'] === '1',
                'members' => [],
                'predicate' => $row['predicate'],
                'prefix' => false,
            ];
            $catalog->indexes[$table][$index]['members'][] = $row['member'];
            if ($row['prefix_length'] !== '') {
                $catalog->indexes[$table][$index]['prefix'] = true;
            }
        }
        $catalog->foreignKeys = self::rows($connection, $postgres ? self::POSTGRES_FOREIGN_KEYS : self::MYSQL_FOREIGN_KEYS);
        $checkQuery = match (true) {
            $postgres => self::POSTGRES_CHECKS,
            $connection->getDatabasePlatform() instanceof MariaDBPlatform => self::MARIADB_CHECKS,
            default => self::MYSQL_CHECKS,
        };
        foreach (self::rows($connection, $checkQuery) as $row) {
            $catalog->checks[$row['table_name']][] = $row['check_clause'];
        }

        return $catalog;
    }

    /** @return list<string> */
    public function referencedColumns(string $table, string $expression): array
    {
        $withoutLiterals = preg_replace("/'(?:[^']|'')*'/", '', $expression) ?? $expression;
        preg_match_all('/[a-z_][a-z0-9_]*/i', strtolower($withoutLiterals), $matches);
        $columns = [];
        foreach ($matches[0] as $field) {
            if (isset($this->columns[$table][$field])) {
                $columns[$field] = true;
            }
            if (isset($this->generated[$table][$field])) {
                foreach ($this->referencedColumns($table, $this->generated[$table][$field]) as $column) {
                    $columns[$column] = true;
                }
            }
        }
        $result = array_keys($columns);
        sort($result, SORT_STRING);

        return $result;
    }

    /**
     * @param list<string> $checks
     * @return array<string, int>
     */
    public function checkColumnGroups(string $table, array $checks): array
    {
        $groups = [];
        foreach ($checks as $expression) {
            $columns = implode(',', $this->referencedColumns($table, $expression));
            $groups[$columns] = ($groups[$columns] ?? 0) + 1;
        }

        return $groups;
    }

    /**
     * @param list<string> $checks
     * @param list<string> $columns
     * @return list<string>
     */
    public static function checkValues(array $checks, array $columns): array
    {
        $values = [];
        foreach ($checks as $expression) {
            // Keep literal case. Metadata includes dialect-specific quoting and casts.
            $expression = str_replace(["\\'", chr(96), '"'], ["'", '', ''], $expression);
            $expression = preg_replace("/_[a-z0-9]+(?=')|::text\b/i", '', $expression) ?? $expression;
            $expression = preg_replace_callback(
                "/'(?:(\d+) days|(\d+):(\d+):(\d+))'::interval/i",
                static function (array $parts): string {
                    return (string) (((int) $parts[1] * 86400
                        + (int) ($parts[2] ?? 0) * 3600
                        + (int) ($parts[3] ?? 0) * 60
                        + (int) ($parts[4] ?? 0)) * 1000000);
                },
                $expression,
            ) ?? $expression;
            $expression = trim(str_replace(['(', ')', '[', ']'], '', $expression));

            if (preg_match('/^(\w+)\s+(?:=\s*ANY\s*ARRAY|IN)\s*((?:\'[^\']*\'\s*,?\s*)+)$/i', $expression, $match) === 1) {
                preg_match_all("/'[^']*'/", $match[2], $literals);
                $members = $literals[0];
                sort($members, SORT_STRING);
                $values[] = $match[1] . ' IN ' . implode(',', $members);
                continue;
            }
            if (preg_match('/^(\w+)\s+BETWEEN\s+(-?\d+)\s+AND\s+(-?\d+)$/i', $expression, $match) === 1) {
                $expression = "$match[1] >= $match[2] AND $match[1] <= $match[3]";
            }
            if (preg_match('/^(\w+)\s*(>=|<=|<>|=|>|<)\s*(-?\d+)(?:\s+AND\s+\1\s*(>=|<=|<>|=|>|<)\s*(-?\d+))?$/i', $expression, $match) === 1
                && in_array($match[1], $columns, true)) {
                $values[] = "$match[1] $match[2] $match[3]";
                if (isset($match[4], $match[5])) {
                    $values[] = "$match[1] $match[4] $match[5]";
                }
            }
        }

        return $values;
    }

    /** @return list<array<string, string>> */
    private static function rows(Connection $connection, string $sql): array
    {
        return array_map(
            static fn(array $row): array => array_map(
                static fn(mixed $value): string => is_scalar($value) ? (string) $value : '',
                $row,
            ),
            $connection->fetchAllAssociative($sql),
        );
    }

    private const POSTGRES_COLUMNS = <<<'SQL'
        SELECT table_name, column_name, data_type AS type, is_nullable AS nullable,
            character_maximum_length AS width, datetime_precision AS fractional_precision, '' AS generation
        FROM information_schema.columns
        WHERE table_schema = current_schema() AND table_name <> 'goose_db_version'
        ORDER BY table_name COLLATE "C", column_name COLLATE "C"
        SQL;

    private const MYSQL_COLUMNS = <<<'SQL'
        SELECT table_name AS table_name, column_name AS column_name, data_type AS type, is_nullable AS nullable,
            character_maximum_length AS width, datetime_precision AS fractional_precision, generation_expression AS generation
        FROM information_schema.columns
        WHERE table_schema = DATABASE() AND table_name <> 'goose_db_version'
        ORDER BY CAST(table_name AS BINARY), CAST(column_name AS BINARY)
        SQL;

    private const POSTGRES_INDEXES = <<<'SQL'
        SELECT t.relname AS table_name, CASE WHEN x.indisprimary THEN 'PRIMARY' ELSE i.relname END AS index_name,
            CASE WHEN x.indisunique THEN 1 ELSE 0 END AS unique_index,
            CASE WHEN a.attname IS NULL THEN pg_get_indexdef(i.oid, k.position, TRUE) ELSE a.attname END AS member,
            pg_get_expr(x.indpred, t.oid) AS predicate, NULL AS prefix_length
        FROM pg_catalog.pg_index x
        JOIN pg_catalog.pg_class t ON t.oid = x.indrelid
        JOIN pg_catalog.pg_class i ON i.oid = x.indexrelid
        JOIN pg_catalog.pg_namespace n ON n.oid = t.relnamespace
        CROSS JOIN LATERAL generate_series(1, x.indnkeyatts) AS k(position)
        LEFT JOIN pg_catalog.pg_attribute a ON a.attrelid = t.oid AND a.attnum = x.indkey[k.position - 1]
        WHERE n.nspname = current_schema() AND t.relname <> 'goose_db_version'
        ORDER BY t.relname, index_name, k.position
        SQL;

    private const MYSQL_INDEXES = <<<'SQL'
        SELECT table_name AS table_name, index_name AS index_name,
            1 - non_unique AS unique_index, column_name AS member, '' AS predicate, sub_part AS prefix_length
        FROM information_schema.statistics
        WHERE table_schema = DATABASE() AND table_name <> 'goose_db_version'
        ORDER BY table_name, index_name, seq_in_index
        SQL;

    private const POSTGRES_FOREIGN_KEYS = <<<'SQL'
        SELECT t.relname AS table_name, c.conname AS name, a.attname AS member, p.relname AS parent_table,
            b.attname AS parent_member,
            CASE c.confdeltype WHEN 'c' THEN 'CASCADE' WHEN 'n' THEN 'SET NULL' ELSE 'RESTRICT' END AS delete_rule,
            CASE c.confupdtype WHEN 'c' THEN 'CASCADE' WHEN 'n' THEN 'SET NULL' ELSE 'RESTRICT' END AS update_rule
        FROM pg_catalog.pg_constraint c
        JOIN pg_catalog.pg_class t ON t.oid = c.conrelid
        JOIN pg_catalog.pg_class p ON p.oid = c.confrelid
        JOIN pg_catalog.pg_namespace n ON n.oid = t.relnamespace
        CROSS JOIN LATERAL generate_subscripts(c.conkey, 1) AS k(position)
        JOIN pg_catalog.pg_attribute a ON a.attrelid = t.oid AND a.attnum = c.conkey[k.position]
        JOIN pg_catalog.pg_attribute b ON b.attrelid = p.oid AND b.attnum = c.confkey[k.position]
        WHERE c.contype = 'f' AND n.nspname = current_schema()
        ORDER BY t.relname COLLATE "C", c.conname COLLATE "C", k.position
        SQL;

    private const MYSQL_FOREIGN_KEYS = <<<'SQL'
        SELECT k.table_name AS table_name, k.constraint_name AS name, k.column_name AS member,
            k.referenced_table_name AS parent_table, k.referenced_column_name AS parent_member,
            IF(r.delete_rule = 'NO ACTION', 'RESTRICT', r.delete_rule) AS delete_rule,
            IF(r.update_rule = 'NO ACTION', 'RESTRICT', r.update_rule) AS update_rule
        FROM information_schema.key_column_usage k
        JOIN information_schema.referential_constraints r ON r.constraint_schema = k.constraint_schema
            AND r.constraint_name = k.constraint_name AND r.table_name = k.table_name
        WHERE k.table_schema = DATABASE() AND k.referenced_table_name IS NOT NULL
        ORDER BY CAST(k.table_name AS BINARY), CAST(k.constraint_name AS BINARY), k.ordinal_position
        SQL;

    private const POSTGRES_CHECKS = <<<'SQL'
        SELECT t.relname AS table_name, pg_get_expr(c.conbin, t.oid) AS check_clause
        FROM pg_catalog.pg_constraint c
        JOIN pg_catalog.pg_class t ON t.oid = c.conrelid
        JOIN pg_catalog.pg_namespace n ON n.oid = t.relnamespace
        WHERE c.contype = 'c' AND n.nspname = current_schema() AND t.relname <> 'goose_db_version'
        SQL;

    private const MARIADB_CHECKS = <<<'SQL'
        SELECT table_name AS table_name, check_clause AS check_clause
        FROM information_schema.check_constraints
        WHERE constraint_schema = DATABASE() AND table_name <> 'goose_db_version'
        SQL;

    private const MYSQL_CHECKS = <<<'SQL'
        SELECT t.table_name AS table_name, c.check_clause AS check_clause
        FROM information_schema.table_constraints t
        JOIN information_schema.check_constraints c ON c.constraint_schema = t.constraint_schema
            AND c.constraint_name = t.constraint_name
        WHERE t.constraint_type = 'CHECK' AND t.enforced = 'YES' AND t.table_schema = DATABASE() AND t.table_name <> 'goose_db_version'
        SQL;
}
