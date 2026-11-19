<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use Closure;
use Doctrine\DBAL\Connection;
use PHPUnit\Framework\Attributes\Group;
use PHPUnit\Framework\TestCase;
use StudioSchedule\Store\CopyError;
use StudioSchedule\Store\Database;
use StudioSchedule\Store\DatabaseCopy;

#[Group('database-copy')]
final class DatabaseCopyTest extends TestCase
{
    private const string PERSON = '017f1234567870008000000000000011';
    private const string MERGED = '017f1234567870008000000000000022';
    private const string ACCOUNT = '017f1234567870008000000000000033';
    private const string ACTION = 'copy-test-action';

    public function testCopyPreservesApplicationRowsAndRefusesToOverwrite(): void
    {
        $this->withDatabases(function (DatabaseCopy $copy, string $pgURL, string $myURL, Connection $pg, Connection $my): void {
            $pg->executeStatement("INSERT INTO persons (id, given_name, family_name) VALUES (?::uuid, 'Éva', 'Muster')", [self::PERSON]);
            $pg->executeStatement('INSERT INTO audit_actions (action) VALUES (?)', [self::ACTION]);
            $pg->executeStatement('UPDATE http_readiness_cache SET checked_at_us = 10');
            $my->executeStatement('UPDATE http_readiness_cache SET checked_at_us = 20');
            $observation = "INSERT INTO php_metrics (family, name, label, status, bucket, observations, duration_us, last_success_us)
                VALUES ('http', 'other', 'GET', 200, 0, ?, 100, 0)";
            $pg->executeStatement($observation, [3]);
            $my->executeStatement($observation, [7]);
            self::assertSame(2, $copy->copy($pgURL, $myURL));
            self::assertContains($my->fetchOne('SELECT checked_at_us FROM http_readiness_cache'), [20, '20']);
            self::assertContains($my->fetchOne('SELECT observations FROM php_metrics'), [7, '7']);
            self::assertSame('Éva', $my->fetchOne('SELECT given_name FROM persons WHERE id = UNHEX(?)', [self::PERSON]));
            self::assertSame(self::ACTION, $my->fetchOne('SELECT action FROM audit_actions WHERE action = ?', [self::ACTION]));
            self::rejects('already contains data', static fn() => $copy->copy($pgURL, $myURL));

            self::clear($pg);
            self::assertSame(2, $copy->copy($myURL, $pgURL));
            self::assertContains($pg->fetchOne('SELECT checked_at_us FROM http_readiness_cache'), [10, '10']);
            self::assertContains($pg->fetchOne('SELECT observations FROM php_metrics'), [3, '3']);
            self::assertSame('Éva', $pg->fetchOne('SELECT given_name FROM persons WHERE id = ?::uuid', [self::PERSON]));
            self::assertSame(self::ACTION, $pg->fetchOne('SELECT action FROM audit_actions WHERE action = ?', [self::ACTION]));
        });
    }

    public function testIntegrityFindingsRollBackCopiedRowsAndLookups(): void
    {
        $this->withDatabases(function (DatabaseCopy $copy, string $pgURL, string $myURL, Connection $pg, Connection $my): void {
            $pg->executeStatement(
                "INSERT INTO persons (id, given_name, family_name, merged_into)
                 VALUES (?::uuid, 'Ada', 'Muster', NULL), (?::uuid, 'Alt', 'Muster', ?::uuid)",
                [self::PERSON, self::MERGED, self::PERSON],
            );
            $pg->executeStatement(
                "INSERT INTO accounts (id, person_id, email, role) VALUES (?::uuid, ?::uuid, 'alt@example.invalid', 'member')",
                [self::ACCOUNT, self::MERGED],
            );
            $pg->executeStatement('INSERT INTO audit_actions (action) VALUES (?)', [self::ACTION]);

            self::rejects('Database integrity checks found inconsistencies', static fn() => $copy->copy($pgURL, $myURL));
            self::assertFalse($my->fetchOne('SELECT 1 FROM persons'));
            self::assertFalse($my->fetchOne('SELECT 1 FROM accounts'));
            self::assertFalse($my->fetchOne('SELECT 1 FROM audit_actions WHERE action = ?', [self::ACTION]));
            self::assertNotFalse($pg->fetchOne('SELECT 1 FROM accounts'));
        });
    }

    public function testCopyRequiresDifferentEnginesAndAvailableMigrationLocks(): void
    {
        $this->withDatabases(function (DatabaseCopy $copy, string $pgURL, string $myURL, Connection $pg): void {
            self::rejects('on one side', static fn() => $copy->copy($pgURL, $pgURL));
            self::assertTrue(Database::lockMigrations($pg));
            try {
                self::rejects('migration or copy is already running', static fn() => $copy->copy($pgURL, $myURL));
            } finally {
                $pg->fetchOne('SELECT pg_advisory_unlock(4097083626)');
            }
            self::assertSame(0, $copy->copy($pgURL, $myURL));
        });
    }

    /** @param Closure(DatabaseCopy,string,string,Connection,Connection):void $test */
    private function withDatabases(Closure $test): void
    {
        $pgURL = getenv('STUDIO_PHP_TEST_POSTGRES_URL');
        $myURL = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($pgURL === false || $myURL === false) {
            self::markTestSkipped('Requires both migrated disposable databases from just php-test');
        }
        $pg = Database::connect($pgURL);
        $my = Database::connect($myURL);
        $migrations = getenv('STUDIO_PHP_TEST_POSTGRES_MIGRATIONS') ?: __DIR__ . '/../../internal/store/postgres/migrations';
        try {
            foreach ([$pg, $my] as $db) {
                self::assertFalse($db->fetchOne('SELECT 1 FROM persons'), 'The copy test requires an empty fixture database');
            }
            $test(new DatabaseCopy($migrations, __DIR__ . '/../migrations/mysql'), $pgURL, $myURL, $pg, $my);
        } finally {
            foreach ([$pg, $my] as $db) {
                self::clear($db);
                $db->executeStatement('DELETE FROM php_metrics');
                $db->executeStatement("UPDATE http_readiness_cache SET version_hash = '', checked_at_us = 0, ready = FALSE");
                $db->close();
            }
        }
    }

    private static function clear(Connection $db): void
    {
        $db->executeStatement("DELETE FROM accounts WHERE email = 'alt@example.invalid'");
        $db->executeStatement("DELETE FROM persons WHERE given_name = 'Alt'");
        $db->executeStatement("DELETE FROM persons WHERE given_name IN ('Ada', 'Éva')");
        $db->executeStatement('DELETE FROM audit_actions WHERE action = ?', [self::ACTION]);
    }

    /** @param Closure():int $operation */
    private static function rejects(string $message, Closure $operation): void
    {
        try {
            $operation();
            self::fail('Expected the copy to fail');
        } catch (CopyError $failure) {
            self::assertStringContainsString($message, $failure->getMessage());
        }
    }
}
