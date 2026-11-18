<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use PHPUnit\Framework\Attributes\Group;
use PHPUnit\Framework\TestCase;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\Migrator;
use StudioSchedule\Store\Schema;

final class SchemaEquivalenceTest extends TestCase
{
    private const MYSQL_MIGRATIONS = __DIR__ . '/../migrations/mysql';

    public function testEveryPostgresMigrationHasAMySqlTwin(): void
    {
        $postgres = Schema::migrationVersions(self::postgresMigrations());
        self::assertNotEmpty($postgres);
        self::assertSame($postgres, Schema::migrationVersions(self::MYSQL_MIGRATIONS));
    }

    #[Group('schema-equivalence')]
    public function testMigratedSchemasMatch(): void
    {
        $postgresURL = getenv('STUDIO_PHP_TEST_POSTGRES_URL');
        $mysqlURL = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($postgresURL === false || $mysqlURL === false) {
            self::markTestSkipped('Requires both of the disposable databases from just php-test');
        }

        $postgres = Database::connect($postgresURL);
        $mysql = Database::connect($mysqlURL);
        try {
            $postgresSchema = new Schema($postgres);
            $mysqlSchema = new Schema($mysql);
            $versions = Schema::migrationVersions(self::postgresMigrations());
            self::assertSame($versions, Schema::migrationVersions(self::MYSQL_MIGRATIONS));
            self::assertSame([], $mysqlSchema->relations(), 'The comparison requires an empty target database');
            if ($postgresSchema->relations() === []) {
                self::assertSame($versions, (new Migrator($postgresURL))->up(self::postgresMigrations()));
            }
            self::assertSame($versions, $postgresSchema->appliedVersions());
            self::assertSame($versions, (new Migrator($mysqlURL))->up(self::MYSQL_MIGRATIONS));
            self::assertSame($versions, $mysqlSchema->appliedVersions());

            $reference = SchemaCatalog::read($postgres, postgres: true);
            $twin = SchemaCatalog::read($mysql, postgres: false);
            $this->compareColumns($reference, $twin);
            $this->compareIndexes($reference, $twin);
            self::assertSame($reference->foreignKeys, $twin->foreignKeys);
            $this->compareChecks($reference, $twin);
            (new SchemaInvariantChecks($postgres, postgres: true))->verify();
            (new SchemaInvariantChecks($mysql, postgres: false))->verify();
        } finally {
            $mysql->close();
            $postgres->close();
        }
    }

    private static function postgresMigrations(): string
    {
        $directory = getenv('STUDIO_PHP_TEST_POSTGRES_MIGRATIONS');
        return $directory === false ? __DIR__ . '/../../internal/store/postgres/migrations' : $directory;
    }

    private function compareColumns(SchemaCatalog $reference, SchemaCatalog $twin): void
    {
        self::assertSame(array_keys($reference->columns), array_keys($twin->columns), 'Tables');
        foreach ($reference->columns as $table => $columns) {
            self::assertSame(array_keys($columns), array_keys($twin->columns[$table]), "Columns in $table");
            foreach ($columns as $name => $column) {
                $actual = $twin->columns[$table][$name];
                $message = "$table.$name ({$column['type']} compared with {$actual['type']})";
                self::assertSame($column['nullable'], $actual['nullable'], $message);
                self::assertContains($actual['type'], match ($column['type']) {
                    'boolean' => ['tinyint'],
                    'uuid' => ['binary'],
                    'smallint' => ['smallint'],
                    'integer' => ['int'],
                    'bigint', 'interval' => ['bigint'],
                    'time without time zone' => ['time'],
                    'timestamp with time zone' => ['datetime'],
                    'character varying', 'text' => ['varchar', 'char', 'varbinary', 'text', 'mediumtext', 'longtext'],
                    'bytea' => ['binary', 'varbinary', 'blob', 'mediumblob', 'longblob'],
                    'json', 'jsonb' => ['json', 'longtext'],
                    default => [$column['type']],
                }, $message);
                if ($column['type'] === 'uuid') {
                    self::assertSame('16', $actual['width'], $message);
                }
                if (in_array($column['type'], ['time without time zone', 'timestamp with time zone'], true)) {
                    self::assertSame('6', $actual['fractional_precision'], $message);
                }
                if (in_array($column['type'], ['json', 'jsonb'], true) && $actual['type'] === 'longtext') {
                    self::assertMatchesRegularExpression(
                        '/json_valid\s*\(\s*' . preg_quote($name, '/') . '\s*\)/i',
                        str_replace(chr(96), '', implode(' ', $twin->checks[$table] ?? [])),
                        $message,
                    );
                }
            }
        }
    }

    private function compareIndexes(SchemaCatalog $reference, SchemaCatalog $twin): void
    {
        foreach ($reference->indexes as $table => $indexes) {
            $twinIndexes = $twin->indexes[$table] ?? [];
            foreach ($indexes as $name => $index) {
                $message = "$table.$name";
                self::assertArrayHasKey($name, $twinIndexes, $message);
                $actual = $twinIndexes[$name];
                self::assertSame($index['unique'], $actual['unique'], $message);
                if ($index['unique']) {
                    self::assertFalse($actual['prefix'], "$message must index the full key");
                }
                if ($table === 'outbox_mails' && $name === 'outbox_mails_failed_idx') {
                    // The MySQL index starts with the original partial predicate's columns.
                    self::assertSame(['sent_at', 'next_attempt_at', ...$index['members']], $actual['members'], $message);
                    continue;
                }
                self::assertCount(count($index['members']), $actual['members'], $message);
                $generatedInputs = [];
                foreach ($index['members'] as $position => $member) {
                    $actualMember = $actual['members'][$position];
                    if ($member === $actualMember) {
                        continue;
                    }
                    $expression = $twin->generated[$table][$actualMember] ?? '';
                    $inputs = $twin->referencedColumns($table, $expression);
                    $expectedInputs = $reference->referencedColumns($table, $member);
                    self::assertNotSame([], $expectedInputs, $message);
                    self::assertSame([], array_diff($expectedInputs, $inputs), "$message.$actualMember");
                    if (str_starts_with($member, 'lower(')) {
                        self::assertStringContainsString('replace(', strtolower($expression), $message);
                    }
                    $generatedInputs = array_merge($generatedInputs, $inputs);
                }
                if ($index['unique'] && $index['predicate'] !== '') {
                    self::assertNotSame([], $generatedInputs, "$message must implement the partial predicate");
                    self::assertSame([], array_diff(
                        $reference->referencedColumns($table, $index['predicate']),
                        $generatedInputs,
                    ), "$message predicate");
                }
            }

            foreach ($twinIndexes as $name => $index) {
                if ($index['unique']) {
                    self::assertArrayHasKey($name, $indexes, "Extra unique key $table.$name");
                }
            }
        }
    }

    private function compareChecks(SchemaCatalog $reference, SchemaCatalog $twin): void
    {
        foreach ($reference->checks as $table => $checks) {
            $expected = $reference->checkColumnGroups($table, $checks);
            $twinChecks = $twin->checks[$table] ?? [];
            $actual = $twin->checkColumnGroups($table, $twinChecks);
            $columns = array_keys($reference->columns[$table]);
            $values = SchemaCatalog::checkValues($twinChecks, $columns);
            foreach (SchemaCatalog::checkValues($checks, $columns) as $predicate) {
                self::assertContains($predicate, $values, "$table CHECK $predicate");
            }
            foreach ($expected as $columns => $count) {
                if ($table === 'lessons' && $columns === 'cancel_note,canceled_at,canceled_by,status') {
                    // MySQL uses a CHECK and row triggers because canceled_by is ON DELETE SET NULL.
                    $columns = 'cancel_note,canceled_at,status';
                }
                self::assertGreaterThanOrEqual($count, $actual[$columns] ?? 0, "$table CHECK on $columns");
            }
        }
    }
}
