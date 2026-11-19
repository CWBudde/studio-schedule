<?php

declare(strict_types=1);

namespace StudioSchedule\Console;

use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use StudioSchedule\Store\Schema;

final readonly class CurrentSchema
{
    public function __construct(private string $postgresMigrations, private string $mysqlMigrations) {}

    public function requireCurrent(Connection $database): void
    {
        $expected = Schema::migrationVersions(
            $database->getDatabasePlatform() instanceof PostgreSQLPlatform ? $this->postgresMigrations : $this->mysqlMigrations,
        );
        if ($expected === []) {
            throw new CommandError('the release has no migration files');
        }
        $schema = new Schema($database);
        $applied = in_array('goose_db_version', $schema->relations(), true) ? $schema->appliedVersions() : [];
        if (array_diff($applied, $expected) !== []) {
            throw new CommandError('the database schema is newer than this binary: run a newer studio-schedule');
        }
        if ($applied !== $expected) {
            throw new CommandError('the database schema is not up to date: run studio-schedule migrate up');
        }
    }
}
