<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use InvalidArgumentException;

final readonly class DatabaseCopy
{
    public function __construct(private string $postgresMigrations, private string $mysqlMigrations) {}

    public function copy(
        #[\SensitiveParameter]
        string $sourceURL,
        #[\SensitiveParameter]
        string $targetURL,
    ): int {
        try {
            $source = Database::connect($sourceURL, dedicated: true);
            $target = Database::connect($targetURL, dedicated: true);
        } catch (InvalidArgumentException) {
            throw new CopyError('Database URLs must name PostgreSQL, MySQL or MariaDB.');
        }

        try {
            $fromPostgres = $source->getDatabasePlatform() instanceof PostgreSQLPlatform;
            if ($fromPostgres === ($target->getDatabasePlatform() instanceof PostgreSQLPlatform)) {
                throw new CopyError('Copy requires PostgreSQL on one side and MySQL or MariaDB on the other.');
            }
            if (!Database::lockMigrations($source) || !Database::lockMigrations($target)) {
                throw new CopyError('A migration or copy is already running on the source or target.');
            }

            if ($fromPostgres) {
                // A serializable InnoDB transaction locks even the empty
                // ranges checked below, keeping concurrent writes out.
                $target->executeStatement('SET SESSION foreign_key_checks = 1, unique_checks = 1');
                $target->executeStatement("SET SESSION sql_mode = CONCAT_WS(',', NULLIF(@@sql_mode, ''), 'STRICT_ALL_TABLES')");
                $target->executeStatement('SET TRANSACTION ISOLATION LEVEL SERIALIZABLE');
            }

            return $target->transactional(fn(): int => Database::inReadSnapshot(
                $source,
                fn(): int => $this->transfer($source, $target, $fromPostgres),
            ));
        } finally {
            $target->close();
            $source->close();
        }
    }

    private function transfer(Connection $source, Connection $target, bool $fromPostgres): int
    {
        $postgres = $fromPostgres ? $source : $target;
        $mysql = $fromPostgres ? $target : $source;
        $pgVersions = Schema::migrationVersions($this->postgresMigrations);
        if ($pgVersions === [] || $pgVersions !== Schema::migrationVersions($this->mysqlMigrations)) {
            throw new CopyError('The release migration sets differ.');
        }

        foreach ([$source, $target] as $db) {
            $schema = new Schema($db);
            if (!in_array('goose_db_version', $schema->relations(), true) || $schema->appliedVersions() !== $pgVersions) {
                throw new CopyError('Migrate both databases to the current release before copying.');
            }
        }

        $columns = CopyCatalog::columns($postgres);
        CopyCatalog::checkColumns($columns, CopyCatalog::columns($mysql));
        $keys = CopyCatalog::references($postgres);
        if ($keys !== CopyCatalog::references($mysql)) {
            throw new CopyError('The source and target foreign keys differ.');
        }
        $tables = CopyCatalog::order($columns, $keys);
        // Readiness and monitoring counters describe the local database and its traffic.
        unset($tables['http_readiness_cache'], $tables['php_metrics']);

        if (!$fromPostgres) {
            $names = array_map($target->quoteSingleIdentifier(...), array_keys($tables));
            $target->executeStatement('LOCK TABLE ' . implode(', ', $names) . ' IN SHARE ROW EXCLUSIVE MODE');
        }
        foreach ($tables as $table => $_reference) {
            if ($table === 'audit_actions') {
                continue;
            }
            $name = $target->quoteSingleIdentifier($table);
            if ($target->fetchOne("SELECT 1 FROM $name LIMIT 1" . ($fromPostgres ? ' FOR UPDATE' : '')) !== false) {
                throw new CopyError('The target database already contains data in ' . $table . '.');
            }
        }

        $rows = 0;
        foreach ($tables as $table => $reference) {
            if ($table === 'audit_actions') {
                $rows += self::copyAuditActions($source, $target, $fromPostgres);
                continue;
            }

            $name = $source->quoteSingleIdentifier($table);
            $expected = $source->fetchOne("SELECT COUNT(*) FROM $name");
            $types = array_map(static fn(array $column): string => $column['type'], $columns[$table]);
            $copied = CopyRows::copy($source, $target, $table, $types, $reference);
            if (!is_numeric($expected) || $copied !== (int) $expected) {
                throw new CopyError('Could not copy every row in ' . $table . '. The target was rolled back.');
            }
            $rows += $copied;
        }

        if ((new IntegrityStore($target))->findings() !== []) {
            throw new CopyError('Database integrity checks found inconsistencies in the copy. The target was rolled back. Run db check on the source.');
        }

        return $rows;
    }

    /** Merge the lookup values installed by the migrations into the data copy. */
    private static function copyAuditActions(Connection $source, Connection $target, bool $toMysql): int
    {
        $select = 'SELECT action FROM audit_actions';
        $actions = array_values(array_filter($source->fetchFirstColumn($select), is_string(...)));
        $seed = array_values(array_filter($target->fetchFirstColumn($select . ($toMysql ? ' FOR UPDATE' : '')), is_string(...)));
        if (array_diff($seed, $actions) !== []) {
            throw new CopyError('The target has audit actions absent from the source.');
        }

        $missing = array_diff($actions, $seed);
        foreach ($missing as $action) {
            $target->executeStatement('INSERT INTO audit_actions (action) VALUES (?)', [$action]);
        }

        return count($missing);
    }
}
