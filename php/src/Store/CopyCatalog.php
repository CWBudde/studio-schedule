<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;

/** @phpstan-type Column array{type:string,nullable:bool,width:?int,precision:?int} */
final class CopyCatalog
{
    /** @return array<string,array<string,Column>> */
    public static function columns(Connection $db): array
    {
        $postgres = $db->getDatabasePlatform() instanceof PostgreSQLPlatform;
        $schema = $postgres ? 'current_schema()' : 'DATABASE()';
        $generated = $postgres ? "c.is_generated = 'NEVER'" : "COALESCE(c.generation_expression, '') = ''";
        $engine = $postgres ? "'InnoDB'" : 't.engine';
        $rows = $db->fetchAllAssociative(
            "SELECT c.table_name AS table_name, c.column_name AS column_name, c.data_type AS data_type,
                    c.is_nullable AS is_nullable, c.character_maximum_length AS width,
                    c.datetime_precision AS fractional_precision, $engine AS engine
             FROM information_schema.columns c
             JOIN information_schema.tables t ON t.table_schema = c.table_schema AND t.table_name = c.table_name
             WHERE c.table_schema = $schema AND c.table_name <> 'goose_db_version'
               AND t.table_type = 'BASE TABLE' AND $generated",
        );
        $tables = [];
        foreach ($rows as $row) {
            $table = $row['table_name'];
            $column = $row['column_name'];
            $type = $row['data_type'];
            $width = $row['width'];
            $precision = $row['fractional_precision'];
            if ($row['engine'] !== 'InnoDB' || !is_string($table) || !is_string($column) || !is_string($type)) {
                throw new CopyError('The database contains an unsupported table or column.');
            }
            $tables[$table][$column] = ['type' => $type, 'nullable' => $row['is_nullable'] === 'YES',
                'width' => is_numeric($width) ? (int) $width : null,
                'precision' => is_numeric($precision) ? (int) $precision : null];
        }
        ksort($tables);
        foreach ($tables as &$columns) {
            ksort($columns);
        }

        return $tables;
    }

    /**
     * @param array<string,array<string,Column>> $postgres
     * @param array<string,array<string,Column>> $mysql
     */
    public static function checkColumns(array $postgres, array $mysql): void
    {
        if ($postgres === [] || array_keys($postgres) !== array_keys($mysql)) {
            throw new CopyError('The source and target table lists differ.');
        }
        foreach ($postgres as $table => $columns) {
            if (array_keys($columns) !== array_keys($mysql[$table])) {
                throw new CopyError('The source and target column lists differ in ' . $table . '.');
            }
            foreach ($columns as $name => $column) {
                $twin = $mysql[$table][$name];
                $types = match ($column['type']) {
                    'uuid' => $twin['width'] === 16 ? ['binary'] : [],
                    'boolean' => ['tinyint'],
                    'json', 'jsonb' => ['json', 'longtext'],
                    'interval' => ['bigint'],
                    'timestamp with time zone' => ['datetime'],
                    'time without time zone' => ['time'],
                    'character varying', 'character', 'text' => ['varchar', 'char', 'varbinary', 'text', 'mediumtext', 'longtext'],
                    'bytea' => ['binary', 'varbinary', 'blob', 'mediumblob', 'longblob', 'tinyblob'],
                    'integer' => ['int'],
                    default => [$column['type']],
                };
                if ($column['nullable'] !== $twin['nullable']
                    || (in_array($column['type'], ['time without time zone', 'timestamp with time zone'], true)
                        && $column['precision'] !== $twin['precision'])
                    || !in_array($twin['type'], $types, true)
                ) {
                    throw new CopyError("Incompatible column in source and target schemas: $table.$name.");
                }
            }
        }
    }

    /** @return list<array{table:string,local:string,parent:string,key:string}> */
    public static function references(Connection $db): array
    {
        $postgres = $db->getDatabasePlatform() instanceof PostgreSQLPlatform;
        $keys = [];
        foreach ($db->fetchAllAssociative($postgres ? self::POSTGRES_KEYS : self::MYSQL_KEYS) as $row) {
            $table = $row['table_name'];
            $local = $row['local_name'];
            $parent = $row['parent_table'];
            $key = $row['parent_key'];
            if (!is_string($table) || !is_string($local) || !is_string($parent) || !is_string($key)
                || $row['source_schema'] !== $row['parent_schema']
            ) {
                throw new CopyError('Copy requires foreign keys within the application schema.');
            }
            $keys[] = ['table' => $table, 'local' => $local, 'parent' => $parent, 'key' => $key];
        }
        sort($keys);

        return $keys;
    }

    /**
     * @param array<string,array<string,Column>> $columns
     * @param list<array{table:string,local:string,parent:string,key:string}> $keys
     * @return array<string,array{local:string,key:string}|null>
     */
    public static function order(array $columns, array $keys): array
    {
        $parents = array_fill_keys(array_keys($columns), []);
        $self = [];
        foreach ($keys as ['table' => $table, 'local' => $local, 'parent' => $parent, 'key' => $key]) {
            if (!isset($columns[$table][$local], $columns[$parent][$key])) {
                throw new CopyError('A foreign key references a column outside the data copy.');
            }
            if ($parent === $table) {
                if (isset($self[$table]) || !$columns[$table][$local]['nullable']) {
                    throw new CopyError('Unsupported self-reference in ' . $table . '.');
                }
                $self[$table] = ['local' => $local, 'key' => $key];
            } else {
                $parents[$table][$parent] = true;
            }
        }

        $ordered = [];
        while ($parents !== []) {
            $before = count($ordered);
            foreach ($parents as $table => $needs) {
                if (array_diff_key($needs, $ordered) === []) {
                    $ordered[$table] = $self[$table] ?? null;
                    unset($parents[$table]);
                }
            }
            if (count($ordered) === $before) {
                throw new CopyError('The database schema has cyclic foreign keys between tables.');
            }
        }

        return $ordered;
    }

    private const string POSTGRES_KEYS = <<<'SQL'
        SELECT t.relname AS table_name, a.attname AS local_name, p.relname AS parent_table,
            b.attname AS parent_key, n.nspname AS source_schema, pn.nspname AS parent_schema
        FROM pg_catalog.pg_constraint c
        JOIN pg_catalog.pg_class t ON t.oid = c.conrelid
        JOIN pg_catalog.pg_class p ON p.oid = c.confrelid
        JOIN pg_catalog.pg_namespace n ON n.oid = t.relnamespace
        JOIN pg_catalog.pg_namespace pn ON pn.oid = p.relnamespace
        CROSS JOIN LATERAL generate_subscripts(c.conkey, 1) AS k(position)
        JOIN pg_catalog.pg_attribute a ON a.attrelid = t.oid AND a.attnum = c.conkey[k.position]
        JOIN pg_catalog.pg_attribute b ON b.attrelid = p.oid AND b.attnum = c.confkey[k.position]
        WHERE c.contype = 'f' AND n.nspname = current_schema()
        SQL;

    private const string MYSQL_KEYS = <<<'SQL'
        SELECT table_name AS table_name, column_name AS local_name, referenced_table_name AS parent_table,
            referenced_column_name AS parent_key, table_schema AS source_schema, referenced_table_schema AS parent_schema
        FROM information_schema.key_column_usage
        WHERE table_schema = DATABASE() AND referenced_table_name IS NOT NULL
        SQL;
}
