<?php

declare(strict_types=1);

namespace StudioSchedule\Store;

use Doctrine\DBAL\Connection;

final readonly class Schema
{
    public function __construct(private Connection $connection) {}

    /** @return list<int> */
    public static function migrationVersions(string $directory): array
    {
        $versions = [];
        foreach (glob($directory . '/*.sql') ?: [] as $file) {
            if (preg_match('/^([0-9]{5})_.+\.sql$/D', basename($file), $match) === 1) {
                $versions[] = (int) $match[1];
            }
        }
        sort($versions, SORT_NUMERIC);

        return $versions;
    }

    /** @return list<int> */
    public function appliedVersions(): array
    {
        $rows = $this->connection->fetchAllAssociative(
            'SELECT version_id, is_applied FROM goose_db_version WHERE version_id > 0 ORDER BY id DESC',
        );
        $latest = [];
        foreach ($rows as $row) {
            $raw = $row['version_id'];
            if (!is_int($raw) && !is_string($raw)) {
                continue;
            }
            $version = (int) $raw;
            $latest[$version] ??= in_array($row['is_applied'], [true, 1, '1', 't', 'true'], true);
        }

        $versions = array_keys(array_filter($latest));
        sort($versions, SORT_NUMERIC);

        return $versions;
    }
}
