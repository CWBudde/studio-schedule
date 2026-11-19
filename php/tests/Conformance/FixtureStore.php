<?php

declare(strict_types=1);

namespace StudioSchedule\Tests\Conformance;

use DateTimeImmutable;
use DateTimeZone;
use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\PostgreSQLPlatform;
use InvalidArgumentException;
use RuntimeException;
use StudioSchedule\Domain\Date;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\DatabaseCopy;
use StudioSchedule\Store\Migrator;
use StudioSchedule\Store\Schema;
use Throwable;

/** Database operations for the disposable conformance fixture. */
final readonly class FixtureStore
{
    public function __construct(
        #[\SensitiveParameter]
        private string $targetURL,
        #[\SensitiveParameter]
        private ?string $seedPostgresURL,
        private string $fixtureDatabase,
        private string $goBinary,
        private string $postgresMigrations,
        private string $mysqlMigrations,
    ) {
        if (preg_match('/\Astudio_conformance_[0-9a-f]{16}\z/', $fixtureDatabase) !== 1) {
            throw new InvalidArgumentException('Invalid conformance fixture database name.');
        }
        $this->requireFixtureURL($targetURL);
        if (Database::driverForURL($targetURL) === 'pdo_pgsql') {
            if ($seedPostgresURL !== null) {
                throw new InvalidArgumentException('PostgreSQL fixtures seed directly into their target database.');
            }
        } else {
            if ($seedPostgresURL === null || Database::driverForURL($seedPostgresURL) !== 'pdo_pgsql') {
                throw new InvalidArgumentException('MySQL and MariaDB fixtures require a PostgreSQL seed database.');
            }
            $this->requireFixtureURL($seedPostgresURL);
        }
    }

    public function reset(): void
    {
        $this->resetDatabase($this->targetURL);
    }

    /** Returns false if the target contains application data. */
    public function seed(Date $anchor, string $school): bool
    {
        if ($anchor->isZero() || !in_array($school, ['programme', 'werkstatt'], true)) {
            throw new InvalidArgumentException('Invalid conformance fixture seed.');
        }

        $target = $this->open($this->targetURL);
        try {
            $schema = new Schema($target);
            $expectedVersions = Schema::migrationVersions($this->migrations($target));
            if ($expectedVersions === [] || $schema->appliedVersions() !== $expectedVersions) {
                throw new RuntimeException('The conformance fixture database must be reset before seeding.');
            }
            $database = $target->quoteSingleIdentifier($this->schemaName($target));
            foreach ($this->tables($target) as $table) {
                if (in_array($table, ['goose_db_version', 'audit_actions', 'http_readiness_cache', 'php_metrics'], true)) {
                    continue;
                }
                $name = $target->quoteSingleIdentifier($table);
                if ($target->fetchOne("SELECT 1 FROM $database.$name LIMIT 1") !== false) {
                    return false;
                }
            }
        } finally {
            $target->close();
        }

        $source = $this->seedPostgresURL;
        if ($source === null) {
            $this->runGoSeed($this->targetURL, $anchor, $school);
        } else {
            $this->resetDatabase($source);
            $this->runGoSeed($source, $anchor, $school);
            (new DatabaseCopy($this->postgresMigrations, $this->mysqlMigrations))->copy($source, $this->targetURL);
        }

        return true;
    }

    /** @return list<array{to:string,subject:string,text:string,template:string,createdAt:string,html?:string}> */
    public function outbox(): array
    {
        $db = $this->open($this->targetURL);
        try {
            $database = $db->quoteSingleIdentifier($this->schemaName($db));
            $rows = $db->fetchAllAssociative(
                "SELECT recipient, subject, body_text, body_html, template, created_at
                 FROM $database.outbox_mails
                 ORDER BY created_at DESC, id DESC
                 LIMIT 1000",
            );
        } finally {
            $db->close();
        }

        $utc = new DateTimeZone('UTC');
        $mails = [];
        foreach ($rows as $row) {
            $recipient = $row['recipient'];
            $subject = $row['subject'];
            $bodyText = $row['body_text'];
            $bodyHtml = $row['body_html'];
            $template = $row['template'];
            $createdAt = $row['created_at'];
            if (!is_string($recipient) || !is_string($subject) || !is_string($createdAt)
                || ($template !== null && !is_string($template))
                || ($bodyText !== null && !is_string($bodyText)) || ($bodyHtml !== null && !is_string($bodyHtml))
            ) {
                throw new RuntimeException('The conformance outbox returned an invalid mail.');
            }

            $created = (new DateTimeImmutable($createdAt, $utc))->setTimezone($utc);
            $fraction = rtrim($created->format('u'), '0');
            $mail = [
                'to' => $recipient, 'subject' => $subject, 'text' => $bodyText ?? '', 'template' => $template ?? '',
                'createdAt' => $created->format('Y-m-d\TH:i:s') . ($fraction === '' ? '' : '.' . $fraction) . 'Z',
            ];
            if ($bodyHtml !== null && $bodyHtml !== '') {
                $mail['html'] = $bodyHtml;
            }
            $mails[] = $mail;
        }

        return $mails;
    }

    private function resetDatabase(#[\SensitiveParameter] string $url): void
    {
        $db = $this->open($url);
        try {
            $directory = $this->migrations($db);
            if (Schema::migrationVersions($directory) === []) {
                throw new RuntimeException('No migrations are available for the conformance fixture.');
            }
            if (!Database::lockMigrations($db)) {
                throw new RuntimeException('A migration or copy is already running for the conformance fixture.');
            }
            if ($db->getDatabasePlatform() instanceof PostgreSQLPlatform) {
                $db->transactional(static function (Connection $db): void {
                    $db->executeStatement('DROP SCHEMA public CASCADE');
                    $db->executeStatement('CREATE SCHEMA public');
                });
            } else {
                $database = $db->quoteSingleIdentifier($this->fixtureDatabase);
                $tables = $this->tables($db);
                $db->executeStatement('SET SESSION FOREIGN_KEY_CHECKS = 0');
                try {
                    foreach ($tables as $table) {
                        $name = $db->quoteSingleIdentifier($table);
                        $db->executeStatement("DROP TABLE $database.$name");
                    }
                } finally {
                    $db->executeStatement('SET SESSION FOREIGN_KEY_CHECKS = 1');
                }
            }
        } finally {
            $db->close();
        }

        (new Migrator($url))->up($directory);
    }

    private function requireFixtureURL(#[\SensitiveParameter] string $url): void
    {
        $parts = parse_url($url);
        $driver = Database::driverForURL($url);
        if ($parts === false || $driver === null || ($parts['host'] ?? '') === ''
            || ($parts['path'] ?? '') !== '/' . $this->fixtureDatabase || isset($parts['fragment'])
            || (isset($parts['query']) && ($driver !== 'pdo_pgsql' || $parts['query'] !== 'sslmode=disable'))
        ) {
            throw new InvalidArgumentException('Expected the isolated conformance fixture database URL.');
        }
    }

    private function open(#[\SensitiveParameter] string $url): Connection
    {
        $db = Database::connect($url, dedicated: true);
        try {
            $postgres = $db->getDatabasePlatform() instanceof PostgreSQLPlatform;
            $name = $db->fetchOne($postgres ? 'SELECT pg_catalog.current_database()' : 'SELECT DATABASE()');
            if ($name !== $this->fixtureDatabase
                || ($postgres && $db->fetchOne('SELECT pg_catalog.current_schemas(false)::text') !== '{public}')
            ) {
                throw new RuntimeException('The connection is outside the expected conformance fixture namespace.');
            }

            return $db;
        } catch (Throwable $error) {
            $db->close();

            throw $error;
        }
    }

    /** @return list<string> */
    private function tables(Connection $db): array
    {
        $rows = $db->fetchFirstColumn(
            "SELECT table_name FROM information_schema.tables
             WHERE table_schema = ? AND table_type = 'BASE TABLE'
             ORDER BY table_name",
            [$this->schemaName($db)],
        );
        $tables = [];
        foreach ($rows as $name) {
            if (!is_string($name)) {
                throw new RuntimeException('The conformance fixture catalog returned an invalid table name.');
            }
            $tables[] = $name;
        }

        return $tables;
    }

    private function schemaName(Connection $db): string
    {
        return $db->getDatabasePlatform() instanceof PostgreSQLPlatform ? 'public' : $this->fixtureDatabase;
    }

    private function migrations(Connection $db): string
    {
        return $db->getDatabasePlatform() instanceof PostgreSQLPlatform ? $this->postgresMigrations : $this->mysqlMigrations;
    }

    private function runGoSeed(#[\SensitiveParameter] string $url, Date $anchor, string $school): void
    {
        $process = proc_open(
            [$this->goBinary, 'seed', '--anchor', (string) $anchor, '--school', $school],
            [0 => ['file', '/dev/null', 'r'], 1 => ['file', '/dev/null', 'w'], 2 => ['file', '/dev/null', 'w']],
            $pipes,
            null,
            ['DATABASE_URL' => $url],
        );
        if (!is_resource($process) || proc_close($process) !== 0) {
            throw new RuntimeException('The Go fixture seeder failed.');
        }
    }
}
