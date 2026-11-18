<?php

declare(strict_types=1);

namespace StudioSchedule\Tests;

use Doctrine\DBAL\Connection;
use Doctrine\DBAL\Platforms\MySQLPlatform;
use PHPUnit\Framework\Attributes\Group;
use PHPUnit\Framework\TestCase;
use RuntimeException;
use StudioSchedule\Store\CopyError;
use StudioSchedule\Store\CopyRows;
use StudioSchedule\Store\Database;

#[Group('database-copy')]
final class CopyRowsTest extends TestCase
{
    private const string TABLE = 'copy_rows_fixture';
    private const string PARENT = '03000000000000000000000000000000';
    private const string CHILD = '01000000000000000000000000000000';
    private const string LEAF = '02000000000000000000000000000000';

    private const COLUMNS = [
        'id' => 'uuid',
        'parent_id' => 'uuid',
        'enabled' => 'boolean',
        'label' => 'character varying',
        'binary_value' => 'bytea',
        'payload' => 'jsonb',
        'schedule_date' => 'date',
        'schedule_time' => 'time without time zone',
        'created_at' => 'timestamp with time zone',
        'duration' => 'interval',
        'counter' => 'bigint',
        'raw_payload' => 'json',
    ];

    public function testTransfersTypedRowsAndTheirParentsInBothDirections(): void
    {
        [$postgres, $mysql] = $this->fixtures();
        try {
            $bytes = str_repeat(implode('', array_map(chr(...), range(0, 255))), 4096);
            $label = "  Malte 🇩🇪 Ą\u{0302} \t\n ";
            $postgres->executeStatement(
                "INSERT INTO copy_rows_fixture
                (id, parent_id, enabled, label, binary_value, payload, schedule_date,
                 schedule_time, created_at, duration, counter) VALUES
                (?::uuid, NULL, NULL, '', decode('', 'hex'), 'null', NULL, NULL, NULL, NULL, NULL),
                (?::uuid, ?::uuid, FALSE, ?, decode(?, 'hex'), ?, '2024-02-29', '23:59:59.654321',
                 '2024-02-29 04:30:00.123456+05:30', '12 months -359 days 0.654321 seconds', 9223372036854775807),
                (?::uuid, ?::uuid, TRUE, 'Leaf', NULL, '[false,\"x\"]', NULL, '24:00:00', NULL,
                 '-1 day -0.000001 seconds', -9223372036854775808)",
                [
                    self::PARENT, self::CHILD, self::PARENT, $label, bin2hex($bytes),
                    '{"integer":9007199254740993,"escaped":"\u0001","nested":[null,false,"\u2764"]}',
                    self::LEAF, self::CHILD,
                ],
            );
            $rawPayload = "{ \"duplicate\": true,\n  \"duplicate\": false }";
            $postgres->executeStatement(
                'UPDATE copy_rows_fixture SET raw_payload = ?::json WHERE id = ?::uuid',
                [$rawPayload, self::CHILD],
            );
            self::assertSame(3, self::copy($postgres, $mysql));
            $row = $mysql->fetchAssociative(
                'SELECT * FROM copy_rows_fixture WHERE id = UNHEX(?)',
                [self::CHILD],
            );
            self::assertIsArray($row);
            self::assertSame(hex2bin(self::PARENT), $row['parent_id']);
            self::assertSame(0, $row['enabled']);
            self::assertSame($label, $row['label']);
            self::assertSame($bytes, $row['binary_value']);
            self::assertSame('2024-02-29', $row['schedule_date']);
            self::assertSame('23:59:59.654321', $row['schedule_time']);
            self::assertSame('2024-02-28 23:00:00.123456', $row['created_at']);
            self::assertSame(86400654321, $row['duration']);
            self::assertSame(PHP_INT_MAX, $row['counter']);
            self::assertSame($rawPayload, $row['raw_payload']);
            $postgres->executeStatement('DELETE FROM copy_rows_fixture');
            self::assertSame(3, self::copy($mysql, $postgres));
            $row = $postgres->fetchAssociative(
                "SELECT enabled, label, encode(binary_value, 'hex') AS binary_value,
                        payload = '{\"integer\":9007199254740993,\"escaped\":\"\\u0001\",\"nested\":[null,false,\"\\u2764\"]}'::jsonb
                            AS same_payload,
                        schedule_date, schedule_time, created_at AT TIME ZONE 'UTC' AS created_at, counter, raw_payload,
                        CAST(EXTRACT(EPOCH FROM duration) * 1000000 AS bigint) AS duration
                   FROM copy_rows_fixture WHERE id = ?::uuid",
                [self::CHILD],
            );
            self::assertSame([
                'enabled' => false,
                'label' => $label,
                'binary_value' => bin2hex($bytes),
                'same_payload' => true,
                'schedule_date' => '2024-02-29',
                'schedule_time' => '23:59:59.654321',
                'created_at' => '2024-02-28 23:00:00.123456',
                'counter' => PHP_INT_MAX,
                'raw_payload' => $rawPayload,
                'duration' => 86400654321,
            ], $row);
            self::assertSame([
                'enabled' => null, 'empty_binary' => true, 'json_null' => true,
            ], $postgres->fetchAssociative(
                "SELECT enabled, binary_value = ''::bytea AS empty_binary, payload = 'null'::jsonb AS json_null
                   FROM copy_rows_fixture WHERE id = ?::uuid",
                [self::PARENT],
            ));
            self::assertSame([
                'enabled' => true, 'schedule_time' => '24:00:00', 'counter' => PHP_INT_MIN, 'duration' => -86400000001,
            ], $postgres->fetchAssociative(
                'SELECT enabled, schedule_time, counter, CAST(EXTRACT(EPOCH FROM duration) * 1000000 AS bigint) AS duration
                   FROM copy_rows_fixture WHERE id = ?::uuid',
                [self::LEAF],
            ));
        } finally {
            self::removeFixtures($postgres, $mysql);
        }
    }

    public function testInvalidBooleanFailsWithoutExposingSourceValues(): void
    {
        [$postgres, $mysql] = $this->fixtures();
        try {
            $mysql->executeStatement(
                'INSERT INTO copy_rows_fixture (id, parent_id, enabled)
                 VALUES (UNHEX(?), NULL, 0), (UNHEX(?), UNHEX(?), 42)',
                [self::PARENT, self::CHILD, self::PARENT],
            );
            $this->expectException(RuntimeException::class);
            $this->expectExceptionMessage('Could not copy rows from copy_rows_fixture');
            self::copy($mysql, $postgres);
        } finally {
            self::assertSame(0, $postgres->fetchOne('SELECT COUNT(*) FROM copy_rows_fixture'));
            self::removeFixtures($postgres, $mysql);
        }
    }

    public function testRejectsNativeJsonThatWouldLoseDecimalPrecision(): void
    {
        [$postgres, $mysql] = $this->fixtures();
        try {
            $payload = '{"value": 1234567890123456789012345.67890123456789}';
            $postgres->executeStatement(
                'INSERT INTO copy_rows_fixture (id, payload) VALUES (?::uuid, ?::jsonb)',
                [self::PARENT, $payload],
            );
            if ($mysql->getDatabasePlatform() instanceof MySQLPlatform) {
                try {
                    self::copy($postgres, $mysql);
                    self::fail('MySQL loses precision when storing this decimal as native JSON');
                } catch (CopyError $error) {
                    self::assertSame('MySQL cannot preserve a JSON value in copy_rows_fixture', $error->getMessage());
                    self::assertNull($error->getPrevious());
                    self::assertSame(0, $mysql->fetchOne('SELECT COUNT(*) FROM copy_rows_fixture'));
                }

                return;
            }

            self::assertSame(1, self::copy($postgres, $mysql));
            $postgres->executeStatement('DELETE FROM copy_rows_fixture');
            self::assertSame(1, self::copy($mysql, $postgres));
            self::assertTrue($postgres->fetchOne(
                'SELECT payload = ?::jsonb FROM copy_rows_fixture',
                [$payload],
            ));
        } finally {
            self::removeFixtures($postgres, $mysql);
        }
    }

    public function testSelfReferenceCycleIsLeftUncopied(): void
    {
        [$postgres, $mysql] = $this->fixtures();
        try {
            $postgres->executeStatement(
                'INSERT INTO copy_rows_fixture (id, parent_id) VALUES (?::uuid, ?::uuid), (?::uuid, ?::uuid)',
                [self::PARENT, self::CHILD, self::CHILD, self::PARENT],
            );
            self::assertSame(0, self::copy($postgres, $mysql));
            self::assertSame(0, $mysql->fetchOne('SELECT COUNT(*) FROM copy_rows_fixture'));
        } finally {
            self::removeFixtures($postgres, $mysql);
        }
    }

    public function testTransfersSelfReferencesDeeperThanTheMysqlSessionLimit(): void
    {
        [$postgres, $mysql] = $this->fixtures();
        try {
            $limit = $mysql->getDatabasePlatform() instanceof MySQLPlatform
                ? 'cte_max_recursion_depth'
                : 'max_recursive_iterations';
            $mysql->executeStatement("SET SESSION $limit = 10");
            $insert = $mysql->prepare('INSERT INTO copy_rows_fixture (id, parent_id) VALUES (UNHEX(?), UNHEX(?))');
            $parent = null;
            for ($index = 1; $index <= 12; $index++) {
                $id = str_pad(dechex($index), 32, '0', STR_PAD_LEFT);
                $insert->bindValue(1, $id);
                $insert->bindValue(2, $parent);
                $insert->executeStatement();
                $parent = $id;
            }

            self::assertSame(12, self::copy($mysql, $postgres));
            self::assertSame(12, $postgres->fetchOne('SELECT COUNT(*) FROM copy_rows_fixture'));
            self::assertEquals(10, $mysql->fetchOne("SELECT @@SESSION.$limit"));
        } finally {
            self::removeFixtures($postgres, $mysql);
        }
    }

    /** @return array{Connection,Connection} */
    private function fixtures(): array
    {
        $postgresURL = getenv('STUDIO_PHP_TEST_POSTGRES_URL');
        $mysqlURL = getenv('STUDIO_PHP_TEST_DATABASE_URL');
        if ($postgresURL === false || $mysqlURL === false) {
            self::markTestSkipped('Requires both of the disposable databases from just php-test');
        }

        $postgres = Database::connect($postgresURL);
        $mysql = Database::connect($mysqlURL);
        $postgres->executeStatement('CREATE TABLE copy_rows_fixture (
            id uuid PRIMARY KEY, parent_id uuid REFERENCES copy_rows_fixture (id), enabled boolean,
            label varchar(80), binary_value bytea, payload jsonb, schedule_date date,
            schedule_time time(6), created_at timestamptz, duration interval, counter bigint, raw_payload json
        )');
        try {
            $mysql->executeStatement('CREATE TABLE copy_rows_fixture (
                id binary(16) PRIMARY KEY, parent_id binary(16), enabled tinyint, label varbinary(255),
                binary_value longblob, payload json, schedule_date date, schedule_time time(6),
                created_at datetime(6), duration bigint, counter bigint, raw_payload longtext CHECK (JSON_VALID(raw_payload)),
                FOREIGN KEY (parent_id) REFERENCES copy_rows_fixture (id)
            )');
        } catch (\Throwable $error) {
            $postgres->executeStatement('DROP TABLE copy_rows_fixture');
            throw $error;
        }

        return [$postgres, $mysql];
    }

    private static function copy(Connection $source, Connection $target): int
    {
        return Database::inReadSnapshot(
            $source,
            static fn() => $target->transactional(static fn(Connection $target): int => CopyRows::copy(
                $source,
                $target,
                self::TABLE,
                self::COLUMNS,
                ['local' => 'parent_id', 'key' => 'id'],
            )),
        );
    }

    private static function removeFixtures(Connection $postgres, Connection $mysql): void
    {
        $postgres->executeStatement('DROP TABLE copy_rows_fixture');
        $mysql->executeStatement('DROP TABLE copy_rows_fixture');
        $postgres->close();
        $mysql->close();
    }
}
