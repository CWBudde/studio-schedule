<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use UnexpectedValueException;

final readonly class Schema
{
    /** Reserved for unfinished MySQL and MariaDB migration batches. */
    public const UPGRADE_IN_PROGRESS = -1;

    public function __construct(private Connection $connection) {}

    /** @return list<int> */
    public static function migrationVersions(string $directory): array
    {
        return array_keys(self::migrationFiles($directory));
    }

    /** @return array<int, string> */
    public static function migrationFiles(string $directory): array
    {
        $files = [];
        foreach (glob($directory . '/*.sql') ?: [] as $file) {
            if (preg_match('/^([0-9]{5})_.+\.sql$/D', basename($file), $match) !== 1) {
                continue;
            }
            $version = (int) $match[1];
            if ($version === 0 || isset($files[$version])) {
                throw new UnexpectedValueException('Invalid or duplicate migration version in ' . basename($file));
            }
            $files[$version] = $file;
        }
        ksort($files, SORT_NUMERIC);

        return $files;
    }

    /** @return list<string> */
    public function relations(): array
    {
        $sql = $this->connection->getDatabasePlatform() instanceof PostgreSQLPlatform
            ? "SELECT c.relname FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace "
                . "WHERE n.nspname = current_schema() AND c.relkind IN ('r', 'p', 'v', 'm', 'f', 'S')"
            : 'SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE()';

        return array_values(array_filter($this->connection->fetchFirstColumn($sql), is_string(...)));
    }

    /** @return list<int> */
    public function appliedVersions(): array
    {
        $rows = $this->connection->fetchAllAssociative(
            'SELECT version_id, is_applied FROM goose_db_version WHERE version_id <> 0 ORDER BY id DESC',
        );
        $latest = [];
        foreach ($rows as $row) {
            $raw = $row['version_id'];
            if (!is_int($raw) && !is_string($raw)) {
                continue;
            }
            $version = (int) $raw;
            if ($version === self::UPGRADE_IN_PROGRESS) {
                throw new MigrationRecoveryRequired();
            }
            $latest[$version] ??= in_array($row['is_applied'], [true, 1, '1', 't', 'true'], true);
        }

        $versions = array_keys(array_filter($latest));
        sort($versions, SORT_NUMERIC);

        return $versions;
    }
}
