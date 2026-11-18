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

    /**
     * @param array<int,string> $files Ordered by migration version.
     * @return array{applied:list<int>,pending:array<int,string>,hasHistory:bool,appliedAt:array<int,int>}
     */
    public function migrationState(array $files, bool $includeTimestamps = false): array
    {
        if ($files === []) {
            throw new MigrationError('The release has no migration files');
        }

        $relations = $this->relations();
        $hasHistory = in_array('goose_db_version', $relations, true);
        $history = $hasHistory ? $this->appliedHistory($includeTimestamps) : [];
        $applied = array_keys($history);
        $versions = array_keys($files);
        if ($applied !== array_slice($versions, 0, count($applied))) {
            throw new MigrationError('Database migration history does not match this release');
        }
        if ($applied === []) {
            $other = array_diff($relations, ['goose_db_version']);
            if ($other !== [] && $hasHistory && $this->connection->getDatabasePlatform() instanceof PostgreSQLPlatform) {
                // Goose's primary key sequence remains after the last migration is reverted.
                $sequence = $this->connection->fetchOne(
                    "SELECT relname FROM pg_catalog.pg_class WHERE oid = pg_get_serial_sequence('goose_db_version', 'id')::regclass",
                );
                if (is_string($sequence)) {
                    $other = array_diff($other, [$sequence]);
                }
            }
            if ($other !== []) {
                throw new MigrationError('An existing database is missing its migration history');
            }
        }

        return [
            'applied' => $applied,
            'pending' => array_slice($files, count($applied), preserve_keys: true),
            'hasHistory' => $hasHistory,
            'appliedAt' => $includeTimestamps ? $history : [],
        ];
    }

    /** @return list<int> */
    public function appliedVersions(): array
    {
        return array_keys($this->appliedHistory(includeTimestamps: false));
    }

    /** @return array<int,int> Applied versions, with Unix timestamps when requested. */
    private function appliedHistory(bool $includeTimestamps): array
    {
        $epoch = '0';
        if ($includeTimestamps) {
            $epoch = $this->connection->getDatabasePlatform() instanceof PostgreSQLPlatform
                ? 'FLOOR(EXTRACT(EPOCH FROM tstamp))'
                : 'UNIX_TIMESTAMP(tstamp)';
        }
        $rows = $this->connection->fetchAllAssociative(
            "SELECT version_id, is_applied, $epoch AS applied_at FROM goose_db_version WHERE version_id <> 0 ORDER BY id DESC",
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
            if (!array_key_exists($version, $latest)) {
                if (!in_array($row['is_applied'], [true, 1, '1', 't', 'true'], true)) {
                    $latest[$version] = null;
                    continue;
                }
                $timestamp = $row['applied_at'];
                if (!is_numeric($timestamp)) {
                    throw new MigrationError('Invalid applied migration timestamp');
                }
                $latest[$version] = (int) $timestamp;
            }
        }

        ksort($latest, SORT_NUMERIC);

        return array_filter($latest, static fn(?int $time): bool => $time !== null);
    }
}
