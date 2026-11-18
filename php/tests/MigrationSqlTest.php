<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use StudioSchedule\Store\MigrationSql;
use StudioSchedule\Store\Schema;
use UnexpectedValueException;

final class MigrationSqlTest extends TestCase
{
    public function testReadsUpOnlyAndKeepsCompoundStatementsTogether(): void
    {
        $file = $this->temporaryFile();
        file_put_contents($file, <<<'SQL'
            -- File comment
            -- +goose Up
            CREATE TABLE examples (id INTEGER PRIMARY KEY);

            -- +goose StatementBegin
            CREATE TRIGGER example_trigger BEFORE INSERT ON examples FOR EACH ROW
            BEGIN
                SET NEW.id = 1;
            END;
            -- +goose StatementEnd
            -- +goose Down
            DROP TABLE examples;
            SQL);
        self::assertSame([
            "CREATE TABLE examples (id INTEGER PRIMARY KEY);\n",
            "CREATE TRIGGER example_trigger BEFORE INSERT ON examples FOR EACH ROW\nBEGIN\n    SET NEW.id = 1;\nEND;\n",
        ], MigrationSql::up($file));
    }

    public function testTrailingCommentsAndMultilineContentMatchGoose(): void
    {
        $file = $this->temporaryFile();
        file_put_contents($file, <<<'SQL'
            -- +goose Up
            INSERT INTO sample VALUES ('first

            -- part of the value
            last'); -- SQL comment
            SELECT 1;
            SQL);
        self::assertSame([
            "INSERT INTO sample VALUES ('first\n\n-- part of the value\nlast'); -- SQL comment\n",
            "SELECT 1;\n",
        ], MigrationSql::up($file));
    }

    public function testMissingFileReportsOnlyItsName(): void
    {
        $file = $this->temporaryFile();
        unlink($file);
        $this->expectException(UnexpectedValueException::class);
        $this->expectExceptionMessage('Could not read migration ' . basename($file));
        MigrationSql::up($file);
    }

    #[DataProvider('malformedFiles')]
    public function testRejectsMalformedUpSections(string $contents): void
    {
        $file = $this->temporaryFile();
        file_put_contents($file, $contents);
        $this->expectException(UnexpectedValueException::class);
        $this->expectExceptionMessage('Invalid Up section');
        MigrationSql::up($file);
    }

    /** @return array<string, array{string}> */
    public static function malformedFiles(): array
    {
        return [
            'missing up' => ['SELECT 1;'],
            'empty up' => ["-- +goose Up\n-- +goose Down"],
            'duplicate up' => ["-- +goose Up\n-- +goose Up\nSELECT 1;"],
            'unknown directive' => ["-- +goose NO TRANSACTION\n-- +goose Up\nSELECT 1;"],
            'down inside statement' => ["-- +goose Up\nSELECT 1\n-- +goose Down"],
            'begin before up' => ["-- +goose StatementBegin\nSELECT 1;\n-- +goose StatementEnd"],
            'nested begin' => ["-- +goose Up\n-- +goose StatementBegin\n-- +goose StatementBegin"],
            'unclosed block' => ["-- +goose Up\n-- +goose StatementBegin\nSELECT 1;"],
            'empty block' => ["-- +goose Up\n-- +goose StatementBegin\n-- +goose StatementEnd"],
            'unterminated statement' => ["-- +goose Up\nSELECT 1"],
        ];
    }

    public function testDuplicateMigrationVersionsAreRejected(): void
    {
        $file = $this->temporaryFile();
        unlink($file);
        self::assertTrue(mkdir($file));
        touch($file . '/00001_a.sql');
        touch($file . '/00001_b.sql');
        try {
            $this->expectException(UnexpectedValueException::class);
            $this->expectExceptionMessage('duplicate migration version');
            Schema::migrationVersions($file);
        } finally {
            unlink($file . '/00001_a.sql');
            unlink($file . '/00001_b.sql');
            rmdir($file);
        }
    }

    private function temporaryFile(): string
    {
        $file = tempnam(sys_get_temp_dir(), 'studio-migration-');
        self::assertIsString($file);
        $this->files[] = $file;

        return $file;
    }

    /** @var list<string> */
    private array $files = [];

    protected function tearDown(): void
    {
        foreach ($this->files as $file) {
            if (is_file($file)) {
                unlink($file);
            }
        }
    }
}
